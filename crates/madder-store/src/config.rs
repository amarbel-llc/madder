//! `blob_store-config`: a local hash-bucketed store's immutable config and
//! its `@` body digest. Three versions are read: `toml-blob_store_config-v3`
//! and `-v4`, the plain stores Go writes by default (madder#318), and
//! `-v5`, the sealed-key store (FDR 0011).

use blake2::Blake2b;
use blake2::digest::consts::U32;
use piggy_markl::{FormatId, Id, PurposeId};
use serde::Deserialize;
use sha2::Digest;
use zeroize::Zeroizing;

use crate::age_x25519;
use crate::blob_id::HashFormat;
use crate::codec::Compression;
use crate::hyphence;
use crate::{Error, Result};

pub const TYPE_V3: &str = "toml-blob_store_config-v3";
pub const TYPE_V4: &str = "toml-blob_store_config-v4";
pub const TYPE_V5: &str = "toml-blob_store_config-v5";

/// The only key-custody holder FDR 0011 defines: the unsealed key lives in
/// the process that opened it.
pub const KEY_HOLDER_PROCESS: &str = "process";

/// The purpose Go stamps on a plain store's raw key
/// (`markl.PurposeMadderPrivateKeyV1`).
pub const PURPOSE_MADDER_PRIVATE_KEY_V1: &str = "madder-private_key-v1";

/// The config versions this crate reads. V4 is what `madder init` writes
/// for a plain store; V5 is the sealed-key store; V3 is V4 without an
/// instance id, which Go upgrades in memory on read.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ConfigVersion {
    V3,
    V4,
    V5,
}

impl ConfigVersion {
    pub fn type_id(self) -> &'static str {
        match self {
            ConfigVersion::V3 => TYPE_V3,
            ConfigVersion::V4 => TYPE_V4,
            ConfigVersion::V5 => TYPE_V5,
        }
    }

    fn parse(type_id: &str) -> Result<Self> {
        match type_id {
            TYPE_V3 => Ok(ConfigVersion::V3),
            TYPE_V4 => Ok(ConfigVersion::V4),
            TYPE_V5 => Ok(ConfigVersion::V5),
            "toml-blob_store_config-v1" | "toml-blob_store_config-v2" => {
                Err(Error::Unsupported(format!(
                    "config type {type_id:?} (this crate reads {TYPE_V3}, {TYPE_V4} and {TYPE_V5}; the v1/v2 shapes are not implemented)"
                )))
            }
            other => Err(Error::Unsupported(format!(
                "config type {other:?} (this crate reads {TYPE_V3}, {TYPE_V4} and {TYPE_V5}, the local hash-bucketed store)"
            ))),
        }
    }

    pub fn is_sealed_key(self) -> bool {
        self == ConfigVersion::V5
    }
}

/// How a store's blobs are encrypted on disk.
#[derive(Clone)]
pub enum Encryption {
    /// `encryption = []`: the file is the compressed plaintext and nothing
    /// else.
    None,
    /// One raw `age_x25519_sec` key in the config, what `madder init
    /// -encryption generate` writes: age to the key's public half, decrypted
    /// with the secret. No sidecar, nothing to unseal.
    Key {
        id: Id,
        secret: Zeroizing<[u8; 32]>,
        public: [u8; 32],
    },
    /// FDR 0011: the store's public key; the secret half is sealed in
    /// `blob_store-key`.
    Sealed { public_key: Id },
}

impl std::fmt::Debug for Encryption {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Encryption::None => f.write_str("None"),
            Encryption::Key { id, .. } => write!(f, "Key({})", id.to_wire()),
            Encryption::Sealed { public_key } => write!(f, "Sealed({})", public_key.to_wire()),
        }
    }
}

impl Encryption {
    /// The X25519 public key blobs are encrypted to, if any.
    pub fn recipient(&self) -> Option<[u8; 32]> {
        match self {
            Encryption::None => None,
            Encryption::Key { public, .. } => Some(*public),
            Encryption::Sealed { public_key } => {
                let mut out = [0u8; 32];
                out.copy_from_slice(public_key.data());
                Some(out)
            }
        }
    }

    /// The public key as a markl id: the config's own for a sealed store,
    /// derived (and purpose-less) for a raw key.
    pub fn public_key(&self) -> Option<Id> {
        match self {
            Encryption::None => None,
            Encryption::Key { public, .. } => {
                Some(Id::new(None, FormatId::AgeX25519Pub, public.to_vec()).expect("32 bytes"))
            }
            Encryption::Sealed { public_key } => Some(public_key.clone()),
        }
    }

    /// What Go's `info-repo encryption` prints: the configured ids in wire
    /// form, empty for a plain unencrypted store.
    pub fn describe(&self) -> String {
        match self {
            Encryption::None => String::new(),
            Encryption::Key { id, .. } => id.to_wire(),
            Encryption::Sealed { public_key } => public_key.to_wire(),
        }
    }

    pub fn is_sealed(&self) -> bool {
        matches!(self, Encryption::Sealed { .. })
    }
}

/// The fields V3, V4 and V5 share, each optional where a version omits it.
#[derive(Deserialize)]
struct TomlLocal {
    /// Absent means no bucketing: the whole hex digest is one file name.
    #[serde(default)]
    hash_buckets: Vec<usize>,
    #[serde(default)]
    base_path: Option<String>,
    #[serde(rename = "hash_type-id")]
    hash_type_id: String,
    /// Omitted entirely by Go when empty; older hand-written configs spell
    /// it as a single string, which Go's decoder accepts too.
    #[serde(default, deserialize_with = "string_or_strings")]
    encryption: Vec<String>,
    #[serde(rename = "compression-type", default)]
    compression_type: String,
    #[serde(rename = "verify-on-collision", default)]
    verify_on_collision: bool,
    #[serde(default)]
    single_hash: bool,
    #[serde(rename = "instance-id", default)]
    instance_id: Option<String>,
    #[serde(rename = "key-custody", default)]
    key_custody: Option<KeyCustody>,
}

#[derive(Deserialize)]
struct KeyCustody {
    holder: String,
}

/// `encryption = []`, `encryption = ["id"]` or the one-string spelling
/// `encryption = "id"` (empty string: no key).
fn string_or_strings<'de, D: serde::Deserializer<'de>>(
    d: D,
) -> std::result::Result<Vec<String>, D::Error> {
    #[derive(Deserialize)]
    #[serde(untagged)]
    enum OneOrMany {
        One(String),
        Many(Vec<String>),
    }
    Ok(match OneOrMany::deserialize(d)? {
        OneOrMany::One(s) if s.is_empty() => Vec::new(),
        OneOrMany::One(s) => vec![s],
        OneOrMany::Many(v) => v,
    })
}

/// A parsed, digest-verified store config.
#[derive(Debug, Clone)]
pub struct Config {
    pub version: ConfigVersion,
    pub hash_format: HashFormat,
    pub hash_buckets: Vec<usize>,
    pub compression: Compression,
    pub encryption: Encryption,
    /// `[key-custody] holder`; present on a sealed-key config only.
    pub holder: Option<String>,
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
        let version = ConfigVersion::parse(&framed.type_id)?;

        if let Some(digest) = &framed.digest {
            verify_body_digest(raw, digest)?;
        }

        let body = std::str::from_utf8(framed.body)
            .map_err(|_| Error::Malformed("config body is not UTF-8".into()))?;
        let toml: TomlLocal =
            toml::from_str(body).map_err(|e| Error::Malformed(format!("config body: {e}")))?;

        if let Some(base_path) = toml.base_path.as_deref().filter(|p| !p.is_empty()) {
            return Err(Error::Unsupported(format!(
                "base_path {base_path:?} (a store rooted away from its config directory)"
            )));
        }

        let (encryption, holder) = if version.is_sealed_key() {
            parse_sealed_encryption(&toml)?
        } else {
            (parse_plain_encryption(&toml)?, None)
        };

        if toml.hash_buckets.iter().sum::<usize>() > 64 {
            return Err(Error::Malformed(format!(
                "hash_buckets {:?} exceed the 64 hex characters of a digest",
                toml.hash_buckets
            )));
        }

        Ok(Config {
            version,
            hash_format: HashFormat::parse(&toml.hash_type_id)?,
            hash_buckets: toml.hash_buckets,
            compression: Compression::parse(&toml.compression_type)?,
            encryption,
            holder,
            multi_hash: !toml.single_hash,
            instance_id: toml.instance_id,
            verify_on_collision: toml.verify_on_collision,
            digest: framed.digest,
        })
    }

    /// The key blobs are encrypted to, as a markl id, if the store encrypts.
    pub fn public_key(&self) -> Option<Id> {
        self.encryption.public_key()
    }

    /// Whether this is a sealed-key store (FDR 0011), with a `blob_store-key`
    /// sidecar beside the config.
    pub fn is_sealed_key(&self) -> bool {
        self.version.is_sealed_key()
    }
}

/// V5: `[key-custody] holder = "process"` and exactly one `age_x25519_pub`
/// id, the store's public key.
fn parse_sealed_encryption(toml: &TomlLocal) -> Result<(Encryption, Option<String>)> {
    let Some(custody) = &toml.key_custody else {
        return Err(Error::Malformed(
            "a sealed-key store config has a [key-custody] table".into(),
        ));
    };
    if custody.holder != KEY_HOLDER_PROCESS {
        return Err(Error::Unsupported(format!(
            "key-custody holder {:?} (this crate supports {KEY_HOLDER_PROCESS:?})",
            custody.holder
        )));
    }

    if toml.encryption.len() != 1 {
        return Err(Error::Malformed(format!(
            "a sealed-key store has exactly one encryption id, its public key; got {}",
            toml.encryption.len()
        )));
    }

    let public_key =
        Id::parse(&toml.encryption[0]).map_err(|e| Error::Markl(format!("encryption id: {e}")))?;
    if public_key.format() != FormatId::AgeX25519Pub {
        return Err(Error::Malformed(format!(
            "a sealed-key store's encryption id must be an age_x25519_pub key, got {}",
            public_key.to_wire()
        )));
    }
    if public_key.data().len() != 32 {
        return Err(Error::Malformed(format!(
            "age_x25519_pub key holds {} bytes, want 32",
            public_key.data().len()
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

    Ok((
        Encryption::Sealed { public_key },
        Some(custody.holder.clone()),
    ))
}

/// V3/V4: no `[key-custody]`; `encryption` is empty or holds one
/// `age_x25519_sec` key. Several keys are Go's age `MultiIdentity` (a file
/// with several stanzas), which this crate does not read.
fn parse_plain_encryption(toml: &TomlLocal) -> Result<Encryption> {
    if toml.key_custody.is_some() {
        return Err(Error::Malformed(
            "a plain store config carries no [key-custody] table (that is the sealed-key store, v5)"
                .into(),
        ));
    }

    match toml.encryption.as_slice() {
        [] => Ok(Encryption::None),
        [one] => {
            let id = Id::parse(one).map_err(|e| Error::Markl(format!("encryption id: {e}")))?;
            if id.format() != FormatId::AgeX25519Sec {
                return Err(Error::Unsupported(format!(
                    "encryption key {} (a plain store's key is an age_x25519_sec identity)",
                    id.to_wire()
                )));
            }
            let mut secret = Zeroizing::new([0u8; 32]);
            if id.data().len() != 32 {
                return Err(Error::Malformed(format!(
                    "age_x25519_sec key holds {} bytes, want 32",
                    id.data().len()
                )));
            }
            secret.copy_from_slice(id.data());
            // A clamped scalar times the base point is never a low-order
            // point, so the recipient check that guards externally supplied
            // public keys has nothing to say here.
            let public = age_x25519::public_key(&secret);
            Ok(Encryption::Key { id, secret, public })
        }
        many => Err(Error::Unsupported(format!(
            "encryption by {} keys (several age identities, Go's MultiIdentity, are not implemented)",
            many.len()
        ))),
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
    const PLAIN_NONE: &[u8] =
        include_bytes!("../tests/fixtures/plain-none-v4/store/blob_store-config");
    const PLAIN_KEY: &[u8] =
        include_bytes!("../tests/fixtures/plain-key-v4/store/blob_store-config");

    #[test]
    fn parses_the_go_written_fixture() {
        let config = Config::parse(FIXTURE).unwrap();
        assert_eq!(config.version, ConfigVersion::V5);
        assert_eq!(config.hash_format, HashFormat::Blake2b256);
        assert_eq!(config.hash_buckets, vec![2]);
        assert_eq!(config.compression, Compression::Zstd);
        assert_eq!(config.holder.as_deref(), Some(KEY_HOLDER_PROCESS));
        assert!(config.multi_hash);
        assert!(!config.verify_on_collision);
        assert!(
            config
                .instance_id
                .as_deref()
                .unwrap()
                .starts_with("uuidv7-")
        );
        assert!(config.encryption.is_sealed());
        assert!(
            config
                .public_key()
                .unwrap()
                .to_wire()
                .starts_with("piggy-recipient-v1@age_x25519_pub-")
        );
        assert!(config.digest.is_some());
    }

    #[test]
    fn parses_the_go_written_plain_fixtures() {
        let none = Config::parse(PLAIN_NONE).unwrap();
        assert_eq!(none.version, ConfigVersion::V4);
        assert!(matches!(none.encryption, Encryption::None));
        assert_eq!(none.public_key(), None);
        assert_eq!(none.encryption.describe(), "");
        assert_eq!(none.holder, None);
        assert_eq!(none.compression, Compression::Zstd);
        assert!(none.digest.is_some());

        let key = Config::parse(PLAIN_KEY).unwrap();
        assert_eq!(key.version, ConfigVersion::V4);
        let Encryption::Key { id, secret, public } = &key.encryption else {
            panic!("want a raw key, got {:?}", key.encryption);
        };
        assert_eq!(id.format(), FormatId::AgeX25519Sec);
        assert_eq!(
            id.purpose(),
            Some(&PurposeId::Other(PURPOSE_MADDER_PRIVATE_KEY_V1.to_owned()))
        );
        assert_eq!(*public, age_x25519::public_key(secret));
        assert!(
            key.encryption
                .describe()
                .starts_with("madder-private_key-v1@age_x25519_sec-")
        );
        assert_eq!(key.public_key().unwrap().format(), FormatId::AgeX25519Pub);
        assert_eq!(key.encryption.recipient(), Some(*public));
        // The Debug form never shows the secret.
        let shown = format!("{:?}", key.encryption);
        assert!(shown.contains("age_x25519_sec"), "{shown}");
        assert!(!shown.contains(&hex(secret.as_slice())), "{shown}");
    }

    fn hex(bytes: &[u8]) -> String {
        bytes.iter().map(|b| format!("{b:02x}")).collect()
    }

    #[test]
    fn a_v3_config_reads_as_a_plain_store() {
        // V3 is V4 without instance-id; Go upgrades it on read. Drop the
        // digest line too, as a legacy config would lack it.
        let text = std::str::from_utf8(PLAIN_NONE).unwrap();
        let v3: String = text
            .lines()
            .filter(|l| !l.starts_with("@ ") && !l.starts_with("instance-id"))
            .map(|l| l.replace(TYPE_V4, TYPE_V3))
            .collect::<Vec<_>>()
            .join("\n")
            + "\n";
        let config = Config::parse(v3.as_bytes()).unwrap();
        assert_eq!(config.version, ConfigVersion::V3);
        assert!(matches!(config.encryption, Encryption::None));
        assert_eq!(config.instance_id, None);
        assert_eq!(config.digest, None);
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

    fn rebuild(type_id: &str, body: &str) -> Vec<u8> {
        format!("---\n! {type_id}\n---\n\n{body}").into_bytes()
    }

    /// A syntactically valid `age_x25519_pub` id whose payload is 31 bytes.
    fn short_key_id() -> String {
        piggy_markl::blech32::encode("age_x25519_pub", &[7u8; 31]).unwrap()
    }

    const SEALED_GOOD: &str = "hash_buckets = [2]\nhash_type-id = \"blake2b256\"\nencryption = [\"piggy-recipient-v1@age_x25519_pub-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy\"]\ncompression-type = \"zstd\"\n\n[key-custody]\nholder = \"process\"\n";

    #[test]
    fn refuses_what_go_refuses_for_a_sealed_store() {
        let good = SEALED_GOOD;
        assert!(Config::parse(&rebuild(TYPE_V5, good)).is_ok());

        for (label, body) in [
            ("unknown holder", good.replace("\"process\"", "\"agent\"")),
            ("no custody", good.replace("\n[key-custody]\nholder = \"process\"\n", "")),
            ("no key", good.replace("encryption = [\"piggy-recipient-v1@age_x25519_pub-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy\"]", "encryption = []")),
            (
                "two keys",
                good.replace("\"]\n", "\", \"piggy-recipient-v1@age_x25519_pub-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy\"]\n"),
            ),
            ("secret key", good.replace("age_x25519_pub-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy", "age_x25519_sec-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy")),
            (
                "a 31-byte public key",
                good.replace(
                    "age_x25519_pub-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy",
                    &short_key_id(),
                ),
            ),
            ("unknown hash", good.replace("blake2b256", "md5")),
            ("unknown compression", good.replace("zstd", "brotli")),
            ("base_path", format!("base_path = \"/elsewhere\"\n{good}")),
        ] {
            assert!(Config::parse(&rebuild(TYPE_V5, &body)).is_err(), "{label}: accepted, want rejection");
        }
    }

    #[test]
    fn refuses_what_is_outside_the_plain_subset() {
        let key = std::str::from_utf8(PLAIN_KEY).unwrap();
        let key_line = key.lines().find(|l| l.starts_with("encryption")).unwrap();
        let key_id = &key_line["encryption = [\"".len()..key_line.len() - 2];
        let good = format!(
            "hash_buckets = [2]\nhash_type-id = \"blake2b256\"\nencryption = [\"{key_id}\"]\ncompression-type = \"zstd\"\n"
        );
        assert!(Config::parse(&rebuild(TYPE_V4, &good)).is_ok());

        for (label, body) in [
            (
                "two keys (MultiIdentity)",
                good.replace("\"]\n", &format!("\", \"{key_id}\"]\n")),
            ),
            (
                "a public key where an identity belongs",
                good.replace(key_id, "piggy-recipient-v1@age_x25519_pub-3aqvttdk3ujkyjh9kg2w5an6dmy5mq5a84a4uxk3hfhnugfc9p0sp50zfy"),
            ),
            ("key-custody on a plain store", format!("{good}\n[key-custody]\nholder = \"process\"\n")),
        ] {
            assert!(
                Config::parse(&rebuild(TYPE_V4, &body)).is_err(),
                "{label}: accepted, want rejection"
            );
        }
        for old in ["toml-blob_store_config-v1", "toml-blob_store_config-v2"] {
            assert!(matches!(
                Config::parse(&rebuild(
                    old,
                    "hash_buckets = [2]\nhash_type-id = \"sha256\"\n"
                )),
                Err(Error::Unsupported(_))
            ));
        }
    }

    #[test]
    fn a_hand_written_v3_with_an_empty_hash_type_fails_on_the_hash_type() {
        // init.bats's broken-store fixture: `encryption` spelled as a
        // string, `hash-buckets` misspelled, `hash_type-id` empty. Go
        // reports the hash type; so does this crate.
        let raw = rebuild(
            TYPE_V3,
            "hash_type-id = \"\"\ncompression-type = \"zstd\"\nencryption = \"\"\nhash-buckets = [2]\n",
        );
        let err = Config::parse(&raw).unwrap_err();
        assert!(err.to_string().contains("unsupported hash type"), "{err}");

        // The same spellings with a real hash type read as a plain store
        // without bucketing.
        let raw = rebuild(
            TYPE_V3,
            "hash_type-id = \"sha256\"\ncompression-type = \"zstd\"\nencryption = \"\"\n",
        );
        let config = Config::parse(&raw).unwrap();
        assert!(matches!(config.encryption, Encryption::None));
        assert!(config.hash_buckets.is_empty());
    }

    #[test]
    fn refuses_other_config_versions() {
        let raw = b"---\n! toml-blob_store_config-v9\n---\n\nhash_buckets = [2]\n";
        assert!(matches!(Config::parse(raw), Err(Error::Unsupported(_))));
        let multi = b"---\n! toml-blob_store_config-multi-v1\n---\n\n";
        assert!(matches!(Config::parse(multi), Err(Error::Unsupported(_))));
    }
}
