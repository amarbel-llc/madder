//! A test-only command line over the madder-store crate that speaks the
//! subset of `madder`'s grammar the store tests use, so the existing bats
//! files run against the Rust implementation with `MADDER_BIN` pointed
//! here (tests tagged `rust_cli` in zz-tests_bats; pigpen_rust_interop.bats).
//! Not shipped; pigsty talks to the crate, not to this.
//!
//! Supported: `init` (`-pigpen`, or `-encryption none|generate`), `write`,
//! `cat`, `cat-ids`, `has`, `fsck`, `info-repo`, `key-status`, plus the
//! shim-only `mint-recipient`. Blob-store-ids are CWD-relative only
//! (`.name`, resolved to `$PWD/.madder/local/share/blob_stores/name`);
//! with no id named, the default store is the alphabetically first one that
//! opens, as Go's discovery picks it, and `cat`/`has` fall back to the
//! remaining stores in that order.
//!
//! Opening a sealed key: with `MADDER_STORE_IDENTITY_HEX` set, the X25519
//! secret it holds; otherwise piggy-agent through piggy-pigpen's pure-Rust
//! `agent` oracle, with the socket resolved as Go does (`PIGGY_AUTH_SOCK`,
//! `SSH_AUTH_SOCK`, `PIVY_AUTH_SOCK`). A plain store needs neither.

use std::collections::BTreeMap;
use std::io::{self, IsTerminal, Read, Write};
use std::path::{Path, PathBuf};
use std::process::ExitCode;

use madder_store::piggy_markl::{FormatId, Id};
use madder_store::{BlobId, Error, InitEncryption, Store, Unsealer, X25519Identity};

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
  madder-store-cli init [-encryption none|generate] [-pigpen <piggy-ids> [-pigpen-kind path|embedded]]
                        [-compression-type none|zstd] [-hash_type-id sha256|blake2b256] <.store>
  madder-store-cli write [-format tap|json|ndjson] [-check] [<.store>] <file|->...
  madder-store-cli cat [<.store>] <id>...
  madder-store-cli cat-ids [<.store>]
  madder-store-cli has [-all] [<.store>] <id>...
  madder-store-cli fsck [-format tap|json|ndjson] [<.store>...]
  madder-store-cli info-repo [<.store>] [<key>...]
  madder-store-cli key-status <.store>
  madder-store-cli mint-recipient            print a fresh X25519 recipient id and its secret hex
Reading a sealed-key store opens its key with MADDER_STORE_IDENTITY_HEX (the store's X25519
secret) when set, else through piggy-agent (PIGGY_AUTH_SOCK, SSH_AUTH_SOCK, PIVY_AUTH_SOCK).";

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
        "has" => has(rest),
        "fsck" => fsck(rest),
        "info-repo" => info_repo(rest),
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
        if arg == "-" {
            positional.push(arg.clone());
            i += 1;
            continue;
        }
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

/// A blob-store-id in the subset of madder's scoped ids the store tests
/// use: `.name` is the CWD scope, a bare `name` or `~name` the XDG user
/// scope (`~` is the parse-only alias for a name a file in CWD shadows).
/// The system, cache and custom-rooted scopes are not resolved here.
fn is_store_id(arg: &str) -> bool {
    let name = store_name(arg);
    // A name starts with a letter or digit, so a flag (`-format`) is never
    // taken for a bare store id.
    name.bytes()
        .next()
        .is_some_and(|b| b.is_ascii_alphanumeric())
        && name
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

/// The bare name of a store id, without its scope prefix.
fn store_name(id: &str) -> &str {
    id.strip_prefix('.')
        .or_else(|| id.strip_prefix('~'))
        .unwrap_or(id)
}

/// Whether an argument names a store to switch to: a store id that is not
/// also an existing path, since madder's arg resolver tries the file first.
fn is_store_switch(arg: &str) -> bool {
    is_store_id(arg) && !Path::new(arg).exists()
}

/// Where blob stores live: the CWD scope,
/// `$PWD/.madder/local/share/blob_stores/<name>`, and the XDG user scope,
/// `$XDG_DATA_HOME/madder/blob_stores/<name>` (`~/.local/share` without
/// XDG_DATA_HOME).
struct Stores {
    cwd_dir: PathBuf,
    /// Empty when neither XDG_DATA_HOME nor HOME is set.
    user_dir: PathBuf,
}

impl Stores {
    fn in_cwd() -> CliResult<Stores> {
        let data_home = std::env::var_os("XDG_DATA_HOME")
            .filter(|v| !v.is_empty())
            .map(PathBuf::from)
            .or_else(|| {
                std::env::var_os("HOME")
                    .filter(|v| !v.is_empty())
                    .map(|home| PathBuf::from(home).join(".local/share"))
            });
        Ok(Stores {
            cwd_dir: std::env::current_dir()?.join(".madder/local/share/blob_stores"),
            user_dir: data_home
                .map(|d| d.join("madder/blob_stores"))
                .unwrap_or_default(),
        })
    }

    fn base(&self, id: &str) -> CliResult<PathBuf> {
        if !is_store_id(id) {
            return Err(format!(
                "blob-store-id {id:?}: this binary resolves .name (CWD), ~name and bare name (XDG user) ids only"
            )
            .into());
        }
        if id.starts_with('.') {
            return Ok(self.cwd_dir.join(store_name(id)));
        }
        if self.user_dir.as_os_str().is_empty() {
            return Err(format!(
                "blob-store-id {id:?} is in the XDG user scope, but neither XDG_DATA_HOME nor HOME is set"
            )
            .into());
        }
        Ok(self.user_dir.join(store_name(id)))
    }

    fn config_path(&self, id: &str) -> CliResult<PathBuf> {
        Ok(self.base(id)?.join(madder_store::store::FILE_NAME_CONFIG))
    }

    /// Every store id with a config file, in both scopes, sorted by id
    /// string, which is also Go's discovery order (and its default-store
    /// rule: the first that opens).
    fn ids(&self) -> CliResult<Vec<String>> {
        let mut ids = Vec::new();
        for (dir, prefix) in [(&self.cwd_dir, "."), (&self.user_dir, "")] {
            if dir.as_os_str().is_empty() {
                continue;
            }
            let entries = match std::fs::read_dir(dir) {
                Ok(entries) => entries,
                Err(e) if e.kind() == io::ErrorKind::NotFound => continue,
                Err(e) => return Err(e.into()),
            };
            for entry in entries {
                let entry = entry?;
                if entry
                    .path()
                    .join(madder_store::store::FILE_NAME_CONFIG)
                    .is_file()
                {
                    ids.push(format!("{prefix}{}", entry.file_name().to_string_lossy()));
                }
            }
        }
        ids.sort();
        Ok(ids)
    }

    fn open(&self, id: &str) -> CliResult<Store> {
        Store::open(self.base(id)?).map_err(|e| format!("blob store {id:?}: {e}").into())
    }

    /// The default store: the first id in order whose config this crate
    /// reads (Go skips stores that fail to build when picking its default).
    fn open_default(&self) -> CliResult<(String, Store)> {
        for id in self.ids()? {
            if let Ok(store) = self.open(&id) {
                return Ok((id, store));
            }
        }
        Err(format!(
            "no blob store found under {} or {} (init one first)",
            self.cwd_dir.display(),
            self.user_dir.display()
        )
        .into())
    }

    /// Every store that opens, in id order; one this crate cannot read is
    /// skipped with a note on stderr.
    fn open_all(&self) -> CliResult<Vec<(String, Store)>> {
        let mut stores = Vec::new();
        for id in self.ids()? {
            match self.open(&id) {
                Ok(store) => stores.push((id, store)),
                Err(e) => eprintln!("skipping {e}"),
            }
        }
        Ok(stores)
    }

    /// Go's discovery decodes every config before a command runs, and a
    /// local store config it cannot decode fails the command, naming the
    /// store and its config file (init.bats pins that). Configs of other
    /// store types (multi, archive, remote) are not this crate's to judge
    /// and are passed over.
    fn check_local_configs(&self) -> CliResult<()> {
        for id in self.ids()? {
            let config_path = self.config_path(&id)?;
            let raw = std::fs::read(&config_path)?;
            let is_local = madder_store::hyphence::parse(&raw).is_ok_and(|framed| {
                [
                    madder_store::config::TYPE_V3,
                    madder_store::config::TYPE_V4,
                    madder_store::config::TYPE_V5,
                ]
                .contains(&framed.type_id.as_str())
            });
            if !is_local {
                continue;
            }
            madder_store::Config::parse(&raw)
                .map_err(|e| format!("blob store {id:?} at {:?}: {e}", config_path.display()))?;
        }
        Ok(())
    }

    /// madder's arg resolver warns when a file argument shares its bare
    /// name with a configured store: the shadowed id and the spelling that
    /// names the store unambiguously (`~name` for a user-scope store, whose
    /// bare name is what just resolved to the file).
    fn shadowing(&self, arg: &str) -> CliResult<Option<(String, String)>> {
        if !is_store_id(arg) || store_name(arg) != arg {
            return Ok(None);
        }
        for id in self.ids()? {
            if store_name(&id) == arg {
                let hint = if id == arg {
                    format!("~{id}")
                } else {
                    id.clone()
                };
                return Ok(Some((id, hint)));
            }
        }
        Ok(None)
    }
}

// --- output ----------------------------------------------------------------------

#[derive(Clone, Copy, PartialEq, Eq)]
enum Format {
    Tap,
    Json,
}

/// madder's `-format`: tap, json or ndjson; unset means TAP on a terminal
/// and NDJSON on a pipe.
fn output_format(flags: &BTreeMap<String, String>) -> CliResult<Format> {
    match flags.get("format").map(String::as_str) {
        None | Some("") | Some("auto") => Ok(if io::stdout().is_terminal() {
            Format::Tap
        } else {
            Format::Json
        }),
        Some("tap") => Ok(Format::Tap),
        Some("json") | Some("ndjson") => Ok(Format::Json),
        Some(other) => Err(format!("unsupported output format: {other:?}").into()),
    }
}

/// A TAP 14 stream: the version line up front, numbered test points as they
/// come, the plan at the end.
struct Tap<W: Write> {
    out: W,
    count: usize,
}

impl<W: Write> Tap<W> {
    fn new(mut out: W) -> io::Result<Self> {
        writeln!(out, "TAP version 14")?;
        Ok(Tap { out, count: 0 })
    }

    fn ok(&mut self, description: &str) -> io::Result<()> {
        self.count += 1;
        writeln!(self.out, "ok {} - {description}", self.count)
    }

    fn not_ok(&mut self, description: &str, message: &str) -> io::Result<()> {
        self.count += 1;
        writeln!(
            self.out,
            "not ok {} - {description}\n  ---\n  message: {}\n  ...",
            self.count,
            json_str(message)
        )
    }

    fn skip(&mut self, description: &str, reason: &str) -> io::Result<()> {
        self.count += 1;
        writeln!(
            self.out,
            "ok {} - {description} # SKIP {reason}",
            self.count
        )
    }

    fn comment(&mut self, message: &str) -> io::Result<()> {
        writeln!(self.out, "# {message}")
    }

    fn plan(&mut self) -> io::Result<()> {
        writeln!(self.out, "1..{}", self.count)?;
        self.out.flush()
    }
}

// --- the sealed key ----------------------------------------------------------

/// Make the store readable: nothing to do for a plain store; for a sealed
/// one, a software identity from the environment, else the agent. The
/// error texts for the no-socket case match the Go madder's, which the bats
/// tests assert.
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

    let encryption = match (flags.get("pigpen"), flags.get("encryption")) {
        (Some(_), Some(_)) => {
            return Err("-pigpen cannot be combined with -encryption: a sealed-key store mints its own key and seals it to the pigpen's recipients".into());
        }
        (Some(pigpen_path), None) => sealed_init_encryption(pigpen_path, &flags)?,
        (None, Some(value)) => match value.as_str() {
            "none" => InitEncryption::None,
            "" | "generate" => InitEncryption::Key(None),
            other => {
                return Err(format!(
                    "-encryption {other:?}: this binary supports none and generate (or -pigpen for a sealed-key store)"
                )
                .into());
            }
        },
        (None, None) => InitEncryption::None,
    };

    let mut options = madder_store::InitOptions::with_encryption(encryption);
    if let Some(compression) = flags.get("compression-type") {
        options.compression = madder_store::Compression::parse(compression)?;
    }
    if let Some(hash) = flags.get("hash_type-id") {
        options.hash_format = madder_store::HashFormat::parse(hash)?;
    }

    let stores = Stores::in_cwd()?;
    // As in Go, init runs discovery first, and a local store whose config
    // does not decode fails it by name.
    stores.check_local_configs()?;
    let base = stores.base(store_id)?;
    let config_path = base.join(madder_store::store::FILE_NAME_CONFIG);
    // The TAP stream is opened only once there is a result to put in it: a
    // failing init leaves stdout empty rather than a header with no plan.
    if config_path.exists() {
        if flags.contains_key("if-not-exists") {
            let mut tap = Tap::new(io::stdout().lock())?;
            tap.ok(&format!("init {} (already exists)", config_path.display()))?;
            return tap.plan().map_err(Into::into);
        }
        return Err(format!(
            "blob store {store_id:?} already exists at {}",
            config_path.display()
        )
        .into());
    }
    madder_store::init(&base, options, madder_store::OpenOptions::default())?;
    let mut tap = Tap::new(io::stdout().lock())?;
    tap.ok(&format!("init {}", config_path.display()))?;
    tap.plan()?;
    Ok(())
}

/// `init -pigpen <file> [-pigpen-kind path|embedded]`.
fn sealed_init_encryption(
    pigpen_path: &str,
    flags: &BTreeMap<String, String>,
) -> CliResult<InitEncryption> {
    let pigpen_text = std::fs::read_to_string(pigpen_path)
        .map_err(|e| format!("reading pigpen: {pigpen_path}: {e}"))?;
    let recipients = madder_store::parse_recipients(&pigpen_text)?;
    if recipients.is_empty() {
        return Err(format!("pigpen {pigpen_path} names no encryption recipients").into());
    }
    let source = match flags
        .get("pigpen-kind")
        .map(String::as_str)
        .unwrap_or("path")
    {
        "embedded" => madder_store::RecipientSource {
            kind: "embedded".to_owned(),
            locator: None,
            pigpen: Some(pigpen_text),
        },
        "path" => madder_store::RecipientSource {
            kind: "path".to_owned(),
            locator: Some(
                std::path::absolute(pigpen_path)?
                    .to_string_lossy()
                    .into_owned(),
            ),
            pigpen: None,
        },
        other => {
            return Err(format!(
                "pigpen kind {other:?} is not supported by this binary (path, embedded)"
            )
            .into());
        }
    };
    Ok(InitEncryption::Sealed { recipients, source })
}

// --- write --------------------------------------------------------------------

/// The per-blob result stream of `write`, in madder's two encodings.
enum WriteSink<'a> {
    Tap(Tap<io::StdoutLock<'a>>),
    Json {
        out: io::StdoutLock<'a>,
        check: bool,
    },
}

impl WriteSink<'_> {
    fn success(
        &mut self,
        id: &BlobId,
        size: u64,
        source: &str,
        store: Option<&str>,
    ) -> io::Result<()> {
        match self {
            WriteSink::Tap(tap) => tap.ok(&format!("{id} {source}")),
            WriteSink::Json { out, check } => {
                let present = if *check { ",\"present\":true" } else { "" };
                writeln!(
                    out,
                    "{{\"id\":{},\"size\":{size},\"source\":{}{}{present}}}",
                    json_str(&id.to_wire()),
                    json_str(source),
                    store_field(store)
                )
            }
        }
    }

    fn check_miss(
        &mut self,
        id: &BlobId,
        size: u64,
        source: &str,
        store: Option<&str>,
    ) -> io::Result<()> {
        match self {
            WriteSink::Tap(tap) => tap.not_ok(&format!("{id} {source}"), "untracked"),
            WriteSink::Json { out, .. } => writeln!(
                out,
                "{{\"id\":{},\"size\":{size},\"source\":{}{},\"present\":false}}",
                json_str(&id.to_wire()),
                json_str(source),
                store_field(store)
            ),
        }
    }

    fn failure(&mut self, source: &str, error: &str) -> io::Result<()> {
        match self {
            WriteSink::Tap(tap) => tap.not_ok(source, error),
            WriteSink::Json { out, .. } => writeln!(
                out,
                "{{\"source\":{},\"error\":{}}}",
                json_str(source),
                json_str(error)
            ),
        }
    }

    fn skip(&mut self, source: &str, reason: &str) -> io::Result<()> {
        match self {
            WriteSink::Tap(tap) => tap.skip(source, reason),
            WriteSink::Json { out, .. } => writeln!(
                out,
                "{{\"source\":{},\"skipped\":{}}}",
                json_str(source),
                json_str(reason)
            ),
        }
    }

    /// A TAP comment; on stderr in JSON mode so stdout stays NDJSON.
    fn notice(&mut self, message: &str) -> io::Result<()> {
        match self {
            WriteSink::Tap(tap) => tap.comment(message),
            WriteSink::Json { .. } => {
                eprintln!("{message}");
                Ok(())
            }
        }
    }

    fn finish(&mut self) -> io::Result<()> {
        match self {
            WriteSink::Tap(tap) => tap.plan(),
            WriteSink::Json { out, .. } => out.flush(),
        }
    }
}

/// Go's record carries `store` only when a store id was named on the
/// command line.
fn store_field(store: Option<&str>) -> String {
    store
        .map(|s| format!(",\"store\":{}", json_str(s)))
        .unwrap_or_default()
}

fn write(args: &[String]) -> CliResult<()> {
    let (flags, positional) = parse_flags(args, &["check"])?;
    let format = output_format(&flags)?;
    let check = flags.contains_key("check");
    let stores = Stores::in_cwd()?;

    // The active store: named on the command line, else the default. When
    // the first argument is a file, the default store is opened before the
    // output stream starts, so a missing store fails with no TAP header.
    let mut active: Option<(Option<String>, Store)> = None;
    if positional.iter().any(|a| !is_store_switch(a))
        && positional.first().is_some_and(|a| !is_store_switch(a))
    {
        let (_, store) = stores.open_default()?;
        active = Some((None, store));
    }

    let stdout = io::stdout().lock();
    let mut sink = match format {
        Format::Tap => WriteSink::Tap(Tap::new(stdout)?),
        Format::Json => WriteSink::Json { out: stdout, check },
    };
    let mut failures = 0usize;
    let mut saw_stdin = false;

    for arg in &positional {
        if arg == "-" && saw_stdin {
            sink.notice("'-' passed in more than once. Ignoring")?;
            continue;
        }
        if is_store_switch(arg) {
            sink.notice(&format!("switched to blob-store-id: {arg}"))?;
            active = Some((Some(arg.clone()), stores.open(arg)?));
            continue;
        }
        if active.is_none() {
            let (_, store) = stores.open_default()?;
            active = Some((None, store));
        }
        let (named, store) = active.as_ref().expect("set above");

        let outcome = if arg == "-" {
            saw_stdin = true;
            digest_or_write(store, io::stdin().lock(), check)
        } else {
            if let Some((shadowed, hint)) = stores.shadowing(arg)? {
                sink.notice(&format!(
                    "warning: {arg:?} shadows blob-store-id {shadowed:?}; use './{arg}' for the file or {hint:?} for the blob-store-id"
                ))?;
            }
            match std::fs::File::open(arg) {
                Ok(file) => digest_or_write(store, file, check),
                Err(e) => Err(Error::Io(e)),
            }
        };
        match outcome {
            Err(e) => {
                failures += 1;
                sink.failure(arg, &e.to_string())?;
            }
            Ok((id, _)) if id.is_null() => sink.skip(arg, "null digest")?,
            Ok((id, size)) => {
                if store.has(&id) || !check {
                    sink.success(&id, size, arg, named.as_deref())?;
                } else {
                    failures += 1;
                    sink.check_miss(&id, size, arg, named.as_deref())?;
                }
            }
        }
    }

    sink.finish()?;
    if failures > 0 {
        return Err(format!("untracked objects: {failures}").into());
    }
    Ok(())
}

/// Write the bytes into the store, or under `-check` only digest them.
/// Returns the id and the byte count.
fn digest_or_write(
    store: &Store,
    reader: impl Read,
    check: bool,
) -> madder_store::Result<(BlobId, u64)> {
    let mut counting = Counting {
        inner: reader,
        count: 0,
    };
    let id = if check {
        let mut hasher = store.config().hash_format.hasher();
        let mut buf = [0u8; 64 * 1024];
        loop {
            let n = counting.read(&mut buf)?;
            if n == 0 {
                break;
            }
            hasher.update(&buf[..n]);
        }
        hasher.finish()
    } else {
        store.write(&mut counting)?
    };
    Ok((id, counting.count))
}

struct Counting<R: Read> {
    inner: R,
    count: u64,
}

impl<R: Read> Read for Counting<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        let n = self.inner.read(buf)?;
        self.count += n as u64;
        Ok(n)
    }
}

// --- cat / cat-ids / has ------------------------------------------------------------

fn cat(args: &[String]) -> CliResult<()> {
    let stores = Stores::in_cwd()?;
    // (id, store, named explicitly)
    let mut active: Option<(String, Store, bool)> = None;
    // Every store, opened once, for the fallback search.
    let mut all_stores: Option<Vec<(String, Store)>> = None;
    let mut missing = 0usize;
    let mut unreadable = 0usize;
    let mut stdout = io::stdout().lock();

    for arg in args {
        // A blob id first, as madder's resolver orders it: a bare store name
        // is spelled like a blob id's hash prefix.
        let id = match BlobId::parse(arg) {
            Ok(id) => id,
            Err(_) if is_store_switch(arg) => {
                eprintln!("switched to blob-store-id: {arg}");
                active = Some((arg.clone(), stores.open(arg)?, true));
                continue;
            }
            Err(_) => {
                eprintln!("{arg:?} is neither a blob id nor a blob-store-id");
                missing += 1;
                continue;
            }
        };
        if active.is_none() {
            let (default_id, store) = stores.open_default()?;
            active = Some((default_id, store, false));
        }
        let (active_id, store, explicit) = active.as_ref().expect("set above");

        match read_blob(store, &id, &mut stdout) {
            Ok(()) => {}
            Err(e) if *explicit => {
                eprintln!("{e}");
                if matches!(e, Error::NotFound(_)) {
                    missing += 1;
                } else {
                    unreadable += 1;
                }
            }
            Err(active_err) => {
                // Not in (or not readable from) the active store: the
                // remaining stores, in order, as Go's cat does.
                let held_but_unreadable = !matches!(active_err, Error::NotFound(_));
                let mut served = false;
                if all_stores.is_none() {
                    all_stores = Some(stores.open_all()?);
                }
                for (other_id, other) in all_stores.as_ref().expect("set above") {
                    if other_id == active_id || !other.has(&id) {
                        continue;
                    }
                    match read_blob(other, &id, &mut stdout) {
                        Ok(()) => {
                            served = true;
                            break;
                        }
                        Err(e) => {
                            eprintln!("{e}");
                            unreadable += 1;
                            served = true;
                            break;
                        }
                    }
                }
                if !served {
                    if held_but_unreadable {
                        eprintln!("{active_err}");
                        unreadable += 1;
                    } else {
                        eprintln!("blob not found in any blob store: {id}");
                        missing += 1;
                    }
                }
            }
        }
    }
    stdout.flush()?;

    match (unreadable, missing) {
        (0, 0) => Ok(()),
        (u, 0) => Err(format!("{u} blob(s) could not be read").into()),
        (0, m) => Err(format!("{m} blob(s) not found").into()),
        (u, m) => Err(format!("{u} blob(s) could not be read, {m} blob(s) not found").into()),
    }
}

/// Copy one blob to `out`; `Error::NotFound` when the store lacks it, any
/// other error when it holds bytes it cannot read. A failure to open the
/// store key (no agent, wrong identity) is passed through as an I/O error
/// carrying its own message: it says nothing about the store's bytes.
fn read_blob(store: &Store, id: &BlobId, out: &mut impl Write) -> madder_store::Result<()> {
    if !id.is_null() {
        if !store.has(id) {
            return Err(Error::NotFound(id.to_wire()));
        }
        unseal(store).map_err(|e| Error::Io(io::Error::other(e.to_string())))?;
    }
    let bytes = store.read_to_vec(id)?;
    out.write_all(&bytes)?;
    Ok(())
}

fn cat_ids(args: &[String]) -> CliResult<()> {
    let stores = Stores::in_cwd()?;
    let store = match args {
        [] => stores.open_default()?.1,
        [store_id] => stores.open(store_id)?,
        _ => return Err("cat-ids takes at most one blob-store-id".into()),
    };
    for id in store.ids()? {
        println!("{id}");
    }
    Ok(())
}

fn has(args: &[String]) -> CliResult<()> {
    let (flags, positional) = parse_flags(args, &["all"])?;
    let all = flags.contains_key("all");
    let stores = Stores::in_cwd()?;
    let mut explicit: Option<(String, Store)> = None;
    // Every store, opened once, for the search when none is named.
    let mut all_stores: Option<Vec<(String, Store)>> = None;
    let mut missing = 0usize;

    for arg in &positional {
        // A blob id first, as madder's resolver orders it.
        let id = match BlobId::parse(arg) {
            Ok(id) => id,
            Err(_) if is_store_switch(arg) => {
                if all {
                    return Err(format!(
                        "-all is incompatible with an explicit blob-store-id arg {arg:?}"
                    )
                    .into());
                }
                eprintln!("switched to blob-store-id: {arg}");
                explicit = Some((arg.clone(), stores.open(arg)?));
                continue;
            }
            Err(_) => {
                eprintln!("{arg:?} is neither a blob id nor a blob-store-id");
                missing += 1;
                continue;
            }
        };

        let mut hits: Vec<String> = Vec::new();
        match &explicit {
            Some((store_id, store)) => {
                if store.has(&id) {
                    hits.push(store_id.clone());
                }
            }
            None => {
                // The default store first, then the rest in id order; stop
                // at the first hit unless -all.
                if all_stores.is_none() {
                    all_stores = Some(stores.open_all()?);
                }
                for (store_id, store) in all_stores.as_ref().expect("set above") {
                    if store.has(&id) {
                        hits.push(store_id.clone());
                        if !all {
                            break;
                        }
                    }
                }
            }
        }

        if hits.is_empty() {
            println!("{id}\tnot found");
            missing += 1;
        }
        for store_id in hits {
            println!("{id}\tfound\t{store_id}");
        }
    }

    if missing > 0 {
        return Err(format!("{missing} blob(s) not found").into());
    }
    Ok(())
}

// --- fsck -----------------------------------------------------------------------

fn fsck(args: &[String]) -> CliResult<()> {
    let (flags, positional) = parse_flags(args, &["relink-report"])?;
    let format = output_format(&flags)?;
    let stores = Stores::in_cwd()?;
    let targets = if positional.is_empty() {
        stores.open_all()?
    } else {
        let mut targets = Vec::new();
        for store_id in &positional {
            targets.push((store_id.clone(), stores.open(store_id)?));
        }
        targets
    };

    let mut sink = match format {
        Format::Tap => FsckSink::Tap(Tap::new(io::stdout().lock())?),
        Format::Json => FsckSink::Json(io::stdout().lock()),
    };

    let mut total_failed = 0usize;
    for (store_id, store) in &targets {
        sink.notice(&format!("(blob_store: {store_id}) starting fsck..."))?;
        let mut verified = 0usize;
        let mut failed = 0usize;
        let mut bytes = 0usize;

        for id in store.ids()? {
            let (state, error) = match unseal(store) {
                Err(e) => ("read_error", Some(e.to_string())),
                Ok(()) => match store.read_to_vec(&id) {
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
            if state == "verified" {
                verified += 1;
            } else {
                failed += 1;
            }
            sink.record(&id, store_id, state, error.as_deref())?;
        }

        let mut summary = format!(
            "(blob_store: {store_id}) blobs verified: {verified}, bytes verified: {bytes} B"
        );
        if failed > 0 {
            summary.push_str(&format!(", blobs failed: {failed}"));
        }
        sink.notice(&summary)?;
        total_failed += failed;
    }

    sink.finish()?;
    if total_failed > 0 {
        return Err(format!("fsck: {total_failed} blob(s) failed verification").into());
    }
    Ok(())
}

/// The per-blob result stream of `fsck`, in madder's two encodings.
enum FsckSink<'a> {
    Tap(Tap<io::StdoutLock<'a>>),
    Json(io::StdoutLock<'a>),
}

impl FsckSink<'_> {
    fn record(
        &mut self,
        id: &BlobId,
        store_id: &str,
        state: &str,
        error: Option<&str>,
    ) -> io::Result<()> {
        match self {
            FsckSink::Tap(tap) => {
                if state == "verified" {
                    tap.ok(&id.to_wire())
                } else {
                    tap.not_ok(&id.to_wire(), error.unwrap_or(state))
                }
            }
            FsckSink::Json(out) => {
                let error = error
                    .map(|e| format!(",\"error\":{}", json_str(e)))
                    .unwrap_or_default();
                writeln!(
                    out,
                    "{{\"id\":{},\"store\":{},\"state\":\"{state}\"{error}}}",
                    json_str(&id.to_wire()),
                    json_str(store_id),
                )
            }
        }
    }

    /// A TAP comment; on stderr with a `# ` prefix in JSON mode, as Go's
    /// verify sink does.
    fn notice(&mut self, message: &str) -> io::Result<()> {
        match self {
            FsckSink::Tap(tap) => tap.comment(message),
            FsckSink::Json(_) => {
                eprintln!("# {message}");
                Ok(())
            }
        }
    }

    fn finish(&mut self) -> io::Result<()> {
        match self {
            FsckSink::Tap(tap) => tap.plan(),
            FsckSink::Json(out) => out.flush(),
        }
    }
}

// --- info-repo -------------------------------------------------------------------------

/// `info-repo [<.store>] [<key>...]`: Go's `ConfigKeyValues` entries for a
/// local store, or the raw config file for a lone store id.
fn info_repo(args: &[String]) -> CliResult<()> {
    let stores = Stores::in_cwd()?;
    let (store_id, store, keys): (String, Store, Vec<String>) = match args {
        [] => {
            let (id, store) = stores.open_default()?;
            (id, store, vec!["config-immutable".to_owned()])
        }
        [one]
            if is_store_id(one)
                && stores
                    .base(one)?
                    .join(madder_store::store::FILE_NAME_CONFIG)
                    .is_file() =>
        {
            (
                one.clone(),
                stores.open(one)?,
                vec!["config-immutable".to_owned()],
            )
        }
        [key] => {
            let (id, store) = stores.open_default()?;
            (id, store, vec![key.clone()])
        }
        [store_id, keys @ ..] => (store_id.clone(), stores.open(store_id)?, keys.to_vec()),
    };

    let config = store.config();
    let mut values: BTreeMap<&str, String> = BTreeMap::new();
    values.insert("blob-store-type", "local".to_owned());
    values.insert("hash_type-id", config.hash_format.as_str().to_owned());
    values.insert("supports-multi-hash", config.multi_hash.to_string());
    values.insert("encryption", config.encryption.describe());
    values.insert("compression-type", config.compression.as_str().to_owned());
    values.insert(
        "hash_buckets",
        format!(
            "[{}]",
            config
                .hash_buckets
                .iter()
                .map(|b| b.to_string())
                .collect::<Vec<_>>()
                .join(" ")
        ),
    );

    for key in &keys {
        match key.to_lowercase().as_str() {
            "config-immutable" => {
                let raw = std::fs::read(
                    stores
                        .base(&store_id)?
                        .join(madder_store::store::FILE_NAME_CONFIG),
                )?;
                io::stdout().write_all(&raw)?;
            }
            "config-path" => println!(
                "{}",
                stores
                    .base(&store_id)?
                    .join(madder_store::store::FILE_NAME_CONFIG)
                    .display()
            ),
            "dir-blob_stores" => println!(
                "{}",
                stores
                    .base(&store_id)?
                    .parent()
                    .unwrap_or(&stores.cwd_dir)
                    .display()
            ),
            other => match values.get(other) {
                Some(value) => println!("{value}"),
                None => {
                    let available: Vec<&str> = values.keys().copied().collect();
                    return Err(format!(
                        "unsupported info key: {key:?}\navailable keys: {}",
                        available.join(", ")
                    )
                    .into());
                }
            },
        }
    }
    Ok(())
}

// --- key-status --------------------------------------------------------------------

fn key_status(args: &[String]) -> CliResult<()> {
    let [store_id] = args else {
        return Err("key-status takes exactly one blob-store-id".into());
    };
    let store = Stores::in_cwd()?.open(store_id)?;
    let Some(source) = store.recipient_source() else {
        return Err(format!(
            "blob store {store_id:?} is not a sealed-key store (its config is {}); key-status applies to sealed-key stores",
            store.config().version.type_id()
        )
        .into());
    };
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
