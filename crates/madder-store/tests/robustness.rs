//! Robustness: random and damaged inputs through every parser and the read
//! path are refused, never served wrong and never a panic; a sealed-key
//! store round-trips under random recipient sets and blob sizes. A small
//! deterministic PRNG keeps the cases reproducible without a dependency;
//! every case prints its seed on failure.

use std::fs;
use std::io::Read;
use std::path::{Path, PathBuf};

use madder_store::age_x25519::Decryptor;
use madder_store::config::Config;
use madder_store::sidecar::Sidecar;
use madder_store::{
    BlobId, Compression, Error, InitOptions, OpenOptions, Store, Unsealer, X25519Identity,
    hyphence, init, parse_recipients,
};

/// xorshift64*: reproducible, dependency-free.
struct Rng(u64);

impl Rng {
    fn new(seed: u64) -> Rng {
        Rng(seed.wrapping_mul(0x9E37_79B9_7F4A_7C15) | 1)
    }

    fn next(&mut self) -> u64 {
        let mut x = self.0;
        x ^= x >> 12;
        x ^= x << 25;
        x ^= x >> 27;
        self.0 = x;
        x.wrapping_mul(0x2545_F491_4F6C_DD1D)
    }

    fn below(&mut self, n: usize) -> usize {
        if n == 0 {
            0
        } else {
            (self.next() % n as u64) as usize
        }
    }

    fn bytes(&mut self, n: usize) -> Vec<u8> {
        (0..n).map(|_| (self.next() >> 24) as u8).collect()
    }

    fn secret(&mut self) -> [u8; 32] {
        let mut out = [0u8; 32];
        out.copy_from_slice(&self.bytes(32));
        out
    }
}

fn fixture_dir(name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures")
        .join(name)
}

/// A scratch directory that removes itself.
struct Scratch(PathBuf);

impl Scratch {
    fn new(label: &str) -> Scratch {
        let path = std::env::temp_dir().join(format!(
            "madder-store-robustness-{label}-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir_all(&path).unwrap();
        Scratch(path)
    }

    fn path(&self) -> &Path {
        &self.0
    }
}

impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = make_writable(&self.0);
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn copy_tree(from: &Path, to: &Path) {
    fs::create_dir_all(to).unwrap();
    for entry in fs::read_dir(from).unwrap() {
        let entry = entry.unwrap();
        let target = to.join(entry.file_name());
        if entry.file_type().unwrap().is_dir() {
            copy_tree(&entry.path(), &target);
        } else {
            fs::copy(entry.path(), &target).unwrap();
        }
    }
}

/// Blobs and configs are published read-only; mutating them needs this.
fn make_writable(path: &Path) -> std::io::Result<()> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for entry in fs::read_dir(path)? {
            let entry = entry?;
            let p = entry.path();
            if entry.file_type()?.is_dir() {
                make_writable(&p)?;
            } else {
                fs::set_permissions(&p, fs::Permissions::from_mode(0o644))?;
            }
        }
    }
    Ok(())
}

fn fixture_identity(name: &str) -> X25519Identity {
    let hex = fs::read_to_string(fixture_dir(name).join("x25519-secret.hex")).unwrap();
    let secret: Vec<u8> = (0..hex.trim().len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&hex.trim()[i..i + 2], 16).unwrap())
        .collect();
    let public =
        madder_store::age_x25519::public_key(secret.as_slice().try_into().unwrap()).to_vec();
    X25519Identity { public, secret }
}

fn fixture_blobs(name: &str) -> Vec<(Vec<u8>, BlobId)> {
    let dir = fixture_dir(name);
    fs::read_to_string(dir.join("blobs.tsv"))
        .unwrap()
        .lines()
        .map(|line| {
            let (plain, id) = line.split_once('\t').unwrap();
            (
                fs::read(dir.join("plaintexts").join(plain)).unwrap(),
                BlobId::parse(id).unwrap(),
            )
        })
        .collect()
}

/// One random edit of `bytes`: a flipped byte, a deleted range, inserted
/// bytes, a truncation, or an appended tail.
fn mutate(rng: &mut Rng, bytes: &mut Vec<u8>) {
    match rng.below(5) {
        0 if !bytes.is_empty() => {
            let at = rng.below(bytes.len());
            bytes[at] ^= 1 << rng.below(8);
        }
        1 if !bytes.is_empty() => {
            let start = rng.below(bytes.len());
            let end = (start + 1 + rng.below(64)).min(bytes.len());
            bytes.drain(start..end);
        }
        2 => {
            let at = rng.below(bytes.len() + 1);
            let count = 1 + rng.below(32);
            let insert = rng.bytes(count);
            bytes.splice(at..at, insert);
        }
        3 => {
            let keep = rng.below(bytes.len() + 1);
            bytes.truncate(keep);
        }
        _ => {
            let count = 1 + rng.below(64);
            let tail = rng.bytes(count);
            bytes.extend(tail);
        }
    }
}

#[test]
fn every_parser_survives_random_bytes() {
    let config_head = b"---\n! toml-blob_store_config-v4\n---\n\n";
    let sidecar_head = b"---\n! toml-blob_store_key-v2\n---\n\n";
    for seed in 0..1500u64 {
        let mut rng = Rng::new(seed);
        let len = rng.below(400);
        let mut bytes = rng.bytes(len);
        // Some rounds start as a plausible document so the body parsers run.
        let head: &[u8] = match rng.below(4) {
            0 => config_head,
            1 => sidecar_head,
            2 => b"age-encryption.org/v1\n-> X25519 ",
            _ => b"",
        };
        bytes.splice(0..0, head.iter().copied());
        let text = String::from_utf8_lossy(&bytes).into_owned();

        let _ = hyphence::parse(&bytes);
        let _ = Config::parse(&bytes);
        let _ = Sidecar::parse(&bytes);
        let _ = BlobId::parse(&text);
        let _ = parse_recipients(&text);
        let secret = rng.secret();
        if let Ok(mut decryptor) = Decryptor::new(&secret, &bytes[..]) {
            let _ = decryptor.read_to_end(&mut Vec::new());
        }
        if let Ok(mut decoder) = Compression::Zstd.decompress(&bytes[..]) {
            let _ = decoder.read_to_end(&mut Vec::new());
        }
        if let Ok(mut decoder) = Compression::None.decompress(&bytes[..]) {
            let _ = decoder.read_to_end(&mut Vec::new());
        };
    }
}

#[test]
fn mutated_store_files_are_refused_or_read_as_written_never_a_panic() {
    for name in [
        "zstd-blake2b256",
        "none-sha256",
        "plain-none-v4",
        "plain-key-v4",
    ] {
        let store_dir = fixture_dir(name).join("store");
        let config = fs::read(store_dir.join("blob_store-config")).unwrap();
        let sidecar = fs::read(store_dir.join("blob_store-key")).ok();

        for seed in 0..400u64 {
            let mut rng = Rng::new(seed);
            let mut mutated = config.clone();
            for _ in 0..1 + rng.below(3) {
                mutate(&mut rng, &mut mutated);
            }
            // A mutated config either fails to parse or parses to the same
            // thing the original says (a mutation in a comment or whitespace).
            if let Ok(parsed) = Config::parse(&mutated) {
                let original = Config::parse(&config).unwrap();
                assert_eq!(
                    parsed.hash_format, original.hash_format,
                    "{name} seed {seed}: a mutated config parsed to a different hash format"
                );
                assert_eq!(
                    parsed.encryption.describe(),
                    original.encryption.describe(),
                    "{name} seed {seed}: a mutated config parsed to a different key"
                );
            }
            if let Some(sidecar) = &sidecar {
                let mut mutated = sidecar.clone();
                for _ in 0..1 + rng.below(3) {
                    mutate(&mut rng, &mut mutated);
                }
                let _ = Sidecar::parse(&mutated);
            }
        }

        // On disk too: Store::open over a mutated config or sidecar.
        let scratch = Scratch::new("files");
        for seed in 0..60u64 {
            let mut rng = Rng::new(1_000_000 + seed);
            let base = scratch.path().join(format!("{name}-{seed}"));
            copy_tree(&store_dir, &base);
            make_writable(&base).unwrap();
            let target = if sidecar.is_some() && rng.below(2) == 0 {
                "blob_store-key"
            } else {
                "blob_store-config"
            };
            let mut bytes = fs::read(base.join(target)).unwrap();
            mutate(&mut rng, &mut bytes);
            fs::write(base.join(target), &bytes).unwrap();
            if let Ok(store) = Store::open(&base) {
                // It opened: it must still behave, with or without a key.
                let _ = store.ids();
                for (_, id) in fixture_blobs(name) {
                    let _ = store.has(&id);
                    let _ = store.read_to_vec(&id);
                }
            }
        }
    }
}

#[test]
fn damaged_blobs_are_refused_or_still_the_right_bytes() {
    for name in [
        "zstd-blake2b256",
        "none-sha256",
        "plain-none-v4",
        "plain-key-v4",
    ] {
        let scratch = Scratch::new("blobs");
        let base = scratch.path().join("store");
        copy_tree(&fixture_dir(name).join("store"), &base);
        make_writable(&base).unwrap();
        let store = Store::open(&base).unwrap();
        if !store.is_unsealed() {
            store
                .unseal(&Unsealer {
                    oracle: None,
                    identities: &[fixture_identity(name)],
                })
                .unwrap();
        }
        let config = store.config();

        for (plain, id) in fixture_blobs(name) {
            let path = base.join(id.relative_path(&config.hash_buckets, config.multi_hash));
            let original = fs::read(&path).unwrap();
            for seed in 0..120u64 {
                let mut rng = Rng::new(seed);
                let mut damaged = original.clone();
                for _ in 0..1 + rng.below(2) {
                    mutate(&mut rng, &mut damaged);
                }
                fs::write(&path, &damaged).unwrap();

                // The blob is still listed and `has` still answers.
                assert!(store.has(&id), "{name} seed {seed}");
                match store.read_to_vec(&id) {
                    Ok(bytes) => assert_eq!(
                        bytes, plain,
                        "{name} seed {seed}: damaged file served DIFFERENT bytes"
                    ),
                    Err(Error::NotFound(_)) => {
                        panic!("{name} seed {seed}: a present damaged blob reported not found")
                    }
                    Err(_) => {}
                }
                // Streaming reads agree with the whole-blob read.
                let streamed = store.read(&id).and_then(|mut reader| {
                    let mut out = Vec::new();
                    reader
                        .read_to_end(&mut out)
                        .map(|_| out)
                        .map_err(Error::from_io)
                });
                match (streamed, store.read_to_vec(&id)) {
                    (Ok(a), Ok(b)) => assert_eq!(a, b, "{name} seed {seed}"),
                    (Err(_), Err(_)) => {}
                    (a, b) => panic!(
                        "{name} seed {seed}: streaming and whole reads disagree: {:?} vs {:?}",
                        a.map(|v| v.len()),
                        b.map(|v| v.len())
                    ),
                }
            }
            fs::write(&path, &original).unwrap();
            assert_eq!(store.read_to_vec(&id).unwrap(), plain, "{name}: restored");
        }
    }
}

#[test]
fn sealed_stores_round_trip_under_random_recipients_and_sizes() {
    let scratch = Scratch::new("roundtrip");
    for round in 0..10u64 {
        let mut rng = Rng::new(7_000 + round);
        let identities: Vec<X25519Identity> = (0..1 + rng.below(4))
            .map(|_| {
                let secret = rng.secret();
                X25519Identity {
                    public: madder_store::age_x25519::public_key(&secret).to_vec(),
                    secret: secret.to_vec(),
                }
            })
            .collect();
        let pigpen: String = identities
            .iter()
            .map(|ident| {
                let id = madder_store::piggy_pigpen::recipient_id(
                    madder_store::piggy_markl::FormatId::AgeX25519Pub,
                    ident.public.clone(),
                )
                .unwrap();
                format!("{}\n", id.to_wire())
            })
            .collect();
        let recipients = parse_recipients(&pigpen).unwrap();
        let base = scratch.path().join(format!("store-{round}"));
        let store = init(
            &base,
            InitOptions::embedded(&pigpen, recipients),
            OpenOptions::default(),
        )
        .unwrap();

        let mut blobs: Vec<(Vec<u8>, BlobId)> = Vec::new();
        for _ in 0..1 + rng.below(5) {
            let len = match rng.below(5) {
                0 => 0,
                1 => 1,
                2 => rng.below(100),
                3 => 65_000 + rng.below(2_000),
                _ => rng.below(200_000),
            };
            let plain = rng.bytes(len);
            let id = store.write_bytes(&plain).unwrap();
            assert_eq!(id.is_null(), plain.is_empty(), "round {round}");
            blobs.push((plain, id));
        }

        // Each recipient alone opens the key and reads every blob.
        for (i, ident) in identities.iter().enumerate() {
            let again = Store::open(&base).unwrap();
            assert!(!again.is_unsealed());
            again
                .unseal(&Unsealer {
                    oracle: None,
                    identities: std::slice::from_ref(ident),
                })
                .unwrap_or_else(|e| panic!("round {round} identity {i}: {e}"));
            for (plain, id) in &blobs {
                assert_eq!(&again.read_to_vec(id).unwrap(), plain, "round {round}");
            }
        }

        // A stranger does not, and the store stays locked.
        let stranger_secret = rng.secret();
        let stranger = X25519Identity {
            public: madder_store::age_x25519::public_key(&stranger_secret).to_vec(),
            secret: stranger_secret.to_vec(),
        };
        let locked = Store::open(&base).unwrap();
        let err = locked
            .unseal(&Unsealer {
                oracle: None,
                identities: &[stranger],
            })
            .unwrap_err();
        assert!(matches!(err, Error::Pigpen(_)), "round {round}: {err:?}");
        assert!(!locked.is_unsealed());
        for (plain, id) in &blobs {
            if plain.is_empty() {
                continue;
            }
            assert!(
                matches!(locked.read(id), Err(Error::Locked)),
                "round {round}"
            );
        }
        assert_eq!(
            locked.ids().unwrap().len(),
            blobs.iter().filter(|(p, _)| !p.is_empty()).count()
        );
    }
}
