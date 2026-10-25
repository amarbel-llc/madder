//! Blob ids: a content digest under one of madder's two hash formats, its
//! markl wire form, its hex form, and its place in the bucketed tree.

use std::path::PathBuf;

use blake2::Blake2b;
use blake2::digest::consts::U32;
use piggy_markl::{FormatId, Id};
use sha2::{Digest, Sha256};

use crate::{Error, Result};

/// The hash formats a local store can be keyed by (`hash_type-id`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum HashFormat {
    Sha256,
    Blake2b256,
}

impl HashFormat {
    /// The markl format id, which is also the directory name in the
    /// multi-hash layout.
    pub fn as_str(self) -> &'static str {
        match self {
            HashFormat::Sha256 => "sha256",
            HashFormat::Blake2b256 => "blake2b256",
        }
    }

    pub fn parse(s: &str) -> Result<Self> {
        match s {
            "sha256" => Ok(HashFormat::Sha256),
            "blake2b256" => Ok(HashFormat::Blake2b256),
            other => Err(Error::Unsupported(format!("hash type {other:?}"))),
        }
    }

    fn markl(self) -> FormatId {
        match self {
            HashFormat::Sha256 => FormatId::Sha256,
            HashFormat::Blake2b256 => FormatId::Blake2b256,
        }
    }

    fn from_markl(format: FormatId) -> Option<Self> {
        match format {
            FormatId::Sha256 => Some(HashFormat::Sha256),
            FormatId::Blake2b256 => Some(HashFormat::Blake2b256),
            _ => None,
        }
    }

    pub fn hasher(self) -> Hasher {
        match self {
            HashFormat::Sha256 => Hasher::Sha256(Sha256::new()),
            HashFormat::Blake2b256 => Hasher::Blake2b256(Blake2b::<U32>::new()),
        }
    }

    /// The null id: the digest of zero bytes. Go stores nothing for it.
    pub fn null_id(self) -> BlobId {
        self.hasher().finish()
    }
}

/// An incremental digest under one hash format.
pub enum Hasher {
    Sha256(Sha256),
    Blake2b256(Blake2b<U32>),
}

impl Hasher {
    pub fn update(&mut self, bytes: &[u8]) {
        match self {
            Hasher::Sha256(h) => h.update(bytes),
            Hasher::Blake2b256(h) => h.update(bytes),
        }
    }

    pub fn finish(self) -> BlobId {
        let (format, digest) = match self {
            Hasher::Sha256(h) => (HashFormat::Sha256, h.finalize()),
            Hasher::Blake2b256(h) => (HashFormat::Blake2b256, h.finalize()),
        };
        let mut out = [0u8; 32];
        out.copy_from_slice(&digest);
        BlobId {
            format,
            digest: out,
        }
    }
}

/// A blob's content-addressed id.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct BlobId {
    format: HashFormat,
    digest: [u8; 32],
}

impl BlobId {
    pub fn new(format: HashFormat, digest: [u8; 32]) -> Self {
        BlobId { format, digest }
    }

    /// Parse the markl wire form, `blake2b256-<blech32>` or
    /// `sha256-<blech32>`; a purpose prefix is accepted and dropped.
    pub fn parse(s: &str) -> Result<Self> {
        let id = Id::parse(s).map_err(|e| Error::Markl(format!("{s:?}: {e}")))?;
        Self::from_markl(&id)
    }

    pub fn from_markl(id: &Id) -> Result<Self> {
        let format = HashFormat::from_markl(id.format())
            .ok_or_else(|| Error::Markl(format!("{} is not a blob digest", id.to_wire())))?;
        let mut digest = [0u8; 32];
        digest.copy_from_slice(id.data());
        Ok(BlobId { format, digest })
    }

    /// Parse the 64 lower-case hex characters a blob's path spells.
    pub fn from_hex(format: HashFormat, hex: &str) -> Result<Self> {
        if hex.len() != 64 {
            return Err(Error::Malformed(format!(
                "blob digest hex has {} characters, want 64",
                hex.len()
            )));
        }
        let mut digest = [0u8; 32];
        for (i, pair) in hex.as_bytes().chunks(2).enumerate() {
            let hi = hex_nibble(pair[0])?;
            let lo = hex_nibble(pair[1])?;
            digest[i] = (hi << 4) | lo;
        }
        Ok(BlobId { format, digest })
    }

    pub fn format(&self) -> HashFormat {
        self.format
    }

    pub fn digest(&self) -> &[u8; 32] {
        &self.digest
    }

    /// The markl wire form, with no purpose (hash ids carry none).
    pub fn to_wire(&self) -> String {
        Id::new(None, self.format.markl(), self.digest.to_vec())
            .expect("a 32-byte digest is a valid markl payload")
            .to_wire()
    }

    pub fn to_markl(&self) -> Id {
        Id::new(None, self.format.markl(), self.digest.to_vec())
            .expect("a 32-byte digest is a valid markl payload")
    }

    /// Lower-case hex, the form the bucketed path is cut from.
    pub fn hex(&self) -> String {
        let mut s = String::with_capacity(64);
        for b in self.digest {
            s.push(HEX[(b >> 4) as usize] as char);
            s.push(HEX[(b & 0x0f) as usize] as char);
        }
        s
    }

    /// Whether this is the digest of zero bytes, for which Go stores no
    /// file and reads back nothing.
    pub fn is_null(&self) -> bool {
        *self == self.format.null_id()
    }

    /// The blob's path relative to the store base: `<format>/` first when
    /// the store is multi-hash (the default), then one directory per
    /// `hash_buckets` entry of that many hex characters, then the rest.
    pub fn relative_path(&self, buckets: &[usize], multi_hash: bool) -> PathBuf {
        let mut path = PathBuf::new();
        if multi_hash {
            path.push(self.format.as_str());
        }
        let hex = self.hex();
        let mut rest = hex.as_str();
        for &bucket in buckets {
            let (head, tail) = rest.split_at(bucket.min(rest.len()));
            path.push(head);
            rest = tail;
        }
        if !rest.is_empty() {
            path.push(rest);
        }
        path
    }
}

impl std::fmt::Display for BlobId {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.to_wire())
    }
}

const HEX: &[u8; 16] = b"0123456789abcdef";

fn hex_nibble(c: u8) -> Result<u8> {
    match c {
        b'0'..=b'9' => Ok(c - b'0'),
        b'a'..=b'f' => Ok(c - b'a' + 10),
        other => Err(Error::Malformed(format!(
            "blob digest hex has a non-hex character {:?}",
            other as char
        ))),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn hex_round_trips_through_the_bucket_path() {
        let id = HashFormat::Blake2b256.hasher().finish();
        let hex = id.hex();
        assert_eq!(hex.len(), 64);
        assert_eq!(BlobId::from_hex(HashFormat::Blake2b256, &hex).unwrap(), id);

        let path = id.relative_path(&[2], true);
        let s = path.to_str().unwrap();
        assert_eq!(s, format!("blake2b256/{}/{}", &hex[..2], &hex[2..]));

        let path = id.relative_path(&[2, 3], false);
        let s = path.to_str().unwrap();
        assert_eq!(s, format!("{}/{}/{}", &hex[..2], &hex[2..5], &hex[5..]));
    }

    #[test]
    fn the_null_id_is_the_digest_of_nothing() {
        assert!(HashFormat::Sha256.null_id().is_null());
        let mut h = HashFormat::Sha256.hasher();
        h.update(b"x");
        assert!(!h.finish().is_null());
        // sha256("") is well known.
        assert_eq!(
            HashFormat::Sha256.null_id().hex(),
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        );
    }

    #[test]
    fn wire_form_round_trips_and_accepts_a_purpose() {
        let id = HashFormat::Sha256.null_id();
        let wire = id.to_wire();
        assert!(wire.starts_with("sha256-"));
        assert_eq!(BlobId::parse(&wire).unwrap(), id);
        assert_eq!(
            BlobId::parse(&format!("dodder-blob-digest-sha256-v1@{wire}")).unwrap(),
            id
        );
        assert!(
            BlobId::parse(
                "age_x25519_pub-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0jqr9fwqu"
            )
            .is_err()
        );
    }
}
