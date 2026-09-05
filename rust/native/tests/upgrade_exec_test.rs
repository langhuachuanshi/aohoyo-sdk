//! 升级执行链集成测试：检测+验签（真实 HMAC 字节级复算）、断点续传、哈希校验。

use aohoyo_native::{CheckRequest, Native};
use hmac::{Hmac, Mac};
use sha2::Sha256;
use std::net::TcpListener;
use std::thread;
use tiny_http::{Header, Response, Server};

type HmacSha256 = Hmac<Sha256>;

fn hmac_hex(key: &str, data: &str) -> String {
    let mut mac = HmacSha256::new_from_slice(key.as_bytes()).unwrap();
    mac.update(data.as_bytes());
    hex::encode(mac.finalize().into_bytes())
}

const SIGNED_BODY: &str = r#"{"code":200,"message":"success","data":{"has_update":true,"force_update":false,"latest_version":"2026.9.4","latest_version_code":20260904,"platform":"windows","download_url":"http://REPLACED/setup.exe","file_size":16,"md5":"","sha256":"abc","update_log":"修复 <问题> & 改进","signature":"SIG"}}"#;

#[test]
fn test_check_upgrade_verify_real() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let addr = listener.local_addr().unwrap();
    let base = format!("http://{addr}");
    let secret = "test_secret".to_string();
    let base_body = base.clone();

    let server = Server::from_listener(listener, None).unwrap();
    let body = {
        let full = SIGNED_BODY.replace("http://REPLACED", &base_body);
        let key = ",\"signature\":\"";
        let data_start = full.rfind(",\"data\":").map(|p| p + 8).unwrap();
        let inner = &full[data_start..full.len() - 1];
        let mi = inner.rfind(key).unwrap();
        let manifest = format!("{}{}", &inner[..mi], "}");
        let sig = hmac_hex(&secret, &manifest);
        full.replace("\"SIG\"", &format!("\"{sig}\""))
    };

    let handle = thread::spawn(move || {
        if let Ok(mut req) = server.recv() {
            let mut rb = String::new();
            req.as_reader().read_to_string(&mut rb).unwrap();
            assert!(rb.contains(r#""platform":"windows""#), "请求应带 platform");
            let _ = req.respond(
                Response::from_string(body)
                    .with_header(Header::from_bytes("Content-Type", "application/json").unwrap()),
            );
        }
    });

    let n = Native::new("demo", "test_secret", &base);
    let r = n
        .check_upgrade(&CheckRequest {
            current_version_code: 1,
            platform: "windows".into(),
            channel_code: String::new(),
            device_id: String::new(),
        })
        .unwrap();

    assert!(r.has_update);
    assert_eq!(r.latest_version_code, 20260904);
    assert!(!r.signature.is_empty());
    assert!(!r.raw_manifest.is_empty() && !r.raw_manifest.contains("signature"));

    // 字节级 manifest 复算（含 Go escapeHTML 字符 <>& 的 update_log）
    assert!(n.verify_check_result(&r).unwrap(), "签名校验应通过");

    // 篡改后必须失败
    let mut evil = r.clone();
    evil.raw_manifest = evil.raw_manifest.replace("setup.exe", "evil.exe");
    assert!(!n.verify_check_result(&evil).unwrap(), "篡改后不应通过");

    let _ = handle.join();
}

#[test]
fn test_download_resume() {
    let payload: Vec<u8> = (0u8..16).collect();
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let addr = listener.local_addr().unwrap();
    let base = format!("http://{addr}");
    let server = Server::from_listener(listener, None).unwrap();
    let data = payload.clone();

    // server 线程 detach：只服务一次 Range 续传请求，测试主体断言完即结束
    thread::spawn(move || {
        if let Ok(req) = server.recv() {
            let range = req
                .headers()
                .iter()
                .find(|h| h.field.equiv("Range"))
                .map(|h| h.value.as_str().to_string());
            if let Some(rg) = &range {
                let start: usize = rg.trim_start_matches("bytes=").trim_end_matches('-').parse().unwrap();
                let end = data.len() - 1;
                let cr = format!("bytes {start}-{end}/{}", data.len());
                let _ = req.respond(
                    Response::from_data(data[start..].to_vec())
                        .with_status_code(206)
                        .with_header(Header::from_bytes("Content-Range", cr.as_str()).unwrap()),
                );
            } else {
                let _ = req.respond(Response::from_data(data.clone()).with_status_code(200));
            }
        }
    });

    let dir = std::env::temp_dir().join(format!("aohoyo_test_{}", std::process::id()));
    std::fs::create_dir_all(&dir).unwrap();
    let dest = dir.join("setup.part_test");

    // 预置半个文件 → 触发续传
    std::fs::write(dest.with_extension("part_test.part"), &payload[..8]).unwrap();
    let n = Native::new("demo", "s", &base);
    let mut progress_calls = 0;
    let dest_str = dest.to_string_lossy().to_string();
    n.download_file(&base, &dest_str, payload.len() as u64, Some(&mut |_r, _t| progress_calls += 1))
        .unwrap();

    let got = std::fs::read(&dest).unwrap();
    assert_eq!(got, payload, "续传拼接结果应等于完整文件");
    assert!(!dest.with_extension("part_test.part").exists(), "完成后 .part 应被 rename");
    assert!(progress_calls > 0, "进度回调应被调用");

    let _ = std::fs::remove_dir_all(&dir);
}

#[test]
fn test_verify_file() {
    let dir = std::env::temp_dir().join(format!("aohoyo_vf_{}", std::process::id()));
    std::fs::create_dir_all(&dir).unwrap();
    let p = dir.join("a.bin");
    std::fs::write(&p, b"hello").unwrap();

    use sha2::Digest;
    let sha = hex::encode(Sha256::digest(b"hello"));
    let n = Native::new("demo", "s", "http://localhost");
    assert!(n.verify_file(p.to_str().unwrap(), &sha, "").is_ok());
    assert!(n.verify_file(p.to_str().unwrap(), "deadbeef", "").is_err());
    assert!(n.verify_file(p.to_str().unwrap(), "", "").is_err());

    let _ = std::fs::remove_dir_all(&dir);
}
