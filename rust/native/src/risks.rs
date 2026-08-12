//! 风险检测（威慑层，可被绕过；服务端不得仅凭此做封禁决策）。

use crate::RisksResult;

#[cfg(windows)]
pub(crate) mod imp {
    use windows_sys::Win32::Foundation::{
        CloseHandle, GetLastError, LocalFree, INVALID_HANDLE_VALUE,
    };
    use windows_sys::Win32::Security::Cryptography::{
        CryptProtectData, CryptUnprotectData, CRYPTPROTECT_UI_FORBIDDEN, CRYPT_INTEGER_BLOB,
    };
    use windows_sys::Win32::Security::{
        GetTokenInformation, TokenElevation, TOKEN_ELEVATION, TOKEN_QUERY,
    };
    use windows_sys::Win32::System::Diagnostics::Debug::IsDebuggerPresent;
    use windows_sys::Win32::System::Diagnostics::ToolHelp::{
        CreateToolhelp32Snapshot, Module32FirstW, Module32NextW, MODULEENTRY32W, TH32CS_SNAPMODULE,
    };
    use windows_sys::Win32::System::Registry::{
        RegCloseKey, RegOpenKeyExW, RegQueryValueExW, HKEY, HKEY_LOCAL_MACHINE, KEY_READ,
    };
    use windows_sys::Win32::System::Threading::{
        CreateMutexW, GetCurrentProcess, OpenProcessToken,
    };

    pub fn read_machine_guid() -> Result<String, String> {
        let subkey = wide(r"SOFTWARE\Microsoft\Cryptography");
        let mut hkey: HKEY = std::ptr::null_mut();
        let r =
            unsafe { RegOpenKeyExW(HKEY_LOCAL_MACHINE, subkey.as_ptr(), 0, KEY_READ, &mut hkey) };
        if r != 0 {
            return Err(format!("RegOpenKeyExW: {r}"));
        }
        let _ = unsafe { RegCloseKey(hkey) };

        let name = wide("MachineGuid");
        let mut size: u32 = 0;
        let r = unsafe {
            RegQueryValueExW(
                hkey,
                name.as_ptr(),
                std::ptr::null_mut(),
                std::ptr::null_mut(),
                std::ptr::null_mut(),
                &mut size,
            )
        };
        if r != 0 || size == 0 {
            return Err(format!("RegQueryValueExW(size): {r}"));
        }
        let mut buf = vec![0u8; size as usize];
        let r = unsafe {
            RegQueryValueExW(
                hkey,
                name.as_ptr(),
                std::ptr::null_mut(),
                std::ptr::null_mut(),
                buf.as_mut_ptr(),
                &mut size,
            )
        };
        if r != 0 {
            return Err(format!("RegQueryValueExW: {r}"));
        }
        // REG_SZ 数据按 UTF-16 存储
        let u16buf: Vec<u16> = buf
            .chunks_exact(2)
            .map(|c| u16::from_le_bytes([c[0], c[1]]))
            .collect();
        Ok(from_wide(&u16buf).trim().to_string())
    }

    pub fn is_debugger_present() -> bool {
        unsafe { IsDebuggerPresent() != 0 }
    }

    pub fn registry_key_exists(root: HKEY, sub_key: &str) -> bool {
        let k = wide(sub_key);
        let mut hkey: HKEY = std::ptr::null_mut();
        let r = unsafe { RegOpenKeyExW(root, k.as_ptr(), 0, KEY_READ, &mut hkey) };
        if r != 0 {
            return false;
        }
        unsafe { RegCloseKey(hkey) };
        true
    }

    pub fn is_elevated() -> bool {
        let process = unsafe { GetCurrentProcess() };
        let mut token: windows_sys::Win32::Foundation::HANDLE = std::ptr::null_mut();
        if unsafe { OpenProcessToken(process, TOKEN_QUERY, &mut token) } == 0 {
            return false;
        }
        let mut elev = TOKEN_ELEVATION { TokenIsElevated: 0 };
        let mut ret_len: u32 = 0;
        let r = unsafe {
            GetTokenInformation(
                token,
                TokenElevation,
                &mut elev as *mut _ as *mut std::ffi::c_void,
                std::mem::size_of::<TOKEN_ELEVATION>() as u32,
                &mut ret_len,
            )
        };
        unsafe { CloseHandle(token) };
        r != 0 && elev.TokenIsElevated != 0
    }

    pub fn loaded_modules() -> Vec<String> {
        let snap = unsafe { CreateToolhelp32Snapshot(TH32CS_SNAPMODULE, 0) };
        if snap == INVALID_HANDLE_VALUE {
            return Vec::new();
        }
        let mut me: MODULEENTRY32W = unsafe { std::mem::zeroed() };
        me.dwSize = std::mem::size_of::<MODULEENTRY32W>() as u32;
        let mut names = Vec::new();
        let mut ok = unsafe { Module32FirstW(snap, &mut me) } != 0;
        while ok {
            names.push(from_wide(&me.szModule).to_lowercase());
            ok = unsafe { Module32NextW(snap, &mut me) } != 0;
        }
        unsafe { CloseHandle(snap) };
        names
    }

    pub fn wide(s: &str) -> Vec<u16> {
        s.encode_utf16().chain(std::iter::once(0)).collect()
    }

    pub fn from_wide(buf: &[u16]) -> String {
        let end = buf.iter().position(|&c| c == 0).unwrap_or(buf.len());
        String::from_utf16_lossy(&buf[..end])
    }

    pub fn protect_data(data: &[u8]) -> Result<Vec<u8>, String> {
        let mut in_blob = CRYPT_INTEGER_BLOB {
            cbData: data.len() as u32,
            pbData: data.as_ptr() as *mut u8,
        };
        let mut out_blob = CRYPT_INTEGER_BLOB {
            cbData: 0,
            pbData: std::ptr::null_mut(),
        };
        let r = unsafe {
            CryptProtectData(
                &mut in_blob,
                std::ptr::null(),
                std::ptr::null(),
                std::ptr::null(),
                std::ptr::null(),
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut out_blob,
            )
        };
        if r == 0 {
            return Err("CryptProtectData 失败".into());
        }
        let out = if out_blob.pbData.is_null() || out_blob.cbData == 0 {
            Vec::new()
        } else {
            unsafe { std::slice::from_raw_parts(out_blob.pbData, out_blob.cbData as usize) }
                .to_vec()
        };
        unsafe { LocalFree(out_blob.pbData as *mut _) };
        Ok(out)
    }

    pub fn unprotect_data(data: &[u8]) -> Result<Vec<u8>, String> {
        let mut in_blob = CRYPT_INTEGER_BLOB {
            cbData: data.len() as u32,
            pbData: data.as_ptr() as *mut u8,
        };
        let mut out_blob = CRYPT_INTEGER_BLOB {
            cbData: 0,
            pbData: std::ptr::null_mut(),
        };
        let r = unsafe {
            CryptUnprotectData(
                &mut in_blob,
                std::ptr::null_mut(),
                std::ptr::null(),
                std::ptr::null(),
                std::ptr::null(),
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut out_blob,
            )
        };
        if r == 0 {
            return Err("CryptUnprotectData 失败".into());
        }
        let out = if out_blob.pbData.is_null() || out_blob.cbData == 0 {
            Vec::new()
        } else {
            unsafe { std::slice::from_raw_parts(out_blob.pbData, out_blob.cbData as usize) }
                .to_vec()
        };
        unsafe { LocalFree(out_blob.pbData as *mut _) };
        Ok(out)
    }

    pub fn create_mutex(
        name: &str,
    ) -> Result<Option<windows_sys::Win32::Foundation::HANDLE>, String> {
        let wide_name = wide(name);
        let handle = unsafe { CreateMutexW(std::ptr::null(), 0, wide_name.as_ptr()) };
        if handle.is_null() {
            return Err("CreateMutexW 失败".into());
        }
        if unsafe { GetLastError() } == 183 {
            unsafe { CloseHandle(handle) };
            return Ok(None); // 已有实例
        }
        Ok(Some(handle))
    }
}

#[cfg(not(windows))]
pub(crate) mod imp {
    pub fn is_tracer_present() -> bool {
        if let Ok(s) = std::fs::read_to_string("/proc/self/status") {
            for line in s.lines() {
                if let Some(v) = line.strip_prefix("TracerPid:") {
                    return v.trim() != "0";
                }
            }
        }
        false
    }

    pub fn is_vm_by_dmi() -> bool {
        let markers = ["vmware", "virtualbox", "qemu", "kvm", "xen", "bochs"];
        for path in [
            "/sys/class/dmi/id/product_name",
            "/sys/class/dmi/id/product_version",
            "/sys/class/dmi/id/sys_vendor",
        ] {
            if let Ok(s) = std::fs::read_to_string(path) {
                let v = s.to_lowercase();
                if markers.iter().any(|m| v.contains(m)) {
                    return true;
                }
            }
        }
        false
    }
}

#[cfg(windows)]
pub fn detect_risks() -> Result<RisksResult, Box<dyn std::error::Error>> {
    let mut flags: Vec<String> = Vec::new();
    if imp::is_debugger_present() {
        flags.push("debug".into());
    }
    if imp::registry_key_exists(
        windows_sys::Win32::System::Registry::HKEY_LOCAL_MACHINE,
        r"SOFTWARE\VMware, Inc.",
    ) || imp::registry_key_exists(
        windows_sys::Win32::System::Registry::HKEY_LOCAL_MACHINE,
        r"SOFTWARE\Oracle\VirtualBox Guest Additions",
    ) {
        flags.push("emulator".into());
    }
    if imp::is_elevated() {
        flags.push("root".into());
    }
    let blacklist = [
        "frida-gadget",
        "frida-agent",
        "frida-core",
        "scylla",
        "megadump",
        "x64dbg",
        "ollydbg",
        "cheatengine",
        "speedhack",
        "inject",
    ];
    let mut hook = false;
    let mut sandbox = false;
    for m in imp::loaded_modules() {
        if blacklist.iter().any(|b| m.contains(b)) {
            hook = true;
        }
        if m.contains("sbie") {
            sandbox = true;
        }
    }
    if hook {
        flags.push("hook".into());
    }
    if sandbox {
        flags.push("multiopen".into());
    }
    Ok(RisksResult { flags })
}

#[cfg(not(windows))]
pub fn detect_risks() -> Result<RisksResult, Box<dyn std::error::Error>> {
    let mut flags: Vec<String> = Vec::new();
    if imp::is_tracer_present() {
        flags.push("debug".into());
    }
    if imp::is_vm_by_dmi() {
        flags.push("emulator".into());
    }
    if unsafe { libc::geteuid() } == 0 {
        flags.push("root".into());
    }
    if std::env::var("LD_PRELOAD")
        .map(|v| !v.is_empty())
        .unwrap_or(false)
    {
        flags.push("hook".into());
    }
    Ok(RisksResult { flags })
}

/// 供 fingerprint 模块复用的宽字符串工具（Windows）
#[cfg(windows)]
pub(crate) use imp::read_machine_guid;
