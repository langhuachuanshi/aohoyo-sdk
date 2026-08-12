//! exe 完整性自检

use sha2::{Digest, Sha256};

pub fn executable_hash() -> Result<Vec<u8>, Box<dyn std::error::Error>> {
    let exe = std::env::current_exe()?;
    let mut f = std::fs::File::open(exe)?;
    let mut hasher = Sha256::new();
    std::io::copy(&mut f, &mut hasher)?;
    Ok(hasher.finalize().to_vec())
}
