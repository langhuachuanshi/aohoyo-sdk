//! 广告客户端接口（主仓库 docs/specs/ad.md）：
//!
//!	GET  /as/v1/ads             DeviceSign（GET 空 body 参与签名）
//!	POST /as/v1/ads/impression  公开（限流 100/min）
//!	POST /as/v1/ads/click       公开（限流 100/min）
//!
//! 位 code 必传：位 code 为客户端构建与后台广告位配置的私有约定，本模块不提供全量拉取；
//! 全量语义只保留在服务端接口（供宿主后端代理场景使用）。

use crate::Native;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::io::Read;

/// 广告项（与服务端 model.AdClientItem 对应，服务端已脱敏不含广告主信息）。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AdItem {
    pub id: i64,
    pub position_id: i64,
    /// 文字广告即内容，其他类型作 alt
    pub title: String,
    /// 文字广告内容
    pub content: String,
    /// 图片/开屏/横幅
    pub image_url: String,
    pub link_url: String,
    /// 1文字 2图片 3弹窗 4开屏 5横幅
    pub ad_type: i16,
    pub width: i32,
    pub height: i32,
    pub start_time: Option<String>,
    pub end_time: Option<String>,
    /// 广告级扩展配置（展示上限/频控等，由后台投放时约定）
    #[serde(default)]
    pub config: Option<serde_json::Value>,
}

/// 拉取广告响应：key = 广告位 code（如 splash、home-banner）。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ClientAds {
    pub positions: HashMap<String, Vec<AdItem>>,
}

impl Native {
    /// 拉取指定广告位的当前有效广告（DeviceSign 鉴权，GET 空 body 签名）。
    /// device_id 建议传机器指纹 hash（灰度/统计口径稳定）。
    /// position_code 必传：位 code 为客户端与后台的私有约定，不提供全量拉取。
    pub fn get_ads(
        &self,
        device_id: &str,
        position_code: &str,
    ) -> Result<ClientAds, Box<dyn std::error::Error>> {
        if position_code.is_empty() {
            return Err("position_code 必传：位 code 为客户端与后台的私有约定，SDK 不提供全量拉取".into());
        }
        if self.app_secret.is_empty() {
            return Err("app_secret 未配置".into());
        }
        let sign = self.sign_request(device_id, "")?;

        let agent = crate::upgrade::http_agent();
        let resp = agent
            .get(&format!("{}/as/v1/ads", self.base_url.trim_end_matches('/')))
            .query("app_id", &self.app_id)
            .query("position", position_code)
            .set("X-App-ID", &self.app_id)
            .set("X-Device-Sign", &sign.sign)
            .set("X-Device-ID", device_id)
            .set("X-Timestamp", &sign.timestamp)
            .set("X-Nonce", &sign.nonce)
            .call()?;

        let mut text = String::new();
        resp.into_reader().take(1 << 20).read_to_string(&mut text)?;

        let v: serde_json::Value = serde_json::from_str(&text)?;
        if v["code"].as_i64() != Some(200) {
            let msg = v["message"].as_str().unwrap_or("unknown");
            return Err(format!("广告拉取失败: {msg}").into());
        }
        Ok(serde_json::from_value(v["data"].clone())?)
    }

    /// 上报广告曝光（公开接口，限流 100/min）。device_id 建议传机器指纹 hash，缺省记空。
    pub fn record_impression(&self, ad_id: i64, device_id: &str) -> Result<(), Box<dyn std::error::Error>> {
        self.report(ad_id, device_id, "/as/v1/ads/impression", "曝光上报失败")
    }

    /// 上报广告点击（公开接口，限流 100/min）。
    pub fn record_click(&self, ad_id: i64, device_id: &str) -> Result<(), Box<dyn std::error::Error>> {
        self.report(ad_id, device_id, "/as/v1/ads/click", "点击上报失败")
    }

    fn report(
        &self,
        ad_id: i64,
        device_id: &str,
        path: &str,
        err_label: &str,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let body = serde_json::json!({ "ad_id": ad_id, "device_id": device_id });

        let agent = crate::upgrade::http_agent();
        let resp = agent
            .post(&format!("{}{}", self.base_url.trim_end_matches('/'), path))
            .set("Content-Type", "application/json")
            .send_string(&body.to_string())?;

        let mut text = String::new();
        resp.into_reader().take(1 << 20).read_to_string(&mut text)?;

        let v: serde_json::Value = serde_json::from_str(&text)?;
        if v["code"].as_i64() != Some(200) {
            let msg = v["message"].as_str().unwrap_or("unknown");
            return Err(format!("{err_label}: {msg}").into());
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::Native;
    use std::io::Read;

    /// 起极简 TCP mock（单次响应固定 JSON），返回 (addr, 收到的完整请求文本)。
    /// 读满整个请求（header 解析 Content-Length 后继续读 body）——POST 的 body
    /// 与 header 可能分 TCP 段到达，单次 read 会截到无 body 的半截请求（flaky）。
    fn spawn_mock(body: &'static str) -> (std::net::SocketAddr, std::sync::mpsc::Receiver<String>) {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let addr = listener.local_addr().unwrap();
        let (tx, rx) = std::sync::mpsc::channel();
        std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buf: Vec<u8> = Vec::new();
            let mut chunk = [0u8; 4096];
            loop {
                if let Some(pos) = buf.windows(4).position(|w| w == b"\r\n\r\n") {
                    let head = String::from_utf8_lossy(&buf[..pos]).to_ascii_lowercase();
                    let cl = head
                        .lines()
                        .find_map(|l| l.strip_prefix("content-length:"))
                        .and_then(|v| v.trim().parse::<usize>().ok())
                        .unwrap_or(0);
                    if buf.len() >= pos + 4 + cl {
                        break;
                    }
                }
                let n = stream.read(&mut chunk).unwrap_or(0);
                if n == 0 {
                    break;
                }
                buf.extend_from_slice(&chunk[..n]);
            }
            let _ = tx.send(String::from_utf8_lossy(&buf).to_string());
            let resp = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                body.len(),
                body
            );
            use std::io::Write;
            stream.write_all(resp.as_bytes()).unwrap();
        });
        (addr, rx)
    }

    #[test]
    fn get_ads_requires_position() {
        let native = Native::new("app", "secret", "http://127.0.0.1:1");
        let err = native.get_ads("device-1", "").unwrap_err();
        assert!(err.to_string().contains("position_code 必传"));
    }

    #[test]
    fn get_ads_sends_signed_query_and_parses() {
        let (addr, rx) = spawn_mock(
            r#"{"code":200,"message":"success","data":{"positions":{"splash":[{"id":7,"position_id":2,"title":"活动","content":"立减","image_url":"https://cdn/x.png","link_url":"https://x.com","ad_type":4,"width":1080,"height":1920,"start_time":null,"end_time":null,"config":{"freq":1}}]}}}"#,
        );
        let native = Native::new("app_ads_test", "secret_ads_test", &format!("http://{addr}"));
        let ads = native.get_ads("device-1", "splash").unwrap();
        let req = rx.recv().unwrap();

        assert!(req.starts_with("GET /as/v1/ads?"), "请求行: {req}");
        assert!(req.contains("app_id=app_ads_test"), "应带 app_id: {req}");
        assert!(req.contains("position=splash"), "应带 position: {req}");
        assert!(req.contains("X-Device-Sign:") || req.contains("x-device-sign:"), "应带签名头: {req}");

        let items = ads.positions.get("splash").unwrap();
        assert_eq!(items.len(), 1);
        assert_eq!(items[0].id, 7);
        assert_eq!(items[0].ad_type, 4);
        assert_eq!(items[0].config.as_ref().unwrap()["freq"], 1);
    }

    #[test]
    fn record_impression_posts_plain() {
        let (addr, rx) = spawn_mock(r#"{"code":200,"message":"ok","data":null}"#);
        let native = Native::new("app_ads_test", "secret_ads_test", &format!("http://{addr}"));
        native.record_impression(7, "device-1").unwrap();
        let req = rx.recv().unwrap();

        assert!(req.starts_with("POST /as/v1/ads/impression "), "请求行: {req}");
        assert!(req.contains("\"ad_id\":7"), "body 应含 ad_id: {req}");
        assert!(req.contains("\"device_id\":\"device-1\""), "body 应含 device_id: {req}");
    }

    #[test]
    fn record_click_posts_plain() {
        let (addr, rx) = spawn_mock(r#"{"code":200,"message":"ok","data":null}"#);
        let native = Native::new("app_ads_test", "secret_ads_test", &format!("http://{addr}"));
        native.record_click(9, "device-1").unwrap();
        let req = rx.recv().unwrap();

        assert!(req.starts_with("POST /as/v1/ads/click "), "请求行: {req}");
        assert!(req.contains("\"ad_id\":9"), "body 应含 ad_id: {req}");
    }

    #[test]
    fn report_surfaces_business_error() {
        let (addr, _rx) = spawn_mock(r#"{"code":500,"message":"boom","data":null}"#);
        let native = Native::new("app_ads_test", "secret_ads_test", &format!("http://{addr}"));
        let err = native.record_impression(7, "device-1").unwrap_err();
        assert!(err.to_string().contains("boom"));
    }
}
