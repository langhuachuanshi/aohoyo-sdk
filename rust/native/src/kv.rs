//! KV 配置读取（GET /as/v1/app/kv，DeviceSign 签名）。
//! 服务端返回本应用 + 公共区（__common__）的合并结果，本应用同 key 优先。

use crate::Native;
use std::collections::HashMap;
use std::io::Read;

impl Native {
    /// 拉取应用可见的 KV 配置（DeviceSign 鉴权，头 X-App-ID）。
    /// 返回 {key: value} 映射，值已由服务端按类型解析：
    /// string→string、number→f64、boolean→bool、json→原生对象/数组。
    ///
    /// keys：传 `&[]` 返回全部；传指定键只返回它们（不存在的键直接缺席）。
    /// 按需过滤仅节省传输，不是访问控制。
    pub fn get_kv(
        &self,
        device_id: &str,
        keys: &[&str],
    ) -> Result<HashMap<String, serde_json::Value>, Box<dyn std::error::Error>> {
        if self.app_secret.is_empty() {
            return Err("app_secret 未配置".into());
        }
        let sign = self.sign_request(device_id, "")?;

        let agent = crate::upgrade::http_agent();
        let mut req = agent
            .get(&format!("{}/as/v1/app/kv", self.base_url.trim_end_matches('/')));
        if !keys.is_empty() {
            req = req.query("key", &keys.join(","));
        }
        let resp = req
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
        assert!(native.get_kv("device-1", &[]).is_err());
    }

    /// 起极简 TCP mock（单次响应固定 JSON），返回 (addr, 收到的请求行)
    fn spawn_kv_mock(body: &'static str) -> (std::net::SocketAddr, std::sync::mpsc::Receiver<String>) {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let addr = listener.local_addr().unwrap();
        let (tx, rx) = std::sync::mpsc::channel();
        std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buf = [0u8; 4096];
            let _ = stream.read(&mut buf);
            let req_line = String::from_utf8_lossy(&buf)
                .lines()
                .next()
                .unwrap_or_default()
                .to_string();
            let _ = tx.send(req_line);
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
    fn get_kv_parses_typed_values() {
        let (addr, rx) = spawn_kv_mock(
            r#"{"code":200,"message":"success","data":{"announcement":"hi","n":7,"flag":true,"cfg":{"a":1}}}"#,
        );
        let native = Native::new("app_kv_test", "secret_kv_test", &format!("http://{addr}"));
        let vars = native.get_kv("device-1", &[]).unwrap();
        let req_line = rx.recv().unwrap();

        assert!(req_line.starts_with("GET /as/v1/app/kv "), "请求行: {req_line}");
        assert!(!req_line.contains("key="), "空 keys 不应带 query: {req_line}");
        assert_eq!(vars["announcement"], "hi");
        assert_eq!(vars["n"], 7);
        assert_eq!(vars["flag"], true);
        assert_eq!(vars["cfg"]["a"], 1);
    }

    #[test]
    fn get_kv_with_keys_sends_query() {
        let (addr, rx) = spawn_kv_mock(
            r#"{"code":200,"message":"success","data":{"group_link":"https://t.me/x"}}"#,
        );
        let native = Native::new("app_kv_test", "secret_kv_test", &format!("http://{addr}"));
        let vars = native.get_kv("device-1", &["group_link", "extra"]).unwrap();
        let req_line = rx.recv().unwrap();

        assert!(
            req_line.starts_with("GET /as/v1/app/kv?key=group_link%2Cextra ")
                || req_line.starts_with("GET /as/v1/app/kv?key=group_link,extra "),
            "指定 keys 应带 key query: {req_line}"
        );
        assert_eq!(vars["group_link"], "https://t.me/x");
    }

    #[test]
    fn get_kv_surfaces_business_error() {
        let (addr, _rx) = spawn_kv_mock(r#"{"code":500,"message":"boom","data":null}"#);
        let native = Native::new("app_kv_test", "secret_kv_test", &format!("http://{addr}"));
        let err = native.get_kv("device-1", &[]).unwrap_err();
        assert!(err.to_string().contains("boom"));
    }
}
