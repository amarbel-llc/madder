//! A test-only command line over the madder-store crate that speaks the
//! subset of `madder`'s grammar the sealed-key store tests use, so the
//! existing bats files run against the Rust implementation with
//! `MADDER_BIN` pointed here (zz-tests_bats/pigpen_store.bats, tests tagged
//! `rust_cli`; zz-tests_bats/pigpen_rust_interop.bats). Not shipped; pigsty
//! talks to the crate, not to this.
//!
//! Supported: `init -pigpen`, `write`, `cat`, `cat-ids`, `fsck`,
//! `key-status`, plus the shim-only `mint-recipient`. Only CWD-relative
//! blob-store-ids (`.name`, resolved to
//! `$PWD/.madder/local/share/blob_stores/name`) are understood.
//!
//! Opening the sealed key: with `MADDER_STORE_IDENTITY_HEX` set, the X25519
//! secret it holds; otherwise piggy-agent through piggy-pigpen's pure-Rust
//! `agent` oracle, with the socket resolved as Go does (`PIGGY_AUTH_SOCK`,
//! `SSH_AUTH_SOCK`, `PIVY_AUTH_SOCK`).

use std::collections::BTreeMap;
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::process::ExitCode;

use madder_store::piggy_markl::{FormatId, Id};
use madder_store::{BlobId, Error, Store, Unsealer, X25519Identity};

type CliResult<T> = Result<T, Box<dyn std::error::Error>>;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match run(&args) {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            eprintln!("madder-store-cli: {e}");
            ExitCode::FAILURE
        }
    }
}

const USAGE: &str = "usage (the madder grammar this test shim speaks):
  madder-store-cli init -pigpen <piggy-ids> [-pigpen-kind path|embedded]
                        [-compression-type none|zstd] [-hash_type-id sha256|blake2b256] <.store>
  madder-store-cli write [-format tap|ndjson] <.store> <file>...
  madder-store-cli cat <.store> <id>...
  madder-store-cli cat-ids <.store>
  madder-store-cli fsck [-format tap|ndjson] <.store>
  madder-store-cli key-status <.store>
  madder-store-cli mint-recipient            print a fresh X25519 recipient id and its secret hex
Reading opens the sealed key with MADDER_STORE_IDENTITY_HEX (the store's X25519 secret) when
set, else through piggy-agent (PIGGY_AUTH_SOCK, SSH_AUTH_SOCK, PIVY_AUTH_SOCK).";

fn run(args: &[String]) -> CliResult<()> {
    let Some(command) = args.first() else {
        return Err(USAGE.into());
    };
    let rest = &args[1..];
    match command.as_str() {
        "init" => init(rest),
        "write" => write(rest),
        "cat" => cat(rest),
        "cat-ids" => cat_ids(rest),
        "fsck" => fsck(rest),
        "key-status" => key_status(rest),
        "mint-recipient" => mint_recipient(),
        "-h" | "--help" | "help" => {
            println!("{USAGE}");
            Ok(())
        }
        other => Err(format!("unknown command {other:?}\n{USAGE}").into()),
    }
}

// --- flags and ids ---------------------------------------------------------

/// madder's single-dash flags: `-name value`, or a bare `-name` for the
/// flags in `bools`. Everything else is positional.
fn parse_flags(
    args: &[String],
    bools: &[&str],
) -> CliResult<(BTreeMap<String, String>, Vec<String>)> {
    let mut flags = BTreeMap::new();
    let mut positional = Vec::new();
    let mut i = 0;
    while i < args.len() {
        let arg = &args[i];
        if arg.len() > 1 && arg.starts_with('-') && !arg.starts_with("--") && !is_store_id(arg) {
            let name = arg.trim_start_matches('-').to_owned();
            if bools.contains(&name.as_str()) {
                flags.insert(name, "true".to_owned());
                i += 1;
                continue;
            }
            if let Some((n, v)) = name.split_once('=') {
                flags.insert(n.to_owned(), v.to_owned());
                i += 1;
                continue;
            }
            let value = args
                .get(i + 1)
                .ok_or_else(|| format!("flag -{name} needs a value"))?;
            flags.insert(name, value.clone());
            i += 2;
            continue;
        }
        positional.push(arg.clone());
        i += 1;
    }
    Ok((flags, positional))
}

/// A CWD-relative blob-store-id, `.name` with madder's id alphabet.
fn is_store_id(arg: &str) -> bool {
    arg.strip_prefix('.')
        .filter(|rest| !rest.is_empty())
        .is_some_and(|rest| {
            rest.bytes()
                .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
        })
}

fn store_base(id: &str) -> CliResult<PathBuf> {
    if !is_store_id(id) {
        return Err(format!(
            "blob-store-id {id:?}: this binary resolves CWD-relative ids only (.name)"
        )
        .into());
    }
    let cwd = std::env::current_dir()?;
    Ok(cwd.join(".madder/local/share/blob_stores").join(&id[1..]))
}

fn switch_notice(id: &str) {
    eprintln!("switched to blob-store-id: {id}");
}

// --- the sealed key ----------------------------------------------------------

/// Open the store key: a software identity from the environment, else the
/// agent, which this build cannot yet talk to. The error texts for the
/// no-socket case match the Go madder's, which the bats tests assert.
fn unseal(store: &Store) -> CliResult<()> {
    if store.is_unsealed() {
        return Ok(());
    }
    if let Ok(hex) = std::env::var("MADDER_STORE_IDENTITY_HEX") {
        let secret = unhex(hex.trim())?;
        let secret: [u8; 32] = secret
            .try_into()
            .map_err(|_| "MADDER_STORE_IDENTITY_HEX is not 32 bytes")?;
        let identity = X25519Identity {
            public: madder_store::age_x25519::public_key(&secret).to_vec(),
            secret: secret.to_vec(),
        };
        store.unseal(&Unsealer {
            oracle: None,
            identities: &[identity],
        })?;
        return Ok(());
    }
    // piggy's pure-Rust agent oracle (piggy_pigpen::agent, the `agent`
    // feature): one card ECDH per process, as the Go madder does.
    let Some(socket) = madder_store::piggy_pigpen::agent::resolve_auth_sock() else {
        return Err(
            "opening sealed store key: no agent socket: none of PIGGY_AUTH_SOCK, SSH_AUTH_SOCK or PIVY_AUTH_SOCK is set"
                .into(),
        );
    };
    let oracle = madder_store::piggy_pigpen::agent::AgentEcdhOracle::new(socket);
    store
        .unseal(&Unsealer {
            oracle: Some(&oracle),
            identities: &[],
        })
        .map_err(|e| format!("opening sealed store key: {e}"))?;
    Ok(())
}

// --- init ---------------------------------------------------------------------

fn init(args: &[String]) -> CliResult<()> {
    let (flags, positional) = parse_flags(args, &["if-not-exists", "discover"])?;
    let [store_id] = positional.as_slice() else {
        return Err("init takes exactly one blob-store-id".into());
    };
    let Some(pigpen_path) = flags.get("pigpen") else {
        return Err("this binary creates sealed-key stores only: pass -pigpen <piggy-ids>".into());
    };
    if flags.contains_key("encryption") {
        return Err("-pigpen cannot be combined with -encryption: a sealed-key store mints its own key and seals it to the pigpen's recipients".into());
    }
    let kind = flags
        .get("pigpen-kind")
        .map(String::as_str)
        .unwrap_or("path");

    let pigpen_text = std::fs::read_to_string(pigpen_path)
        .map_err(|e| format!("reading pigpen: {pigpen_path}: {e}"))?;
    let recipients = madder_store::parse_recipients(&pigpen_text)?;
    if recipients.is_empty() {
        return Err(format!("pigpen {pigpen_path} names no encryption recipients").into());
    }

    let mut options = madder_store::InitOptions::embedded(&pigpen_text, recipients);
    match kind {
        "embedded" => {}
        "path" => {
            options.source = madder_store::RecipientSource {
                kind: "path".to_owned(),
                locator: Some(
                    std::path::absolute(pigpen_path)?
                        .to_string_lossy()
                        .into_owned(),
                ),
                pigpen: None,
            };
        }
        other => {
            return Err(format!(
                "pigpen kind {other:?} is not supported by this binary (path, embedded)"
            )
            .into());
        }
    }
    if let Some(compression) = flags.get("compression-type") {
        options.compression = madder_store::Compression::parse(compression)?;
    }
    if let Some(hash) = flags.get("hash_type-id") {
        options.hash_format = madder_store::HashFormat::parse(hash)?;
    }

    let base = store_base(store_id)?;
    let config_path = base.join(madder_store::store::FILE_NAME_CONFIG);
    if config_path.exists() {
        if flags.contains_key("if-not-exists") {
            print_tap(&[format!("init {} (already exists)", config_path.display())]);
            return Ok(());
        }
        return Err(format!(
            "blob store {store_id:?} already exists at {}",
            config_path.display()
        )
        .into());
    }
    madder_store::init(&base, options, madder_store::OpenOptions::default())?;
    print_tap(&[format!("init {}", config_path.display())]);
    Ok(())
}

fn print_tap(ok_lines: &[String]) {
    println!("TAP version 14");
    for (i, line) in ok_lines.iter().enumerate() {
        println!("ok {} - {line}", i + 1);
    }
    println!("1..{}", ok_lines.len());
}

// --- write --------------------------------------------------------------------

fn write(args: &[String]) -> CliResult<()> {
    let (flags, positional) = parse_flags(args, &["check"])?;
    let ndjson = matches!(
        flags.get("format").map(String::as_str),
        Some("ndjson") | Some("json")
    );
    let mut store: Option<(String, Store)> = None;
    let mut lines: Vec<String> = Vec::new();
    let mut failures = 0usize;
    let mut stdout = io::stdout().lock();
    if !ndjson {
        writeln!(stdout, "TAP version 14")?;
    }

    for arg in &positional {
        if is_store_id(arg) && !Path::new(arg).exists() {
            switch_notice(arg);
            store = Some((arg.clone(), Store::open(store_base(arg)?)?));
            continue;
        }
        let Some((store_id, store)) = store.as_ref() else {
            return Err(
                "no blob store selected: pass a .name blob-store-id before the files".into(),
            );
        };
        let outcome = (|| -> madder_store::Result<(BlobId, u64)> {
            let file = std::fs::File::open(arg).map_err(Error::Io)?;
            let size = file.metadata().map(|m| m.len()).unwrap_or(0);
            Ok((store.write(file)?, size))
        })();
        match outcome {
            Ok((id, _)) if id.is_null() => {
                if ndjson {
                    writeln!(
                        stdout,
                        "{{\"source\":{},\"skipped\":\"null digest\"}}",
                        json_str(arg)
                    )?;
                } else {
                    lines.push(format!("{arg} # SKIP null digest"));
                }
            }
            Ok((id, size)) => {
                if ndjson {
                    writeln!(
                        stdout,
                        "{{\"id\":{},\"size\":{size},\"source\":{},\"store\":{}}}",
                        json_str(&id.to_wire()),
                        json_str(arg),
                        json_str(store_id)
                    )?;
                } else {
                    lines.push(format!("{id} {arg}"));
                }
            }
            Err(e) => {
                failures += 1;
                if ndjson {
                    writeln!(
                        stdout,
                        "{{\"source\":{},\"error\":{}}}",
                        json_str(arg),
                        json_str(&e.to_string())
                    )?;
                } else {
                    lines.push(format!(
                        "NOT OK {arg}\n  ---\n  message: {}\n  ...",
                        json_str(&e.to_string())
                    ));
                }
            }
        }
    }

    if !ndjson {
        for (i, line) in lines.iter().enumerate() {
            match line.strip_prefix("NOT OK ") {
                Some(rest) => writeln!(stdout, "not ok {} - {rest}", i + 1)?,
                None => writeln!(stdout, "ok {} - {line}", i + 1)?,
            }
        }
        writeln!(stdout, "1..{}", lines.len())?;
    }
    if failures > 0 {
        return Err(format!("untracked objects: {failures}").into());
    }
    Ok(())
}

// --- cat / cat-ids --------------------------------------------------------------

fn cat(args: &[String]) -> CliResult<()> {
    let mut store: Option<Store> = None;
    let mut missing = 0usize;
    let mut unreadable = 0usize;
    let mut stdout = io::stdout().lock();

    for arg in args {
        if let Ok(id) = BlobId::parse(arg) {
            let Some(store) = store.as_ref() else {
                return Err(
                    "no blob store selected: pass a .name blob-store-id before the ids".into(),
                );
            };
            if !id.is_null() {
                if let Err(e) = unseal(store) {
                    eprintln!("{e}");
                    unreadable += 1;
                    continue;
                }
            }
            match store.read_to_vec(&id) {
                Ok(bytes) => stdout.write_all(&bytes)?,
                Err(Error::NotFound(_)) => {
                    eprintln!("blob {id} not found in the store");
                    missing += 1;
                }
                Err(e) => {
                    eprintln!("blob {id}: {e}");
                    unreadable += 1;
                }
            }
            continue;
        }
        if is_store_id(arg) {
            switch_notice(arg);
            store = Some(Store::open(store_base(arg)?)?);
            continue;
        }
        eprintln!("{arg:?} is neither a blob id nor a blob-store-id");
        missing += 1;
    }
    stdout.flush()?;

    match (unreadable, missing) {
        (0, 0) => Ok(()),
        (u, 0) => Err(format!("{u} blob(s) could not be read").into()),
        (0, m) => Err(format!("{m} blob(s) not found").into()),
        (u, m) => Err(format!("{u} blob(s) could not be read, {m} blob(s) not found").into()),
    }
}

fn cat_ids(args: &[String]) -> CliResult<()> {
    let [store_id] = args else {
        return Err("cat-ids takes exactly one blob-store-id".into());
    };
    let store = Store::open(store_base(store_id)?)?;
    for id in store.ids()? {
        println!("{id}");
    }
    Ok(())
}

// --- fsck -----------------------------------------------------------------------

fn fsck(args: &[String]) -> CliResult<()> {
    let (flags, positional) = parse_flags(args, &["relink-report"])?;
    let [store_id] = positional.as_slice() else {
        return Err("fsck takes exactly one blob-store-id".into());
    };
    let ndjson = matches!(
        flags.get("format").map(String::as_str),
        Some("ndjson") | Some("json")
    );
    let store = Store::open(store_base(store_id)?)?;
    let ids = store.ids()?;

    let mut verified = 0usize;
    let mut failed = 0usize;
    let mut bytes = 0usize;
    let mut tap_lines: Vec<String> = Vec::new();
    let mut stdout = io::stdout().lock();
    if !ndjson {
        writeln!(stdout, "TAP version 14")?;
    }

    for id in &ids {
        let state = match unseal(&store) {
            Err(e) => ("read_error", Some(e.to_string())),
            Ok(()) => match store.read_to_vec(id) {
                Ok(plain) => {
                    bytes += plain.len();
                    ("verified", None)
                }
                Err(Error::NotFound(_)) => ("missing", None),
                Err(e @ (Error::DigestMismatch { .. } | Error::Age(_) | Error::Codec(_))) => {
                    ("corrupt", Some(e.to_string()))
                }
                Err(e) => ("read_error", Some(e.to_string())),
            },
        };
        if state.0 == "verified" {
            verified += 1;
        } else {
            failed += 1;
        }
        if ndjson {
            let error = state
                .1
                .as_ref()
                .map(|e| format!(",\"error\":{}", json_str(e)))
                .unwrap_or_default();
            writeln!(
                stdout,
                "{{\"id\":{},\"store\":{},\"state\":\"{}\"{error}}}",
                json_str(&id.to_wire()),
                json_str(store_id),
                state.0
            )?;
        } else if state.0 == "verified" {
            tap_lines.push(format!("ok {}", id));
        } else {
            tap_lines.push(format!(
                "not ok {}\n  ---\n  message: {}\n  ...",
                id,
                json_str(state.1.as_deref().unwrap_or(state.0))
            ));
        }
    }
    if !ndjson {
        for (i, line) in tap_lines.iter().enumerate() {
            let (status, rest) = match line.strip_prefix("not ok ") {
                Some(rest) => ("not ok", rest),
                None => ("ok", line.strip_prefix("ok ").unwrap_or(line)),
            };
            writeln!(stdout, "{status} {} - {rest}", i + 1)?;
        }
        writeln!(stdout, "1..{}", tap_lines.len())?;
    }
    stdout.flush()?;

    let mut summary =
        format!("# (blob_store: {store_id}) blobs verified: {verified}, bytes verified: {bytes} B");
    if failed > 0 {
        summary.push_str(&format!(", blobs failed: {failed}"));
    }
    eprintln!("{summary}");
    if failed > 0 {
        return Err(format!("{failed} blob(s) failed verification").into());
    }
    Ok(())
}

// --- key-status --------------------------------------------------------------------

fn key_status(args: &[String]) -> CliResult<()> {
    let [store_id] = args else {
        return Err("key-status takes exactly one blob-store-id".into());
    };
    let store = Store::open(store_base(store_id)?)?;
    let source = store.recipient_source();
    let sealed = store.sealed_recipients();

    let pigpen = match source.kind.as_str() {
        "embedded" => "embedded in blob_store-key".to_owned(),
        kind => format!("{} ({kind})", source.locator.as_deref().unwrap_or("")),
    };
    println!("pigpen:      {pigpen}");
    println!("sealed to:   {}", describe_recipient_set(&sealed));

    let current = match source.kind.as_str() {
        "path" => {
            let path = source.locator.as_deref().unwrap_or("");
            let text = std::fs::read_to_string(path).map_err(|e| {
                format!("loading recipients from {path} (path): reading pigpen: {e}")
            })?;
            madder_store::parse_recipients(&text)?
        }
        "embedded" => madder_store::parse_recipients(source.pigpen.as_deref().unwrap_or(""))?,
        other => {
            return Err(format!(
                "pigpen source kind {other:?} needs a pigpen resolver, which this binary does not run"
            )
            .into());
        }
    };
    if current.is_empty() {
        return Err("pigpen names no encryption recipients".into());
    }
    println!("pigpen now:  {}", describe_recipient_set(&current));

    for id in missing_from(&current, &sealed) {
        println!("added:       {id}");
    }
    for id in missing_from(&sealed, &current) {
        println!("removed:     {id}");
    }
    if same_set(&current, &sealed) {
        println!("status:      in sync");
    } else {
        println!("status:      changed; run `madder key-reseal {store_id}`");
    }
    Ok(())
}

/// A key as (format, bytes): the purpose spelling does not make two
/// recipients different.
fn key_of(id: &Id) -> (&'static str, Vec<u8>) {
    (id.format().as_str(), id.data().to_vec())
}

fn same_set(a: &[Id], b: &[Id]) -> bool {
    let mut ka: Vec<_> = a.iter().map(key_of).collect();
    let mut kb: Vec<_> = b.iter().map(key_of).collect();
    ka.sort();
    ka.dedup();
    kb.sort();
    kb.dedup();
    ka == kb
}

/// Recipients of `recipients` absent from `other`, in order, printed as
/// bare ids (no purpose), as Go's key-status does.
fn missing_from(recipients: &[Id], other: &[Id]) -> Vec<String> {
    let have: Vec<_> = other.iter().map(key_of).collect();
    let mut seen = Vec::new();
    let mut out = Vec::new();
    for id in recipients {
        let key = key_of(id);
        if have.contains(&key) || seen.contains(&key) {
            continue;
        }
        seen.push(key);
        out.push(
            Id::new(None, id.format(), id.data().to_vec())
                .map(|bare| bare.to_wire())
                .unwrap_or_else(|_| id.to_wire()),
        );
    }
    out
}

/// "N recipient(s) (<digest>)". The digest is this shim's own, a blake2b256
/// over the sorted bare ids, not Go's canonical recipient-set digest; the
/// tests assert the count, not the digest.
fn describe_recipient_set(recipients: &[Id]) -> String {
    use madder_store::piggy_markl::blech32;
    let mut keys: Vec<_> = recipients.iter().map(key_of).collect();
    keys.sort();
    keys.dedup();
    let mut hasher = madder_store::HashFormat::Blake2b256.hasher();
    for (format, data) in &keys {
        hasher.update(format.as_bytes());
        hasher.update(b"-");
        hasher.update(data);
        hasher.update(b"\n");
    }
    let digest = hasher.finish();
    let noun = if keys.len() == 1 {
        "recipient"
    } else {
        "recipients"
    };
    format!(
        "{} {noun} ({})",
        keys.len(),
        blech32::encode("blake2b256", digest.digest()).unwrap_or_default()
    )
}

// --- mint-recipient -------------------------------------------------------------------

fn mint_recipient() -> CliResult<()> {
    use rand_core::RngCore;
    let mut secret = [0u8; 32];
    rand_core::OsRng.fill_bytes(&mut secret);
    let public = madder_store::age_x25519::public_key(&secret);
    let id = madder_store::piggy_pigpen::recipient_id(FormatId::AgeX25519Pub, public.to_vec())?;
    println!("{}", id.to_wire());
    println!("{}", hex(&secret));
    Ok(())
}

// --- small helpers ------------------------------------------------------------------------

fn json_str(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 2);
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

fn unhex(s: &str) -> CliResult<Vec<u8>> {
    if !s.len().is_multiple_of(2) {
        return Err("odd-length hex".into());
    }
    (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&s[i..i + 2], 16).map_err(|e| e.into()))
        .collect()
}
