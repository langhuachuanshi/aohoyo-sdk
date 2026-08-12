//! 防多开互斥：Windows Global Mutex / Unix flock。

#[cfg(windows)]
pub struct InstanceMutex {
    handle: Option<windows_sys::Win32::Foundation::HANDLE>,
}

#[cfg(windows)]
impl Default for InstanceMutex {
    fn default() -> Self {
        Self { handle: None }
    }
}

#[cfg(windows)]
impl InstanceMutex {
    pub fn acquire(&mut self, app_id: &str) -> Result<bool, Box<dyn std::error::Error>> {
        let name = format!(
            "Global\\aohoyo_{}",
            app_id.replace('\\', "_").replace('/', "_")
        );
        match crate::risks::imp::create_mutex(&name)? {
            Some(h) => {
                self.handle = Some(h);
                Ok(true)
            }
            None => Ok(false),
        }
    }
}

#[cfg(windows)]
impl Drop for InstanceMutex {
    fn drop(&mut self) {
        if let Some(h) = self.handle {
            unsafe {
                windows_sys::Win32::Foundation::CloseHandle(h);
            }
        }
    }
}

#[cfg(not(windows))]
pub struct InstanceMutex {
    file: Option<std::fs::File>,
}

#[cfg(not(windows))]
impl Default for InstanceMutex {
    fn default() -> Self {
        Self { file: None }
    }
}

#[cfg(not(windows))]
impl InstanceMutex {
    pub fn acquire(&mut self, app_id: &str) -> Result<bool, Box<dyn std::error::Error>> {
        let cache = std::env::var("XDG_CACHE_HOME").unwrap_or_else(|_| {
            format!(
                "{}/.cache",
                std::env::var("HOME").unwrap_or_else(|_| "/tmp".into())
            )
        });
        let dir = format!("{cache}/aohoyo/locks");
        std::fs::create_dir_all(&dir)?;
        let path = format!("{dir}/{}.lock", sanitize_key(app_id));
        let f = std::fs::OpenOptions::new()
            .create(true)
            .read(true)
            .write(true)
            .open(path)?;
        let r = unsafe {
            libc::flock(
                std::os::fd::AsRawFd::as_raw_fd(&f),
                libc::LOCK_EX | libc::LOCK_NB,
            )
        };
        if r != 0 {
            return Ok(false); // 已有实例
        }
        self.file = Some(f);
        Ok(true)
    }
}

#[cfg(not(windows))]
fn sanitize_key(key: &str) -> String {
    let out: String = key
        .chars()
        .filter(|c| c.is_ascii_alphanumeric() || matches!(c, '_' | '-' | '.'))
        .collect();
    if out.is_empty() {
        "_".into()
    } else {
        out
    }
}
