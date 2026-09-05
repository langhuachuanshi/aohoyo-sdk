//! 安装包下载（断点续传）与哈希校验。

use crate::Native;
use md5::Md5;
use sha2::{Digest, Sha256};
use std::fs::OpenOptions;
use std::io::{Read, Write};
use std::path::Path;

/// 下载进度回调参数：(received, total)，total 未知为 0。
pub type ProgressFn<'a> = dyn FnMut(u64, u64) + 'a;

impl Native {
    /// 下载安装包到 `dest`，断点续传（`dest.part` 已有内容时发 Range 续传，续传被拒则重下）。
    /// `expected_size` 传服务端 file_size；`progress` 可为 None。
    pub fn download_file(
        &self,
        url: &str,
        dest: &str,
        expected_size: u64,
        mut progress: Option<&mut ProgressFn>,
    ) -> Result<(), Box<dyn std::error::Error>> {
        if url.is_empty() {
            return Err("下载地址为空".into());
        }
        let part = format!("{dest}.part");

        let mut offset: u64 = 0;
        if let Ok(st) = std::fs::metadata(&part) {
            offset = st.len();
            // 服务端已知总大小且 .part 已达标 → 直接落位（上次下完但没 rename 的场景）
            if expected_size > 0 && offset == expected_size {
                std::fs::rename(&part, dest)?;
                return Ok(());
            }
        }

        let agent = crate::upgrade::http_agent();
        let mut req = agent.get(url);
        if offset > 0 {
            req = req.set("Range", &format!("bytes={offset}-"));
        }
        let resp = req.call()?;

        let status = resp.status();
        if offset > 0 && status != 206 {
            // 服务端拒绝续传（如文件已变更）→ 丢弃 .part 从头下
            let _ = std::fs::remove_file(&part);
            offset = 0;
        } else if status != 200 && status != 206 {
            return Err(format!("下载失败 HTTP {status}").into());
        }

        let mut total = expected_size;
        if total == 0 {
            if let Some(v) = resp.header("Content-Range").and_then(parse_content_range_total) {
                total = v;
            }
        }

        let append = offset > 0 && status == 206;
        let mut f = OpenOptions::new()
            .create(true)
            .write(true)
            .append(append)
            .truncate(!append)
            .open(&part)?;

        let mut reader = resp.into_reader();
        let mut buf = [0u8; 64 << 10];
        let mut received = offset;
        loop {
            let n = reader.read(&mut buf)?;
            if n == 0 {
                break;
            }
            f.write_all(&buf[..n])?;
            received += n as u64;
            if let Some(cb) = progress.as_deref_mut() {
                cb(received, total);
            }
        }
        drop(f);

        if total > 0 && received != total {
            return Err(format!("下载不完整: {received}/{total} 字节（.part 保留，下次续传）").into());
        }
        std::fs::rename(&part, dest)?;
        Ok(())
    }

    /// 校验文件哈希：SHA256 优先，其次 MD5；对应期望值为空则跳过该项。
    pub fn verify_file(&self, path: &str, sha256_hex: &str, md5_hex: &str) -> Result<(), Box<dyn std::error::Error>> {
        if sha256_hex.is_empty() && md5_hex.is_empty() {
            return Err("未提供任何哈希期望值".into());
        }
        let data = std::fs::read(Path::new(path))?;

        if !sha256_hex.is_empty() {
            let got = hex::encode(Sha256::digest(&data));
            if !got.eq_ignore_ascii_case(sha256_hex) {
                return Err(format!("SHA256 不匹配: 期望 {sha256_hex} 实得 {got}").into());
            }
        }
        if !md5_hex.is_empty() {
            let got = hex::encode(Md5::digest(&data));
            if !got.eq_ignore_ascii_case(md5_hex) {
                return Err(format!("MD5 不匹配: 期望 {md5_hex} 实得 {got}").into());
            }
        }
        Ok(())
    }
}

/// 解析 `Content-Range: bytes 100-199/300` → 300。
fn parse_content_range_total(v: &str) -> Option<u64> {
    let total = v.rsplit('/').next()?;
    total.parse::<u64>().ok().filter(|&t| t > 0)
}
