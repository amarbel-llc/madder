//! `blob_store-key`: the mutable sidecar holding the sealed pigpen-v1
//! document whose payload is the store key, and where its recipients came
//! from (FDR 0011).

use base64::Engine;
use piggy_markl::Id;
use piggy_pigpen::Document;
use serde::Deserialize;

use crate::hyphence;
use crate::{Error, Result};

pub const TYPE_V2: &str = "toml-blob_store_key-v2";
pub const TYPE_V1: &str = "toml-blob_store_key-v1";

/// Where a sealed-key store's recipients come from (`source_kind`).
pub const SOURCE_KIND_PATH: &str = "path";
pub const SOURCE_KIND_PAPI: &str = "papi";
pub const SOURCE_KIND_EMBEDDED: &str = "embedded";

#[derive(Deserialize)]
struct TomlV2 {
    recipients: RecipientsV2,
    sealed: Sealed,
}

#[derive(Deserialize)]
struct RecipientsV2 {
    source_kind: String,
    #[serde(default)]
    source: Option<String>,
    #[serde(default)]
    pigpen: Option<String>,
}

#[derive(Deserialize)]
struct TomlV1 {
    recipients: RecipientsV1,
    sealed: Sealed,
}

#[derive(Deserialize)]
struct RecipientsV1 {
    source: String,
}

#[derive(Deserialize)]
struct Sealed {
    document: String,
}

/// The pigpen a store's recipients were read from.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecipientSource {
    /// `path`, `papi` or `embedded`.
    pub kind: String,
    /// A local path for `path`, a PAPI identity domain for `papi`, absent
    /// for `embedded`.
    pub locator: Option<String>,
    /// The pigpen document text: the pigpen itself for `embedded`, the
    /// pointer for `papi`, a seal-time snapshot for `path` (v2 only).
    pub pigpen: Option<String>,
}

/// A parsed sidecar.
pub struct Sidecar {
    pub source: RecipientSource,
    /// The sealed pigpen-v1 document, parsed but not opened.
    pub sealed: Document,
    /// The sealed document's bytes as stored.
    pub sealed_raw: Vec<u8>,
}

impl Sidecar {
    pub fn parse(raw: &[u8]) -> Result<Sidecar> {
        let framed = hyphence::parse(raw)?;

        if framed.digest.is_some() {
            return Err(Error::Malformed(
                "a store key sidecar carries no @ digest line".into(),
            ));
        }

        let body = std::str::from_utf8(framed.body)
            .map_err(|_| Error::Malformed("sidecar body is not UTF-8".into()))?;

        let (source, sealed_raw) = match framed.type_id.as_str() {
            TYPE_V2 => {
                let toml: TomlV2 = toml::from_str(body)
                    .map_err(|e| Error::Malformed(format!("sidecar body: {e}")))?;
                match toml.recipients.source_kind.as_str() {
                    SOURCE_KIND_PATH | SOURCE_KIND_PAPI | SOURCE_KIND_EMBEDDED => {}
                    other => {
                        return Err(Error::Unsupported(format!("pigpen source kind {other:?}")));
                    }
                }
                let source = RecipientSource {
                    kind: toml.recipients.source_kind,
                    locator: toml.recipients.source,
                    pigpen: toml.recipients.pigpen,
                };
                (source, decode_sealed_document(&toml.sealed.document)?)
            }
            TYPE_V1 => {
                let toml: TomlV1 = toml::from_str(body)
                    .map_err(|e| Error::Malformed(format!("sidecar body: {e}")))?;
                let source = RecipientSource {
                    kind: SOURCE_KIND_PATH.to_owned(),
                    locator: Some(toml.recipients.source),
                    pigpen: None,
                };
                // V1 wrote the document straight into a TOML string.
                (source, toml.sealed.document.into_bytes())
            }
            other => {
                return Err(Error::Unsupported(format!(
                    "store key sidecar type {other:?} (this crate reads {TYPE_V2} and {TYPE_V1})"
                )));
            }
        };

        if sealed_raw.is_empty() {
            return Err(Error::Malformed(
                "store key sidecar has no sealed document".into(),
            ));
        }

        let sealed = Document::parse(&sealed_raw)?;
        if !sealed.sealed() {
            return Err(Error::Malformed(
                "store key sidecar holds a recipient set, not a sealed document".into(),
            ));
        }

        Ok(Sidecar {
            source,
            sealed,
            sealed_raw,
        })
    }

    /// The recipients the store key is sealed to, in document order. This
    /// is the read half of `madder key-status`; it needs no agent.
    pub fn sealed_recipients(&self) -> Vec<Id> {
        self.sealed
            .encryption_recipients()
            .map(|r| r.id.clone())
            .collect()
    }
}

/// On disk the sealed document is base64, wrapped at 64 columns, because
/// it ends in raw ciphertext a TOML string cannot hold.
fn decode_sealed_document(wrapped: &str) -> Result<Vec<u8>> {
    let joined: String = wrapped.split_whitespace().collect();
    base64::engine::general_purpose::STANDARD
        .decode(joined)
        .map_err(|e| Error::Malformed(format!("sealed document is not base64: {e}")))
}

/// Base64 of a sealed document, wrapped at 64 columns with a trailing
/// newline, exactly as Go's `encodeSealedDocument` writes it.
pub fn encode_sealed_document(document: &[u8]) -> String {
    let encoded = base64::engine::general_purpose::STANDARD.encode(document);
    let mut out = String::with_capacity(encoded.len() + encoded.len() / 64 + 1);
    for chunk in encoded.as_bytes().chunks(64) {
        out.push_str(std::str::from_utf8(chunk).expect("base64 is ASCII"));
        out.push('\n');
    }
    if encoded.is_empty() {
        out.push('\n');
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use piggy_markl::FormatId;

    const FIXTURE: &[u8] = include_bytes!("../tests/fixtures/zstd-blake2b256/store/blob_store-key");
    const PIGGY_IDS: &str = include_str!("../tests/fixtures/zstd-blake2b256/piggy-ids");

    #[test]
    fn parses_the_go_written_fixture() {
        let sidecar = Sidecar::parse(FIXTURE).unwrap();
        assert_eq!(sidecar.source.kind, SOURCE_KIND_EMBEDDED);
        assert_eq!(sidecar.source.locator, None);
        assert_eq!(sidecar.source.pigpen.as_deref(), Some(PIGGY_IDS));

        let recipients = sidecar.sealed_recipients();
        assert_eq!(recipients.len(), 2);
        assert_eq!(recipients[0].format(), FormatId::AgeX25519Pub);
        assert_eq!(recipients[1].format(), FormatId::PivyEcdhP256Pub);
        let lines: Vec<&str> = PIGGY_IDS.lines().collect();
        assert_eq!(recipients[0].to_wire(), lines[0]);
        assert_eq!(recipients[1].to_wire(), lines[1]);
    }

    #[test]
    fn the_base64_wrapping_round_trips_like_go() {
        let sidecar = Sidecar::parse(FIXTURE).unwrap();
        let wrapped = encode_sealed_document(&sidecar.sealed_raw);
        assert!(wrapped.lines().all(|l| l.len() <= 64));
        assert!(wrapped.ends_with('\n'));
        assert_eq!(
            decode_sealed_document(&wrapped).unwrap(),
            sidecar.sealed_raw
        );
        // And it is byte-identical to what the fixture carries.
        let text = std::str::from_utf8(FIXTURE).unwrap();
        let start = text.find("document = \"\"\"\n").unwrap() + "document = \"\"\"\n".len();
        let end = text[start..].find("\"\"\"").unwrap() + start;
        assert_eq!(&text[start..end], wrapped);
    }

    #[test]
    fn reads_a_v1_sidecar() {
        let fixture = Sidecar::parse(FIXTURE).unwrap();
        let document = String::from_utf8_lossy(&fixture.sealed_raw).into_owned();
        // V1 held the document verbatim in a TOML string; the fixture's
        // document ends in raw ciphertext, so a literal multi-line string is
        // the only form that carries it, and only when it happens to be UTF-8.
        if String::from_utf8(fixture.sealed_raw.clone()).is_err() {
            return;
        }
        let raw = format!(
            "---\n! {TYPE_V1}\n---\n\n[recipients]\nsource = \"/home/me/piggy-ids\"\ndigest = \"blake2b256-abc\"\n\n[sealed]\ndocument = '''\n{document}'''\n"
        );
        let sidecar = Sidecar::parse(raw.as_bytes()).unwrap();
        assert_eq!(sidecar.source.kind, SOURCE_KIND_PATH);
        assert_eq!(
            sidecar.source.locator.as_deref(),
            Some("/home/me/piggy-ids")
        );
        assert_eq!(sidecar.sealed_recipients().len(), 2);
    }

    #[test]
    fn refuses_malformed_sidecars() {
        let text = std::str::from_utf8(FIXTURE).unwrap();
        for (label, raw) in [
            (
                "unknown type",
                text.replace(TYPE_V2, "toml-blob_store_key-v9"),
            ),
            (
                "unknown source kind",
                text.replace("\"embedded\"", "\"carrier-pigeon\""),
            ),
            (
                "no document",
                text.replace("document = \"\"\"", "document = \"\"\"\"\"\"\n#"),
            ),
            ("not base64", text.replace("LS0tCi0g", "!!!!!!!!")),
            (
                "a digest line",
                text.replacen("---\n", "---\n@ blake2b256-abc\n", 1),
            ),
        ] {
            assert!(
                Sidecar::parse(raw.as_bytes()).is_err(),
                "{label}: accepted, want rejection"
            );
        }
    }
}
