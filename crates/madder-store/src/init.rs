//! Creating a new sealed-key store, as `madder init -pigpen` does: mint
//! an X25519 store key, seal it to a recipient set, write the sidecar and
//! then the digest-pinned config (FDR 0011). pigsty on Android has no Go
//! madder to shell out to, so this lives here.
//!
//! The TOML these files carry is not byte-identical to what Go's tommy
//! writes and need not be: the config's `@` digest pins the bytes written
//! here, and Go decodes any valid TOML.

use std::path::Path;

use blake2::Blake2b;
use blake2::digest::consts::U32;
use piggy_markl::{FormatId, Id, blech32};
use piggy_pigpen::Document;
use rand_core::{OsRng, RngCore};
use serde::Serialize;
use sha2::Digest;
use zeroize::Zeroizing;

use crate::age_x25519;
use crate::blob_id::HashFormat;
use crate::codec::Compression;
use crate::config::{Config, KEY_HOLDER_PROCESS, TYPE_V5};
use crate::fs::Fs;
use crate::sidecar::{
    RecipientSource, SOURCE_KIND_EMBEDDED, SOURCE_KIND_PAPI, SOURCE_KIND_PATH, Sidecar, TYPE_V2,
    encode_sealed_document,
};
use crate::store::{FILE_NAME_CONFIG, FILE_NAME_KEY, OpenOptions, Store};
use crate::{Error, Result};

/// What a new store looks like. The defaults are `madder init`'s.
pub struct InitOptions {
    pub hash_format: HashFormat,
    pub hash_buckets: Vec<usize>,
    pub compression: Compression,
    /// The recipients the store key is sealed to, as `piggy-recipient-v1@…`
    /// ids (an X25519 or P-256 key each). At least one.
    pub recipients: Vec<Id>,
    /// Where those recipients came from, recorded in the sidecar. For a
    /// `path` source the `pigpen` snapshot is rendered here when absent.
    pub source: RecipientSource,
}

impl InitOptions {
    /// `madder init -pigpen <file> -pigpen-kind embedded`: the pigpen text
    /// becomes the sidecar's own pigpen, nothing host-specific is recorded.
    pub fn embedded(pigpen_text: &str, recipients: Vec<Id>) -> Self {
        InitOptions {
            hash_format: HashFormat::Blake2b256,
            hash_buckets: vec![2],
            compression: Compression::Zstd,
            recipients,
            source: RecipientSource {
                kind: SOURCE_KIND_EMBEDDED.to_owned(),
                locator: None,
                pigpen: Some(pigpen_text.to_owned()),
            },
        }
    }
}

/// Parse a piggy-ids file in its simplest form, one recipient id per line
/// (blank lines and `#` comments skipped), or a pigpen-v1 recipient set.
/// Returns the encryption recipients. A pointer document is refused: this
/// crate runs no resolver.
pub fn parse_recipients(pigpen_text: &str) -> Result<Vec<Id>> {
    let trimmed = pigpen_text.trim_start();
    if trimmed.starts_with("---") {
        let doc = Document::parse(pigpen_text.as_bytes()).map_err(|e| {
            Error::Malformed(format!(
                "pigpen document: {e} (a pigpen pointer cannot be resolved here)"
            ))
        })?;
        if doc.sealed() {
            return Err(Error::Malformed(
                "pigpen is a sealed document, not a recipient set".into(),
            ));
        }
        return Ok(doc.encryption_recipients().map(|r| r.id.clone()).collect());
    }

    let mut recipients = Vec::new();
    for line in pigpen_text.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let id_text = line.split_whitespace().next().unwrap_or(line);
        let id = Id::parse(id_text).map_err(|e| Error::Markl(format!("{id_text:?}: {e}")))?;
        if !matches!(
            id.format(),
            FormatId::AgeX25519Pub | FormatId::PivyEcdhP256Pub
        ) {
            return Err(Error::Unsupported(format!(
                "{} is not an encryption recipient",
                id.to_wire()
            )));
        }
        recipients.push(id);
    }
    Ok(recipients)
}

/// The sidecar body, rendered by hand so its two long fields are TOML
/// basic multi-line strings (`"""`), the shape Go's tommy writes and the
/// shape a reader greps for; a generic serializer may pick a literal
/// string instead.
fn render_sidecar_body(
    kind: &str,
    locator: Option<&str>,
    pigpen: Option<&str>,
    document: &str,
) -> Result<String> {
    let mut out = String::new();
    out.push_str("[recipients]\n");
    out.push_str(&format!("source_kind = {}\n", toml_basic_string(kind)?));
    if let Some(locator) = locator {
        out.push_str(&format!("source = {}\n", toml_basic_string(locator)?));
    }
    if let Some(pigpen) = pigpen {
        out.push_str(&format!("pigpen = {}\n", toml_multiline_string(pigpen)?));
    }
    out.push_str("\n[sealed]\n");
    out.push_str(&format!(
        "document = {}\n",
        toml_multiline_string(document)?
    ));
    Ok(out)
}

/// A TOML basic string, `"…"`, with the escapes TOML needs.
fn toml_basic_string(s: &str) -> Result<String> {
    let mut out = String::from("\"");
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 || c == '\u{7f}' => {
                return Err(Error::Malformed(format!(
                    "control character {:?} in a sidecar string",
                    c
                )));
            }
            c => out.push(c),
        }
    }
    out.push('"');
    Ok(out)
}

/// A TOML basic multi-line string, `"""\n…"""`: the opening newline is
/// trimmed by TOML, so the value is exactly `s`. Backslashes and any `"""`
/// inside are escaped; control characters other than newline and tab are
/// refused rather than written.
fn toml_multiline_string(s: &str) -> Result<String> {
    let mut out = String::from("\"\"\"\n");
    let mut quotes = 0;
    for c in s.chars() {
        match c {
            '"' => {
                quotes += 1;
                if quotes == 3 {
                    out.push_str("\\\"");
                    quotes = 0;
                } else {
                    out.push('"');
                }
                continue;
            }
            '\\' => out.push_str("\\\\"),
            '\n' | '\t' => out.push(c),
            c if (c as u32) < 0x20 || c == '\u{7f}' => {
                return Err(Error::Malformed(format!(
                    "control character {:?} in a sidecar string",
                    c
                )));
            }
            c => out.push(c),
        }
        quotes = 0;
    }
    // A closing quote right before the delimiter would merge with it.
    if s.ends_with('"') {
        out.push('\\');
        out.push('\n');
    }
    out.push_str("\"\"\"");
    Ok(out)
}

#[derive(Serialize)]
struct ConfigToml<'a> {
    hash_buckets: &'a [usize],
    #[serde(rename = "hash_type-id")]
    hash_type_id: &'a str,
    encryption: [String; 1],
    #[serde(rename = "compression-type")]
    compression_type: &'a str,
    #[serde(rename = "instance-id")]
    instance_id: String,
    #[serde(rename = "key-custody")]
    key_custody: KeyCustodyToml<'a>,
}

#[derive(Serialize)]
struct KeyCustodyToml<'a> {
    holder: &'a str,
}

/// Create the store at `base` (which must not already hold one), then open
/// it. The sidecar is written before the config, as Go does: an
/// interrupted init leaves a stray sidecar and no store, never a store
/// whose key is gone.
pub fn init(base: impl AsRef<Path>, options: InitOptions, open: OpenOptions) -> Result<Store> {
    let base = base.as_ref();
    let fs: &dyn Fs = &*open.fs;

    if options.recipients.is_empty() {
        return Err(Error::Malformed(
            "no encryption recipients; a key sealed to nobody could never be opened".into(),
        ));
    }
    match options.source.kind.as_str() {
        SOURCE_KIND_PATH | SOURCE_KIND_PAPI | SOURCE_KIND_EMBEDDED => {}
        other => return Err(Error::Unsupported(format!("pigpen source kind {other:?}"))),
    }
    if options.hash_buckets.iter().sum::<usize>() > 64 {
        return Err(Error::Malformed(format!(
            "hash_buckets {:?} exceed the 64 hex characters of a digest",
            options.hash_buckets
        )));
    }

    let config_path = base.join(FILE_NAME_CONFIG);
    if fs.exists(&config_path) {
        return Err(Error::Malformed(format!(
            "a blob store already exists at {}",
            config_path.display()
        )));
    }

    // The store key.
    let mut secret = Zeroizing::new([0u8; 32]);
    OsRng.fill_bytes(&mut *secret);
    let public = age_x25519::public_key(&secret);
    age_x25519::check_recipient(&public)?;
    let public_id = Id::new(
        Some(piggy_markl::PurposeId::PiggyRecipientV1),
        FormatId::AgeX25519Pub,
        public.to_vec(),
    )
    .map_err(|e| Error::Markl(e.to_string()))?;

    let sealed = Document::seal(&*secret, &options.recipients)?;
    drop(secret);
    let sealed_raw = sealed.to_bytes()?;

    // The sidecar. A path source gets a snapshot of the recipients, so the
    // sidecar says who the pigpen named when the key was sealed.
    let snapshot;
    let pigpen = match (&options.source.pigpen, options.source.kind.as_str()) {
        (Some(text), _) => Some(text.as_str()),
        (None, SOURCE_KIND_PATH) => {
            snapshot = render_recipient_set(&options.recipients)?;
            Some(snapshot.as_str())
        }
        (None, _) => None,
    };
    let sidecar_body = render_sidecar_body(
        &options.source.kind,
        options.source.locator.as_deref(),
        pigpen,
        &encode_sealed_document(&sealed_raw),
    )?;
    let sidecar_bytes = format!("---\n! {TYPE_V2}\n---\n\n{sidecar_body}").into_bytes();

    // Refuse to write a sidecar that does not read back as what was meant.
    let read_back = Sidecar::parse(&sidecar_bytes)?;
    if read_back.sealed_raw != sealed_raw
        || read_back.source != options.source && options.source.pigpen.is_some()
    {
        return Err(Error::Malformed(
            "the store key sidecar does not read back as written; refusing to write it".into(),
        ));
    }

    // The config, digest-pinned over exactly the body bytes written.
    let config_body = toml::to_string(&ConfigToml {
        hash_buckets: &options.hash_buckets,
        hash_type_id: options.hash_format.as_str(),
        encryption: [public_id.to_wire()],
        compression_type: options.compression.as_str(),
        instance_id: mint_instance_id(),
        key_custody: KeyCustodyToml {
            holder: KEY_HOLDER_PROCESS,
        },
    })
    .map_err(|e| Error::Malformed(format!("rendering the config: {e}")))?;
    let digest = Blake2b::<U32>::digest(config_body.as_bytes());
    let digest_id =
        blech32::encode("blake2b256", &digest).map_err(|e| Error::Markl(e.to_string()))?;
    let config_bytes =
        format!("---\n! {TYPE_V5}\n@ {digest_id}\n---\n\n{config_body}").into_bytes();
    Config::parse(&config_bytes)?;

    fs.create_dir_all(base)?;
    fs.write_new(&base.join(FILE_NAME_KEY), &sidecar_bytes)?;
    fs.write_new(&config_path, &config_bytes)?;
    fs.sync_dir(base)?;

    Store::open_with(
        base,
        OpenOptions {
            temp_dir: open.temp_dir,
            fs: open.fs,
        },
    )
}

/// A pigpen-v1 recipient set naming `recipients`, as Go's
/// `store_key.Snapshot` embeds for a `path` source.
fn render_recipient_set(recipients: &[Id]) -> Result<String> {
    let doc = Document::new_recipient_set(
        recipients
            .iter()
            .map(|id| piggy_pigpen::Recipient {
                id: id.clone(),
                comment: None,
                wrap: None,
            })
            .collect(),
    );
    let bytes = doc.to_bytes()?;
    String::from_utf8(bytes).map_err(|_| Error::Malformed("recipient set is not UTF-8".into()))
}

/// A fresh RFC 9562 UUIDv7 as the `uuidv7-<blech32>` markl id Go mints
/// (`markl_registrations.MintInstanceId`): 48-bit big-endian Unix
/// milliseconds, then random, with the version and variant bits set.
fn mint_instance_id() -> String {
    let mut bytes = [0u8; 16];
    OsRng.fill_bytes(&mut bytes[6..]);
    let ms = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or(0);
    bytes[..6].copy_from_slice(&ms.to_be_bytes()[2..]);
    bytes[6] = (bytes[6] & 0x0f) | 0x70;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    blech32::encode("uuidv7", &bytes).expect("uuidv7 is a valid HRP")
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::store::Unsealer;
    use piggy_pigpen::X25519Identity;

    fn scratch() -> std::path::PathBuf {
        let path = std::env::temp_dir().join(format!(
            "madder-store-init-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        std::fs::create_dir_all(&path).unwrap();
        path
    }

    fn identity(seed: u8) -> (Id, X25519Identity) {
        let secret = [seed; 32];
        let public = age_x25519::public_key(&secret);
        let id = piggy_pigpen::recipient_id(FormatId::AgeX25519Pub, public.to_vec()).unwrap();
        (
            id,
            X25519Identity {
                public: public.to_vec(),
                secret: secret.to_vec(),
            },
        )
    }

    #[test]
    fn a_new_store_opens_writes_and_reads_back() {
        let dir = scratch();
        let (recipient, ident) = identity(5);
        let pigpen = format!("{}\n", recipient.to_wire());
        let recipients = parse_recipients(&pigpen).unwrap();
        let store = init(
            dir.join("store"),
            InitOptions::embedded(&pigpen, recipients),
            OpenOptions::default(),
        )
        .unwrap();

        assert_eq!(store.sealed_recipients(), vec![recipient]);
        assert_eq!(store.recipient_source().kind, SOURCE_KIND_EMBEDDED);
        assert_eq!(
            store.recipient_source().pigpen.as_deref(),
            Some(pigpen.as_str())
        );
        assert!(
            store
                .config()
                .instance_id
                .as_deref()
                .unwrap()
                .starts_with("uuidv7-")
        );
        assert!(store.config().digest.is_some());

        let id = store
            .write_bytes(b"first blob in a rust-made store")
            .unwrap();
        store
            .unseal(&Unsealer {
                oracle: None,
                identities: &[ident],
            })
            .unwrap();
        assert_eq!(
            store.read_to_vec(&id).unwrap(),
            b"first blob in a rust-made store"
        );

        // Reopening reads the same two files back.
        let again = Store::open(dir.join("store")).unwrap();
        assert_eq!(again.public_key(), store.public_key());
        assert!(again.has(&id));

        // A second init must not replace the key.
        let recipients = parse_recipients(&pigpen).unwrap();
        assert!(
            init(
                dir.join("store"),
                InitOptions::embedded(&pigpen, recipients),
                OpenOptions::default()
            )
            .is_err()
        );
        assert_eq!(
            Store::open(dir.join("store")).unwrap().public_key(),
            store.public_key()
        );

        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn a_path_source_gets_a_recipient_snapshot() {
        let dir = scratch();
        let (recipient, _) = identity(6);
        let options = InitOptions {
            source: RecipientSource {
                kind: SOURCE_KIND_PATH.to_owned(),
                locator: Some("/home/me/piggy-ids".to_owned()),
                pigpen: None,
            },
            ..InitOptions::embedded("", vec![recipient.clone()])
        };
        let store = init(dir.join("store"), options, OpenOptions::default()).unwrap();
        let source = store.recipient_source();
        assert_eq!(source.kind, SOURCE_KIND_PATH);
        assert_eq!(source.locator.as_deref(), Some("/home/me/piggy-ids"));
        let snapshot = source.pigpen.as_deref().unwrap();
        assert!(snapshot.contains("! pigpen-v1"));
        assert!(snapshot.contains(&recipient.to_wire()));
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn refuses_a_store_sealed_to_nobody_and_unknown_recipients() {
        let dir = scratch();
        assert!(
            init(
                dir.join("store"),
                InitOptions::embedded("", vec![]),
                OpenOptions::default()
            )
            .is_err()
        );
        assert!(!dir.join("store").exists());
        assert!(
            parse_recipients("sha256-qqqsyqcyq5rqwzqfpg9scrgwpugpzysnzs23v9ccrydpk8qarc0s7lcgm6\n")
                .is_err()
        );
        assert!(parse_recipients("not an id\n").is_err());
        assert!(
            parse_recipients(
                "---\n- kind=\"papi-http\"\n- locator=\"example.test\"\n! pigpen-pointer-v1\n---\n"
            )
            .is_err()
        );
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn sidecar_strings_render_as_basic_multiline_and_round_trip() {
        for text in [
            "piggy-recipient-v1@age_x25519_pub-abc\n",
            "has a \\ backslash and \"quotes\" and \"\"\" three\n",
            "ends with a quote\"",
            "",
        ] {
            let body = render_sidecar_body("embedded", None, Some(text), "QUJD\n").unwrap();
            assert!(body.contains("pigpen = \"\"\"\n"), "{body}");
            assert!(body.contains("document = \"\"\"\n"), "{body}");
            let parsed: toml::Value = toml::from_str(&body).unwrap();
            assert_eq!(
                parsed["recipients"]["pigpen"].as_str(),
                Some(text),
                "{body}"
            );
            assert_eq!(parsed["sealed"]["document"].as_str(), Some("QUJD\n"));
        }
        let body = render_sidecar_body("path", Some("/tmp/a \"b\"/ids"), None, "QUJD\n").unwrap();
        let parsed: toml::Value = toml::from_str(&body).unwrap();
        assert_eq!(
            parsed["recipients"]["source"].as_str(),
            Some("/tmp/a \"b\"/ids")
        );
        assert!(parsed["recipients"].get("pigpen").is_none());
        assert!(render_sidecar_body("embedded", None, Some("bell\u{7}"), "x").is_err());
    }

    #[test]
    fn parses_the_fixture_piggy_ids_file() {
        let text = include_str!("../tests/fixtures/zstd-blake2b256/piggy-ids");
        let recipients = parse_recipients(text).unwrap();
        assert_eq!(recipients.len(), 2);
        assert_eq!(recipients[0].format(), FormatId::AgeX25519Pub);
        assert_eq!(recipients[1].format(), FormatId::PivyEcdhP256Pub);
    }
}
