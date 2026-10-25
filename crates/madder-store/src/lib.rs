//! madder-store: read and write madder local blob stores, byte-compatible
//! with the Go `madder`: the sealed-key store (FDR 0011) and the plain
//! V3/V4 stores Go writes by default.
//!
//! A store is a directory holding `blob_store-config` (immutable, digest
//! pinned) and blobs bucketed by digest. A sealed-key store adds
//! `blob_store-key` (the store key sealed to a pigpen recipient set) and
//! stores each blob as an age file to its public key: writing needs only
//! that public key, reading opens the sealed key once, through an injected
//! [`piggy_pigpen::EcdhOracle`] for a PIV recipient or a software
//! [`piggy_pigpen::X25519Identity`], and decrypts every blob in software
//! after that. A plain store is either unencrypted (compressed plaintext
//! on disk) or carries one raw X25519 key in its config.
//!
//! A blob's id is the digest of its plaintext: compression never changes
//! an id, and two writers of the same bytes publish the same path.
//!
//! See `docs/plans/2026-10-25-rust-sealed-key-store-design.md` in the
//! madder repository for the design and the on-disk facts.

#![forbid(unsafe_code)]

pub mod age_x25519;
pub mod blob_id;
pub mod codec;
pub mod config;
mod error;
pub mod fs;
pub mod hyphence;
pub mod init;
pub mod sidecar;
pub mod store;

pub use blob_id::{BlobId, HashFormat};
pub use codec::Compression;
pub use config::{Config, ConfigVersion, Encryption};
pub use error::{Error, Result};
pub use fs::{Fs, StdFs};
pub use init::{InitEncryption, InitOptions, init, parse_recipients};
pub use sidecar::RecipientSource;
pub use store::{BlobReader, OpenOptions, Store, Unsealer};

// Re-exported so a consumer can wire an oracle or identity without naming
// piggy-pigpen's version itself.
pub use piggy_markl;
pub use piggy_pigpen;
pub use piggy_pigpen::{EcdhOracle, X25519Identity};
