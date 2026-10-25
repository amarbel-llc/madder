//! `blob_store-config` for a sealed-key store: the
//! `toml-blob_store_config-v5` document and its `@` body digest.

use blake2::Blake2b;
use blake2::digest::consts::U32;
use piggy_markl::{FormatId, Id, PurposeId};
use serde::Deserialize;
use sha2::Digest;

use crate::blob_id::HashFormat;
use crate::codec::Compression;
use crate::hyphence;
use crate::{Error, Result};

pub const TYPE_V5: &str = "toml-blob_store_config-v5";

/// The only key-custody holder FDR 0011 defines: the unsealed key lives in
/// the process that opened it.
pub const KEY_HOLDER_PROCESS: &str = "process";

#[derive(Deserialize)]
struct TomlV5 {
    hash_buckets: Vec<usize>,
    #[serde(rename = "hash_type-id")]
    hash_type_id: String,
    encryption: Vec<String>,
    #[serde(rename = "compression-type", default)]
    compression_type: String,
    #[serde(rename = "verify-on-collision", default)]
    verify_on_collision: bool,
    #[serde(default)]
    single_hash: bool,
    #[serde(rename = "instance-id", default)]
    instance_id: Option<String>,
    #[serde(rename = "key-custody")]
    key_custody: KeyCustody,
}

#[derive(Deserialize)]
struct KeyCustody {
    holder: String,
}

/// A parsed, digest-verified sealed-key store config.
#[derive(Debug, Clone)]
pub struct Config {
    pub hash_format: HashFormat,
    pub hash_buckets: Vec<usize>,
    pub compression: Compression,
    /// The store's public key, a `piggy-recipient-v1@age_x25519_pub-…` id.
    pub public_key: Id,
    pub holder: String,
    /// Multi-hash layout (`<base>/<format>/…`) unless `single_hash`.
    pub multi_hash: bool,
    /// Carried opaquely: a `uuidv7-…` markl id this crate does not decode.
    pub instance_id: Option<String>,
    pub verify_on_collision: bool,
    /// The recorded `@` digest line, verified against the body.
    pub digest: Option<String>,
}

impl Config {
    pub fn parse(raw: &[u8]) -> Result<Config> {
        let framed = hyphence::parse(raw)?;

        if framed.type_id != TYPE_V5 {
            return Err(Error::Unsupported(format!(
                "config type {:?} (this crate reads {TYPE_V5}, the sealed-key store)",
                framed.type_id
            )));
        }

        if let Some(digest) = &framed.digest {
            verify_body_digest(raw, digest)?;
        }

        let body = std::str::from_utf8(framed.body)
            .map_err(|_| Error::Malformed("config body is not UTF-8".into()))?;
        let toml: TomlV5 =
            toml::from_str(body).map_err(|e| Error::Malformed(format!("config body: {e}")))?;

        if toml.key_custody.holder != KEY_HOLDER_PROCESS {
            return Err(Error::Unsupported(format!(
                "key-custody holder {:?} (this crate supports {KEY_HOLDER_PROCESS:?})",
                toml.key_custody.holder
            )));
        }

        if toml.encryption.len() != 1 {
            return Err(Error::Malformed(format!(
                "a sealed-key store has exactly one encryption id, its public key; got {}",
                toml.encryption.len()
            )));
        }

        let public_key = Id::parse(&toml.encryption[0])
            .map_err(|e| Error::Markl(format!("encryption id: {e}")))?;
        if public_key.format() != FormatId::AgeX25519Pub {
            return Err(Error::Malformed(format!(
                "a sealed-key store's encryption id must be an age_x25519_pub key, got {}",
                public_key.to_wire()
            )));
        }
        if !matches!(
            public_key.purpose(),
            None | Some(PurposeId::PiggyRecipientV1)
        ) {
            return Err(Error::Malformed(format!(
                "encryption id carries an unexpected purpose: {}",
                public_key.to_wire()
            )));
        }

        if toml.hash_buckets.iter().sum::<usize>() > 64 {
            return Err(Error::Malformed(format!(
                "hash_buckets {:?} exceed the 64 hex characters of a digest",
                toml.hash_buckets
            )));
        }

        Ok(Config {
            hash_format: HashFormat::parse(&toml.hash_type_id)?,
            hash_buckets: toml.hash_buckets,
            compression: Compression::parse(&toml.compression_type)?,
            public_key,
            holder: toml.key_custody.holder,
            multi_hash: !toml.single_hash,
            instance_id: toml.instance_id,
            verify_on_collision: toml.verify_on_collision,
            digest: framed.digest,
        })
    }

    /// The store public key's raw 32 bytes.
    pub fn public_key_bytes(&self) -> [u8; 32] {
        let mut out = [0u8; 32];
        out.copy_from_slice(self.public_key.data());
        out
    }
}

/// The `@` line is the blake2b256 of the body bytes after the LAST
/// `---\n\n` (FDR 0008; `DecodeAndVerify`). The id may carry a purpose;
/// only its format and bytes are compared, as Go's `markl.AssertEqual`
/// does.
fn verify_body_digest(raw: &[u8], digest_line: &str) -> Result<()> {
    let recorded =
        Id::parse(digest_line).map_err(|e| Error::Markl(format!("@ digest line: {e}")))?;
    if recorded.format() != FormatId::Blake2b256 {
        return Err(Error::Unsupported(format!(
            "config digest format {} (madder pins blake2b256)",
            recorded.format()
        )));
    }

    let body = hyphence::body_after_last_separator(raw)
        .ok_or_else(|| Error::Framing("could not locate the config body".into()))?;
    let computed = Blake2b::<U32>::digest(body);

    if computed.as_slice() != recorded.data() {
        return Err(Error::ConfigTampered);
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    const FIXTURE: &[u8] =
        include_bytes!("../tests/fixtures/zstd-blake2b256/store/blob_store-config");

    #[test]
    fn parses_the_go_written_fixture() {
        let config = Config::parse(FIXTURE).unwrap();
        assert_eq!(config.hash_format, HashFormat::Blake2b256);
        assert_eq!(config.hash_buckets, vec![2]);
        assert_eq!(config.compression, Compression::Zstd);
        assert_eq!(config.holder, KEY_HOLDER_PROCESS);
        assert!(config.multi_hash);
        assert!(!config.verify_on_collision);
        assert!(
            config
                .instance_id
                .as_deref()
                .unwrap()
                .starts_with("uuidv7-")
        );
        assert!(
            config
                .public_key
                .to_wire()
                .starts_with("piggy-recipient-v1@age_x25519_pub-")
        );
        assert!(config.digest.is_some());
    }

    #[test]
    fn a_changed_body_byte_is_tampering() {
        let mut raw = FIXTURE.to_vec();
        let at = raw.windows(4).position(|w| w == b"zstd").unwrap();
        raw[at..at + 4].copy_from_slice(b"none");
        assert!(matches!(Config::parse(&raw), Err(Error::ConfigTampered)));
    }

    #[test]
    fn a_config_without_a_digest_line_is_trusted() {
        let text = String::from_utf8(FIXTURE.to_vec()).unwrap();
        let without: String = text
            .lines()
            .filter(|l| !l.starts_with("@ "))
            .collect::<Vec<_>>()
            .join("\n")
            + "\n";
        let config = Config::parse(without.as_bytes()).unwrap();
        assert_eq!(config.digest, None);
    }

    fn rebuild(body: &str) -> Vec<u8> {
        format!("---\n! {TYPE_V5}\n---\n\n{body}").into_bytes()
    }

    #[test]
    fn refuses_what_go_refuses() {
        let good = "hash_buckets = [2]\nhash_type-id = \"blake2b256\"\nencryption = [\"piggy-recipient-v1@age_x25519_pub-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy\"]\ncompression-type = \"zstd\"\n\n[key-custody]\nholder = \"process\"\n";
        assert!(Config::parse(&rebuild(good)).is_ok());

        for (label, body) in [
            ("unknown holder", good.replace("\"process\"", "\"agent\"")),
            ("no key", good.replace("encryption = [\"piggy-recipient-v1@age_x25519_pub-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy\"]", "encryption = []")),
            (
                "two keys",
                good.replace("\"]\n", "\", \"piggy-recipient-v1@age_x25519_pub-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy\"]\n"),
            ),
            ("secret key", good.replace("age_x25519_pub-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy", "age_x25519_sec-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy")),
            ("unknown hash", good.replace("blake2b256", "md5")),
            ("unknown compression", good.replace("zstd", "brotli")),
        ] {
            assert!(Config::parse(&rebuild(&body)).is_err(), "{label}: accepted, want rejection");
        }
    }

    #[test]
    fn refuses_other_config_versions() {
        let raw = b"---\n! toml-blob_store_config-v4\n---\n\nhash_buckets = [2]\n";
        assert!(matches!(Config::parse(raw), Err(Error::Unsupported(_))));
    }
}
