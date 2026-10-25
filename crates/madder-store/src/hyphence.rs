//! The hyphence framing the two store files share: `---` boundaries, a
//! `! type` line, an optional `@ digest` line, other metadata lines
//! ignored, and a body after a blank separator line.
//!
//! This is deliberately not a hyphence implementation (that is
//! hyphence's lane): it reads exactly what `blob_store-config` and
//! `blob_store-key` carry and refuses anything else.

use crate::{Error, Result};

pub const BOUNDARY: &[u8] = b"---\n";

/// A framed store file: its type id, its optional digest line, and its
/// body (the bytes after the blank separator, empty when there is none).
#[derive(Debug, PartialEq, Eq)]
pub struct Framed<'a> {
    pub type_id: String,
    pub digest: Option<String>,
    pub body: &'a [u8],
}

pub fn parse(raw: &[u8]) -> Result<Framed<'_>> {
    let mut pos = 0usize;

    let first = next_line(raw, &mut pos).ok_or_else(|| Error::Framing("empty file".into()))?;
    if first != BOUNDARY {
        return Err(Error::Framing("missing opening --- boundary".into()));
    }

    let mut type_id: Option<String> = None;
    let mut digest: Option<String> = None;
    let mut closed = false;

    while let Some(line) = next_line(raw, &mut pos) {
        if line == BOUNDARY {
            closed = true;
            break;
        }

        let line = line.strip_suffix(b"\n").unwrap_or(line);
        if line.len() < 2 || line[1] != b' ' {
            return Err(Error::Framing(format!(
                "malformed metadata line: {:?}",
                String::from_utf8_lossy(line)
            )));
        }

        let content = std::str::from_utf8(&line[2..])
            .map_err(|_| Error::Framing("metadata line is not UTF-8".into()))?;

        match line[0] {
            b'!' => {
                if type_id.is_some() {
                    return Err(Error::Framing("more than one ! type line".into()));
                }
                type_id = Some(content.to_owned());
            }
            b'@' => {
                if digest.is_some() {
                    return Err(Error::Framing("more than one @ digest line".into()));
                }
                digest = Some(content.to_owned());
            }
            b'#' | b'-' | b'%' | b'<' => {}
            other => {
                return Err(Error::Framing(format!(
                    "unknown metadata prefix {:?}",
                    other as char
                )));
            }
        }
    }

    if !closed {
        return Err(Error::Framing("missing closing --- boundary".into()));
    }

    let type_id = type_id.ok_or_else(|| Error::Framing("missing ! type line".into()))?;

    let rest = &raw[pos..];
    let body = if rest.is_empty() {
        rest
    } else if rest[0] == b'\n' {
        &rest[1..]
    } else {
        return Err(Error::Framing(
            "body present without the blank separator line".into(),
        ));
    };

    Ok(Framed {
        type_id,
        digest,
        body,
    })
}

/// The bytes after the LAST `---\n\n` in the file. This is what the Go
/// implementation hashes for the config's `@` digest
/// (`blob_store_configs.DecodeAndVerify` uses `bytes.LastIndex`), so a
/// digest check must use this and not `Framed::body`; the two agree for
/// every real config, since a TOML config body never contains `---`.
pub fn body_after_last_separator(raw: &[u8]) -> Option<&[u8]> {
    const SEPARATOR: &[u8] = b"---\n\n";
    let at = raw
        .windows(SEPARATOR.len())
        .rposition(|window| window == SEPARATOR)?;
    Some(&raw[at + SEPARATOR.len()..])
}

fn next_line<'a>(buf: &'a [u8], pos: &mut usize) -> Option<&'a [u8]> {
    if *pos >= buf.len() {
        return None;
    }
    let start = *pos;
    match buf[start..].iter().position(|&b| b == b'\n') {
        Some(i) => {
            *pos = start + i + 1;
            Some(&buf[start..*pos])
        }
        None => {
            *pos = buf.len();
            Some(&buf[start..])
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_a_config_shaped_file() {
        let raw = b"---\n! toml-blob_store_config-v5\n@ blake2b256-abc\n---\n\nkey = 1\n";
        let framed = parse(raw).unwrap();
        assert_eq!(framed.type_id, "toml-blob_store_config-v5");
        assert_eq!(framed.digest.as_deref(), Some("blake2b256-abc"));
        assert_eq!(framed.body, b"key = 1\n");
        assert_eq!(body_after_last_separator(raw), Some(&b"key = 1\n"[..]));
    }

    #[test]
    fn parses_a_sidecar_whose_body_starts_with_a_blank_line() {
        let raw = b"---\n! toml-blob_store_key-v2\n---\n\n\n[recipients]\n";
        let framed = parse(raw).unwrap();
        assert_eq!(framed.type_id, "toml-blob_store_key-v2");
        assert_eq!(framed.digest, None);
        assert_eq!(framed.body, b"\n[recipients]\n");
    }

    #[test]
    fn a_header_only_file_has_an_empty_body() {
        let framed = parse(b"---\n! t\n---\n").unwrap();
        assert_eq!(framed.body, b"");
    }

    #[test]
    fn rejects_malformed_framing() {
        for (label, raw) in [
            ("empty", &b""[..]),
            ("no opening boundary", b"! t\n---\n"),
            ("no closing boundary", b"---\n! t\n"),
            ("no type line", b"---\n# only a comment\n---\n"),
            ("two type lines", b"---\n! a\n! b\n---\n"),
            ("two digest lines", b"---\n@ a\n@ b\n! t\n---\n"),
            ("bad prefix", b"---\n? what\n! t\n---\n"),
            ("no space after prefix", b"---\n!t\n---\n"),
            ("body without separator", b"---\n! t\n---\nbody\n"),
        ] {
            assert!(parse(raw).is_err(), "{label}: accepted, want rejection");
        }
    }

    #[test]
    fn last_separator_wins_for_the_digest_body() {
        let raw = b"---\n! t\n---\n\nx = \"---\n\n\"\n";
        assert_eq!(body_after_last_separator(raw), Some(&b"\"\n"[..]));
    }
}
