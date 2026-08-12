use aohoyo_native::Native;

#[test]
fn sign_request_round_trip() {
    let n = Native::new("app_test", "secret_123", "https://api.example.com");
    let res = n.sign_request("dev_001", r#"{"a":1}"#).expect("sign");
    assert_eq!(res.sign.len(), 64, "sign 应为 64 hex");
    assert!(!res.timestamp.is_empty());
    assert!(res.nonce.len() >= 16);

    // 同参数签名应一致（服务端可复算）
    let res2 = n.sign_request("dev_001", r#"{"a":1}"#).expect("sign2");
    assert_eq!(res2.sign, res.sign);
}

#[test]
fn verify_upgrade_signature() {
    let n = Native::new("app_test", "secret_123", "");
    let manifest = r#"{"has_update":true,"latest_version":"1.0.0"}"#;
    assert!(!n.verify_upgrade(manifest, "deadbeef"), "错误签名应失败");

    // 服务端同构：HMAC-SHA256(app_secret, manifestJSON)
    let sig = {
        use hmac::{Hmac, Mac};
        use sha2::Sha256;
        let mut mac = Hmac::<Sha256>::new_from_slice(b"secret_123").unwrap();
        mac.update(manifest.as_bytes());
        hex::encode(mac.finalize().into_bytes())
    };
    assert!(n.verify_upgrade(manifest, &sig), "正确签名应通过");
}

#[test]
fn machine_fingerprint_deterministic() {
    let n = Native::new("app_test", "secret_123", "");
    let a = n.machine_fingerprint().expect("fp1");
    let b = n.machine_fingerprint().expect("fp2");
    assert!(!a.hash.is_empty());
    assert_eq!(a.hash, b.hash, "指纹应确定");
}

#[test]
fn detect_risks_valid_flags() {
    let n = Native::new("app_test", "secret_123", "");
    let res = n.detect_risks().expect("risks");
    for f in &res.flags {
        assert!(
            matches!(
                f.as_str(),
                "debug" | "emulator" | "root" | "hook" | "multiopen"
            ),
            "未知 flag: {f}"
        );
    }
}

#[test]
fn self_integrity_returns_hash() {
    let n = Native::new("app_test", "secret_123", "");
    let res = n.self_integrity().expect("integrity");
    assert_eq!(res.hash.len(), 64, "SHA256 hex 长度应为 64");
    assert!(res.ok);
}

#[test]
fn acquire_mutex_twice() {
    let n = Native::new("app_test", "secret_123", "");
    let first = n.acquire_mutex().expect("mutex1");
    if !first {
        return; // 环境已有实例锁
    }
    let second = n.acquire_mutex().expect("mutex2");
    assert!(!second, "同一进程第二次获取应返回 false");
}

#[test]
fn secure_store_round_trip() {
    let n = Native::new("app_test", "secret_123", "");
    let key = "test_token_rust";
    n.secure_set(key, "secret-value-xyz").expect("set");
    let got = n.secure_get(key).expect("get");
    assert_eq!(got, "secret-value-xyz");
}

#[test]
fn cert_pin_round_trip() {
    let n = Native::new("app_test", "secret_123", "");
    n.set_cert_pin("sha256/abc123");
    assert_eq!(n.get_cert_pin().as_deref(), Some("sha256/abc123"));
}
