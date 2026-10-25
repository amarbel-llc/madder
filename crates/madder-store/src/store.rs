//! A sealed-key local hash-bucketed store (FDR 0011).

use std::cell::OnceCell;
use std::io::{self, BufWriter, Read, Write};
use std::path::{Path, PathBuf};

use piggy_markl::Id;
use piggy_pigpen::{EcdhOracle, X25519Identity};
use zeroize::Zeroizing;

use crate::age_x25519::{self, Decryptor, Encryptor};
use crate::blob_id::{BlobId, HashFormat, Hasher};
use crate::config::Config;
use crate::fs::{Fs, StdFs};
use crate::sidecar::{RecipientSource, Sidecar};
use crate::{Error, Result};

pub const FILE_NAME_CONFIG: &str = "blob_store-config";
pub const FILE_NAME_KEY: &str = "blob_store-key";
/// The pre-rename config name Go still skips when enumerating blobs.
const FILE_NAME_CONFIG_LEGACY: &str = "dodder-blob_store-config";

fn is_store_metadata_file(name: &str) -> bool {
    name == FILE_NAME_CONFIG || name == FILE_NAME_KEY || name == FILE_NAME_CONFIG_LEGACY
}

/// What opens the sealed store key: the card-bound oracle for P-256
/// recipients, software identities for X25519 ones. Either may be absent.
pub struct Unsealer<'a> {
    pub oracle: Option<&'a dyn EcdhOracle>,
    pub identities: &'a [X25519Identity],
}

/// Options for [`Store::open_with`].
pub struct OpenOptions {
    /// Where temp files are written before being linked into the store. It
    /// must be on the same filesystem as the store. Defaults to a sibling of
    /// the store base, `<parent>/.<name>.tmp`, which is outside the walk
    /// Go's blob enumeration performs.
    pub temp_dir: Option<PathBuf>,
    pub fs: Box<dyn Fs>,
}

impl Default for OpenOptions {
    fn default() -> Self {
        OpenOptions {
            temp_dir: None,
            fs: Box::new(StdFs),
        }
    }
}

/// An open store. Reading needs [`Store::unseal`] first; writing never does.
pub struct Store {
    base: PathBuf,
    temp_dir: PathBuf,
    fs: Box<dyn Fs>,
    config: Config,
    sidecar: Sidecar,
    key: OnceCell<Zeroizing<[u8; 32]>>,
}

impl Store {
    /// Open the store at `base` with `std::fs` and the default temp dir.
    pub fn open(base: impl AsRef<Path>) -> Result<Store> {
        Store::open_with(base, OpenOptions::default())
    }

    /// Open the store at `base`: parse and verify the config, parse the
    /// sidecar. Nothing is unsealed.
    pub fn open_with(base: impl AsRef<Path>, options: OpenOptions) -> Result<Store> {
        let base = base.as_ref().to_path_buf();
        let fs = options.fs;

        let config = Config::parse(&fs.read(&base.join(FILE_NAME_CONFIG))?)?;
        let sidecar = Sidecar::parse(&fs.read(&base.join(FILE_NAME_KEY))?)?;

        let temp_dir = options.temp_dir.unwrap_or_else(|| default_temp_dir(&base));

        Ok(Store {
            base,
            temp_dir,
            fs,
            config,
            sidecar,
            key: OnceCell::new(),
        })
    }

    pub fn base(&self) -> &Path {
        &self.base
    }

    pub fn config(&self) -> &Config {
        &self.config
    }

    /// The store's public key, a `piggy-recipient-v1@age_x25519_pub-…` id.
    pub fn public_key(&self) -> &Id {
        &self.config.public_key
    }

    /// The recipients the store key is sealed to (the read half of
    /// `madder key-status`). Needs no agent.
    pub fn sealed_recipients(&self) -> Vec<Id> {
        self.sidecar.sealed_recipients()
    }

    /// Where the recipients were read from when the key was sealed.
    pub fn recipient_source(&self) -> &RecipientSource {
        &self.sidecar.source
    }

    pub fn is_unsealed(&self) -> bool {
        self.key.get().is_some()
    }

    /// Open the sealed store key. Done at most once per `Store`; a P-256
    /// recipient costs one oracle call. The opened key must be the secret
    /// half of the config's public key, else the sidecar belongs to another
    /// store.
    pub fn unseal(&self, with: &Unsealer<'_>) -> Result<()> {
        if self.key.get().is_some() {
            return Ok(());
        }

        let payload = Zeroizing::new(self.sidecar.sealed.open(with.oracle, with.identities)?);
        if payload.len() != 32 {
            return Err(Error::Malformed(format!(
                "sealed store key holds {} bytes, want 32",
                payload.len()
            )));
        }
        let mut secret = Zeroizing::new([0u8; 32]);
        secret.copy_from_slice(&payload);
        drop(payload);

        if age_x25519::public_key(&secret) != self.config.public_key_bytes() {
            return Err(Error::WrongKey);
        }

        // A concurrent unseal on another thread cannot happen: OnceCell is
        // !Sync, so a Store is used from one thread at a time.
        let _ = self.key.set(secret);
        Ok(())
    }

    fn secret(&self) -> Result<&[u8; 32]> {
        self.key.get().map(|k| &**k).ok_or(Error::Locked)
    }

    fn blob_path(&self, id: &BlobId) -> PathBuf {
        self.base
            .join(id.relative_path(&self.config.hash_buckets, self.config.multi_hash))
    }

    /// Whether the blob is in the store. The null id is always present.
    pub fn has(&self, id: &BlobId) -> bool {
        id.is_null() || self.fs.exists(&self.blob_path(id))
    }

    /// Read a blob. The bytes are decrypted and decompressed as they are
    /// read; at EOF the reader checks they hash to `id` and fails with
    /// [`io::ErrorKind::InvalidData`] (source: [`Error::DigestMismatch`])
    /// if not.
    pub fn read(&self, id: &BlobId) -> Result<BlobReader<'_>> {
        if id.is_null() {
            return Ok(BlobReader {
                inner: Box::new(io::empty()),
                hasher: Some(id.format().hasher()),
                expected: id.clone(),
                verified: false,
            });
        }

        let secret = self.secret()?;
        let path = self.blob_path(id);
        let file = match self.fs.open(&path) {
            Ok(file) => file,
            Err(e) if e.kind() == io::ErrorKind::NotFound => {
                return Err(Error::NotFound(id.to_wire()));
            }
            Err(e) => return Err(e.into()),
        };

        let decrypted = Decryptor::new(secret, file)?;
        let decompressed = self.config.compression.decompress(decrypted)?;

        Ok(BlobReader {
            inner: decompressed,
            hasher: Some(id.format().hasher()),
            expected: id.clone(),
            verified: false,
        })
    }

    /// Read a whole blob into memory.
    pub fn read_to_vec(&self, id: &BlobId) -> Result<Vec<u8>> {
        let mut reader = self.read(id)?;
        let mut out = Vec::new();
        reader.read_to_end(&mut out).map_err(Error::from_io)?;
        Ok(out)
    }

    /// Write a blob and return its id. Needs no key: the bytes are
    /// encrypted to the store's public key. Publishing follows
    /// blob-store(7) CONCURRENCY AND DURABILITY: temp file, fsync, chmod
    /// 0444, link(2) into the tree (an existing blob is the same bytes),
    /// fsync the directory. Zero bytes produce the null id and no file.
    pub fn write(&self, plaintext: impl Read) -> Result<BlobId> {
        // Encryptor::new validates the recipient point.
        let recipient = self.config.public_key_bytes();

        self.fs.create_dir_all(&self.temp_dir)?;
        let (temp_path, temp_file) = self.fs.create_temp(&self.temp_dir)?;

        let result = self.write_temp(&recipient, plaintext, temp_file);
        let id = match result {
            Ok(id) => id,
            Err(e) => {
                let _ = self.fs.remove_file(&temp_path);
                return Err(e);
            }
        };

        if id.is_null() {
            self.fs.remove_file(&temp_path)?;
            return Ok(id);
        }

        if let Err(e) = self.publish(&temp_path, &id) {
            let _ = self.fs.remove_file(&temp_path);
            return Err(e);
        }
        Ok(id)
    }

    /// Write bytes already in memory.
    pub fn write_bytes(&self, plaintext: &[u8]) -> Result<BlobId> {
        self.write(plaintext)
    }

    fn write_temp(
        &self,
        recipient: &[u8; 32],
        plaintext: impl Read,
        temp_file: Box<dyn crate::fs::TempFile>,
    ) -> Result<BlobId> {
        let mut temp_file = temp_file;
        let hashing = HashingReader {
            inner: plaintext,
            hasher: self.config.hash_format.hasher(),
        };
        let encryptor = Encryptor::new(recipient, BufWriter::new(&mut temp_file))?;
        let mut encryptor = encryptor;
        let mut hashing = hashing;
        self.config
            .compression
            .compress(&mut hashing, &mut encryptor)?;
        let mut buffered = encryptor.finish()?;
        buffered.flush()?;
        drop(buffered);
        temp_file.sync()?;
        drop(temp_file);
        Ok(hashing.hasher.finish())
    }

    fn publish(&self, temp_path: &Path, id: &BlobId) -> Result<()> {
        let final_path = self.blob_path(id);
        let dir = final_path
            .parent()
            .ok_or_else(|| Error::Malformed("blob path has no parent".into()))?;
        self.fs.create_dir_all(dir)?;
        self.fs.set_readonly(temp_path)?;

        // Once link(2) has returned, the blob is published and the write
        // has succeeded; failing to unlink the temp name afterwards leaves a
        // stray file in the temp dir, not a failed write, so it is not
        // reported as one.
        match self.fs.hard_link(temp_path, &final_path) {
            Ok(()) => {
                let _ = self.fs.remove_file(temp_path);
                self.fs.sync_dir(dir)?;
                Ok(())
            }
            Err(e) if e.kind() == io::ErrorKind::AlreadyExists => {
                // A same-digest blob is already published: the same bytes
                // (ADR 0002). verify-on-collision is not implemented here;
                // it would need the unsealed key on the write path.
                let _ = self.fs.remove_file(temp_path);
                Ok(())
            }
            Err(e) if e.kind() == io::ErrorKind::CrossesDevices => Err(Error::Io(io::Error::new(
                e.kind(),
                format!(
                    "blob temp dir {} and store base {} are on different filesystems; link(2) cannot cross mount boundaries (blob-store(7), ADR 0003)",
                    self.temp_dir.display(),
                    self.base.display()
                ),
            ))),
            Err(e) => Err(e.into()),
        }
    }

    /// Every blob id in the store, in path order. In the multi-hash layout
    /// only directories at the base are walked (Go's
    /// `localAllBlobsMultihash` skips files there, which is where its
    /// `key-reseal` leaves a temp sidecar); inside a hash directory, and in
    /// the single-hash layout, store metadata files are skipped and anything
    /// else that is not a blob path is an error, as in Go.
    pub fn ids(&self) -> Result<Vec<BlobId>> {
        let mut ids = Vec::new();
        if self.config.multi_hash {
            for entry in self.fs.read_dir(&self.base)? {
                if !entry.is_dir {
                    continue;
                }
                let format = HashFormat::parse(&entry.name).map_err(|_| {
                    Error::Malformed(format!(
                        "directory {:?} at the store base is not a hash format",
                        entry.name
                    ))
                })?;
                self.walk_blobs(
                    &self.base.join(&entry.name),
                    format,
                    String::new(),
                    &mut ids,
                )?;
            }
        } else {
            self.walk_blobs(&self.base, self.config.hash_format, String::new(), &mut ids)?;
        }
        Ok(ids)
    }

    fn walk_blobs(
        &self,
        dir: &Path,
        format: HashFormat,
        hex_prefix: String,
        ids: &mut Vec<BlobId>,
    ) -> Result<()> {
        for entry in self.fs.read_dir(dir)? {
            if !entry.is_dir && is_store_metadata_file(&entry.name) {
                continue;
            }
            let hex = format!("{hex_prefix}{}", entry.name);
            if entry.is_dir {
                self.walk_blobs(&dir.join(&entry.name), format, hex, ids)?;
            } else {
                ids.push(BlobId::from_hex(format, &hex).map_err(|e| {
                    Error::Malformed(format!("{}: {e}", dir.join(&entry.name).display()))
                })?);
            }
        }
        Ok(())
    }
}

fn default_temp_dir(base: &Path) -> PathBuf {
    let name = base
        .file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_else(|| "store".to_owned());
    let parent = base.parent().unwrap_or(base);
    parent.join(format!(".{name}.tmp"))
}

/// A blob being read; see [`Store::read`].
pub struct BlobReader<'a> {
    inner: Box<dyn Read + 'a>,
    hasher: Option<Hasher>,
    expected: BlobId,
    verified: bool,
}

impl Read for BlobReader<'_> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        // An empty buffer is not a read: inner.read returns 0 for it, which
        // must not be taken for EOF.
        if self.verified || buf.is_empty() {
            return Ok(0);
        }
        let n = self.inner.read(buf)?;
        if n > 0 {
            if let Some(h) = self.hasher.as_mut() {
                h.update(&buf[..n]);
            }
            return Ok(n);
        }
        let got = self.hasher.take().expect("verified once").finish();
        self.verified = true;
        if got != self.expected {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                Error::DigestMismatch {
                    wanted: self.expected.to_wire(),
                    got: got.to_wire(),
                },
            ));
        }
        Ok(0)
    }
}

struct HashingReader<R: Read> {
    inner: R,
    hasher: Hasher,
}

impl<R: Read> Read for HashingReader<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        let n = self.inner.read(buf)?;
        self.hasher.update(&buf[..n]);
        Ok(n)
    }
}
