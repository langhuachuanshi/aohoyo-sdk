//! KV 配置读取（GET /as/v1/app/kv，DeviceSign 签名）。
//! 服务端返回本应用 + 公共区（__common__）的合并结果，本应用同 key 优先。

use crate::Native;
use std::collections::HashMap;
use std::io::Read;

impl Native {
    /// 拉取当前应用可见的全部 KV 配置（DeviceSign 鉴权，头 X-App-ID）。
    /// 返回 {key: value} 映射，值已由服务端按类型解析：
    /// string→string、number→f64、boolean→bool、json→原生对象/数组。
    pub fn get_kv(
        &self,
        device_id: &str,
    ) -> Result<HashMap<String, serde_json::Value>, Box<dyn std::error::Error>> {
        if self.app_secret.is_empty() {
            return Err("app_secret 未配置".into());
        }
        let sign = self.sign_request(device_id, "")?;

        let agent = crate::upgrade::http_agent();
        let resp = agent
            .get(&format!("{}/as/v1/app/kv", self.base_url.trim_end_matches('/')))
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
            return Err(format!("KV 拉取失败: {msg}").into());
        }
        Ok(serde_json::from_value(v["data"].clone())?)
    }
}

#[cfg(test)]
mod tests {
    use crate::Native;
    use std::io::Read;

    #[test]
    fn get_kv_requires_secret() {
        let native = Native::new("app", "", "http://127.0.0.1:1");
        assert!(native.get_kv("device-1").is_err());
    }

    #[test]
    fn get_kv_parses_typed_values() {
        // 极简 TCP mock：单次响应固定 JSON（与 admin-server 统一信封一致）
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let addr = listener.local_addr().unwrap();
        let server = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buf = [0u8; 4096];
            let _ = stream.read(&mut buf);
            let body = r#"{"code":200,"message":"success","data":{"announcement":"hi","n":7,"flag":true,"cfg":{"a":1}}}"#;
            let resp = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                body.len(),
                body
            );
            use std::io::Write;
            stream.write_all(resp.as_bytes()).unwrap();
        });

        let native = Native::new("app_kv_test", "secret_kv_test", &format!("http://{addr}"));
        let vars = native.get_kv("device-1").unwrap();
        server.join().unwrap();

        assert_eq!(vars["announcement"], "hi");
        assert_eq!(vars["n"], 7);
        assert_eq!(vars["flag"], true);
        assert_eq!(vars["cfg"]["a"], 1);
    }

    #[test]
    fn get_kv_surfaces_business_error() {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let addr = listener.local_addr().unwrap();
        let server = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buf = [0u8; 4096];
            let _ = stream.read(&mut buf);
            let body = r#"{"code":500,"message":"boom","data":null}"#;
            let resp = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
                body.len(),
                body
            );
            use std::io::Write;
            stream.write_all(resp.as_bytes()).unwrap();
        });

        let native = Native::new("app_kv_test", "secret_kv_test", &format!("http://{addr}"));
        let err = native.get_kv("device-1").unwrap_err();
        server.join().unwrap();
        assert!(err.to_string().contains("boom"));
    }
}
