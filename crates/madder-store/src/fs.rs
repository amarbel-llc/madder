//! The filesystem the store runs on, behind a trait so a host without
//! `std::fs` (wasm32) can supply its own. [`StdFs`] is the default.

use std::fs::{self, File, OpenOptions};
use std::io::{self, Read, Write};
use std::path::{Path, PathBuf};

use rand_core::{OsRng, RngCore};

/// A directory entry as [`Fs::read_dir`] reports it.
pub struct DirEntry {
    pub name: String,
    pub is_dir: bool,
}

/// A temp file being written: a writer that can be synced to disk.
pub trait TempFile: Write {
    fn sync(&mut self) -> io::Result<()>;
}

impl TempFile for File {
    fn sync(&mut self) -> io::Result<()> {
        self.sync_all()
    }
}

/// What the store needs from a filesystem. Paths are absolute or relative
/// to the host's working directory; the store builds them from its base.
pub trait Fs {
    fn read(&self, path: &Path) -> io::Result<Vec<u8>>;
    fn open(&self, path: &Path) -> io::Result<Box<dyn Read>>;
    fn exists(&self, path: &Path) -> bool;
    fn read_dir(&self, path: &Path) -> io::Result<Vec<DirEntry>>;
    fn create_dir_all(&self, path: &Path) -> io::Result<()>;
    /// Create a new, uniquely named file under `dir` and return its path
    /// and writer.
    fn create_temp(&self, dir: &Path) -> io::Result<(PathBuf, Box<dyn TempFile>)>;
    /// Make `path` read-only for everyone (mode 0444).
    fn set_readonly(&self, path: &Path) -> io::Result<()>;
    /// `link(2)`: an existing `to` must fail with
    /// [`io::ErrorKind::AlreadyExists`].
    fn hard_link(&self, from: &Path, to: &Path) -> io::Result<()>;
    fn remove_file(&self, path: &Path) -> io::Result<()>;
    /// `fsync` a directory so its entries are durable.
    fn sync_dir(&self, path: &Path) -> io::Result<()>;
    /// Create `path`, failing if it exists, and write `bytes` to it.
    fn write_new(&self, path: &Path, bytes: &[u8]) -> io::Result<()>;
}

/// `std::fs`.
#[derive(Debug, Default, Clone, Copy)]
pub struct StdFs;

impl Fs for StdFs {
    fn read(&self, path: &Path) -> io::Result<Vec<u8>> {
        fs::read(path)
    }

    fn open(&self, path: &Path) -> io::Result<Box<dyn Read>> {
        Ok(Box::new(File::open(path)?))
    }

    fn exists(&self, path: &Path) -> bool {
        path.exists()
    }

    fn read_dir(&self, path: &Path) -> io::Result<Vec<DirEntry>> {
        let mut entries = Vec::new();
        for entry in fs::read_dir(path)? {
            let entry = entry?;
            entries.push(DirEntry {
                name: entry.file_name().to_string_lossy().into_owned(),
                is_dir: entry.file_type()?.is_dir(),
            });
        }
        entries.sort_by(|a, b| a.name.cmp(&b.name));
        Ok(entries)
    }

    fn create_dir_all(&self, path: &Path) -> io::Result<()> {
        fs::create_dir_all(path)
    }

    fn create_temp(&self, dir: &Path) -> io::Result<(PathBuf, Box<dyn TempFile>)> {
        loop {
            let mut random = [0u8; 8];
            OsRng.fill_bytes(&mut random);
            let name = format!(
                "tmp-{}-{}",
                std::process::id(),
                random
                    .iter()
                    .map(|b| format!("{b:02x}"))
                    .collect::<String>()
            );
            let path = dir.join(name);
            match OpenOptions::new().write(true).create_new(true).open(&path) {
                Ok(file) => return Ok((path, Box::new(file))),
                Err(e) if e.kind() == io::ErrorKind::AlreadyExists => continue,
                Err(e) => return Err(e),
            }
        }
    }

    fn set_readonly(&self, path: &Path) -> io::Result<()> {
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(path, fs::Permissions::from_mode(0o444))
        }
        #[cfg(not(unix))]
        {
            let mut permissions = fs::metadata(path)?.permissions();
            permissions.set_readonly(true);
            fs::set_permissions(path, permissions)
        }
    }

    fn hard_link(&self, from: &Path, to: &Path) -> io::Result<()> {
        fs::hard_link(from, to)
    }

    fn remove_file(&self, path: &Path) -> io::Result<()> {
        fs::remove_file(path)
    }

    fn sync_dir(&self, path: &Path) -> io::Result<()> {
        File::open(path)?.sync_all()
    }

    fn write_new(&self, path: &Path, bytes: &[u8]) -> io::Result<()> {
        let mut file = OpenOptions::new().write(true).create_new(true).open(path)?;
        file.write_all(bytes)?;
        file.sync_all()
    }
}
