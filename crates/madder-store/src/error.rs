//! The crate's error type. Variants are the distinctions a vault UI acts
//! on; everything else carries a message.

/// Errors produced by this crate.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// A store file (config, sidecar) is not framed as expected.
    #[error("hyphence framing: {0}")]
    Framing(String),
    /// The config body does not match its recorded digest.
    #[error("blob_store-config body does not match its @ digest")]
    ConfigTampered,
    /// A config or sidecar names something this crate cannot use.
    #[error("unsupported: {0}")]
    Unsupported(String),
    /// A config or sidecar is well framed but malformed inside.
    #[error("malformed: {0}")]
    Malformed(String),
    /// A markl id did not parse.
    #[error("markl: {0}")]
    Markl(String),
    /// Reading before `unseal`.
    #[error("the store key has not been unsealed")]
    Locked,
    /// No recipient of the sealed key could be opened.
    #[error("sealed store key: {0}")]
    Pigpen(#[from] piggy_pigpen::Error),
    /// The sealed key's public half is not the store's public key.
    #[error("the sealed store key does not match the store's public key")]
    WrongKey,
    /// A blob is not in the store.
    #[error("blob not found: {0}")]
    NotFound(String),
    /// Decrypted bytes did not hash to the id they were read under.
    #[error("digest mismatch: wanted {wanted}, got {got}")]
    DigestMismatch { wanted: String, got: String },
    /// An age file could not be read with the store key.
    #[error("age: {0}")]
    Age(String),
    /// A compressed stream could not be decoded.
    #[error("codec: {0}")]
    Codec(String),
    #[error(transparent)]
    Io(#[from] std::io::Error),
}

impl Error {
    /// Recover an `Error` the streaming readers carried through
    /// `std::io::Read` (as the source of an `InvalidData` error); any other
    /// I/O error is wrapped as [`Error::Io`].
    pub fn from_io(e: std::io::Error) -> Error {
        if e.kind() == std::io::ErrorKind::InvalidData
            && e.get_ref().is_some_and(|inner| inner.is::<Error>())
        {
            return *e
                .into_inner()
                .expect("get_ref was Some")
                .downcast::<Error>()
                .expect("checked with is::<Error>");
        }
        Error::Io(e)
    }
}

pub type Result<T> = std::result::Result<T, Error>;
