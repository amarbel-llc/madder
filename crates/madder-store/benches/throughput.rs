//! Throughput of the store on this host, as plain numbers (no harness, no
//! dependency): seal and unseal of the store key, write and read of 1 KB,
//! 1 MB and 50 MB blobs through a plain store (zstd, no key) and a
//! sealed-key store (zstd + age), and open and `ids()` on a store holding
//! many blobs. Random (incompressible) bytes, so zstd's cost is its worst.
//!
//! Run: `just run-bench-rust` (release build). `--quick` scales the
//! repetitions and the blob count down for a smoke run; `--blobs N` sets
//! the many-blobs count (default 10000). Output goes to stdout as text;
//! the numbers recorded in madder-store(7) PERFORMANCE come from here.

use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use madder_store::{
    InitEncryption, InitOptions, OpenOptions, Store, Unsealer, X25519Identity, init,
    parse_recipients,
};

struct Scratch(PathBuf);

impl Scratch {
    fn new(label: &str) -> Scratch {
        let path =
            std::env::temp_dir().join(format!("madder-store-bench-{label}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&path);
        fs::create_dir_all(&path).unwrap();
        Scratch(path)
    }

    fn path(&self) -> &Path {
        &self.0
    }
}

impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

fn random_bytes(seed: u64, len: usize) -> Vec<u8> {
    let mut x = seed.wrapping_mul(0x9E37_79B9_7F4A_7C15) | 1;
    let mut out = Vec::with_capacity(len);
    while out.len() < len {
        x ^= x >> 12;
        x ^= x << 25;
        x ^= x >> 27;
        out.extend_from_slice(&x.wrapping_mul(0x2545_F491_4F6C_DD1D).to_le_bytes());
    }
    out.truncate(len);
    out
}

fn identity() -> (X25519Identity, String) {
    let secret = [0x42u8; 32];
    let identity = X25519Identity {
        public: madder_store::age_x25519::public_key(&secret).to_vec(),
        secret: secret.to_vec(),
    };
    let id = madder_store::piggy_pigpen::recipient_id(
        madder_store::piggy_markl::FormatId::AgeX25519Pub,
        identity.public.clone(),
    )
    .unwrap();
    (identity, format!("{}\n", id.to_wire()))
}

fn sealed_store(base: &Path, pigpen: &str) -> Store {
    let recipients = parse_recipients(pigpen).unwrap();
    init(
        base,
        InitOptions::embedded(pigpen, recipients),
        OpenOptions::default(),
    )
    .unwrap()
}

fn plain_store(base: &Path) -> Store {
    init(
        base,
        InitOptions::with_encryption(InitEncryption::None),
        OpenOptions::default(),
    )
    .unwrap()
}

fn unseal(store: &Store, identity: &X25519Identity) {
    store
        .unseal(&Unsealer {
            oracle: None,
            identities: std::slice::from_ref(identity),
        })
        .unwrap();
}

fn per_op(total: Duration, reps: usize) -> String {
    let each = total / reps as u32;
    if each < Duration::from_millis(1) {
        format!("{:>8.1} µs/op", each.as_secs_f64() * 1e6)
    } else {
        format!("{:>8.2} ms/op", each.as_secs_f64() * 1e3)
    }
}

fn rate(total: Duration, bytes: usize) -> String {
    if bytes == 0 {
        return String::new();
    }
    format!(
        "{:>8.1} MB/s",
        bytes as f64 / 1e6 / total.as_secs_f64().max(1e-9)
    )
}

fn row(label: &str, total: Duration, reps: usize, bytes: usize) {
    println!(
        "  {label:<34} {}  {}",
        per_op(total, reps),
        rate(total, bytes)
    );
}

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let quick = args.iter().any(|a| a == "--quick");
    let blobs = args
        .iter()
        .position(|a| a == "--blobs")
        .and_then(|i| args.get(i + 1))
        .and_then(|n| n.parse().ok())
        .unwrap_or(if quick { 1_000 } else { 10_000 });
    let scale = if quick { 10 } else { 1 };

    println!(
        "madder-store throughput on {} {} (temp dir {}){}",
        std::env::consts::ARCH,
        std::env::consts::OS,
        std::env::temp_dir().display(),
        if quick { ", --quick" } else { "" }
    );
    let scratch = Scratch::new("throughput");
    let (identity, pigpen) = identity();

    // Seal and unseal: the store key's life cycle.
    {
        let reps = 50 / scale;
        let start = Instant::now();
        for i in 0..reps {
            sealed_store(&scratch.path().join(format!("seal-{i}")), &pigpen);
        }
        println!("seal (init a sealed-key store, 1 X25519 recipient)");
        row("init: mint + seal + 2 files", start.elapsed(), reps, 0);

        let base = scratch.path().join("seal-0");
        let start = Instant::now();
        for _ in 0..reps {
            let store = Store::open(&base).unwrap();
            unseal(&store, &identity);
        }
        row(
            "open + unseal (software identity)",
            start.elapsed(),
            reps,
            0,
        );
        let start = Instant::now();
        for _ in 0..reps {
            Store::open(&base).unwrap();
        }
        row(
            "open alone (config + sidecar parse)",
            start.elapsed(),
            reps,
            0,
        );
    }

    // Write and read throughput per size, per store kind.
    let sizes: [(&str, usize, usize); 3] = [
        ("1 KB", 1_000, 200 / scale),
        ("1 MB", 1_000_000, 20 / scale),
        ("50 MB", 50_000_000, if quick { 1 } else { 3 }),
    ];
    for (kind, sealed) in [
        ("plain store (zstd, no key)", false),
        ("sealed-key store (zstd + age)", true),
    ] {
        println!("{kind}");
        for (label, len, reps) in sizes {
            let base = scratch
                .path()
                .join(format!("{}-{len}", if sealed { "s" } else { "p" }));
            let store = if sealed {
                let s = sealed_store(&base, &pigpen);
                unseal(&s, &identity);
                s
            } else {
                plain_store(&base)
            };
            // Distinct bytes per rep so every write publishes a new blob.
            let plains: Vec<Vec<u8>> = (0..reps as u64).map(|i| random_bytes(i + 1, len)).collect();

            let start = Instant::now();
            let ids: Vec<_> = plains
                .iter()
                .map(|p| store.write_bytes(p).unwrap())
                .collect();
            row(
                &format!("write {label} x{reps}"),
                start.elapsed(),
                reps,
                len * reps,
            );

            let start = Instant::now();
            for (id, plain) in ids.iter().zip(&plains) {
                let got = store.read_to_vec(id).unwrap();
                assert_eq!(got.len(), plain.len());
            }
            row(
                &format!("read  {label} x{reps}"),
                start.elapsed(),
                reps,
                len * reps,
            );

            let start = Instant::now();
            for id in &ids {
                assert!(store.has(id));
            }
            row(&format!("has   {label} x{reps}"), start.elapsed(), reps, 0);
            let _ = fs::remove_dir_all(&base);
            let _ = fs::remove_dir_all(
                scratch
                    .path()
                    .join(format!(".{}-{len}.tmp", if sealed { "s" } else { "p" })),
            );
        }
    }

    // A store holding many blobs: what open and ids() cost.
    {
        println!("sealed-key store holding {blobs} blobs of 1 KB");
        let base = scratch.path().join("many");
        let store = sealed_store(&base, &pigpen);
        let start = Instant::now();
        for i in 0..blobs as u64 {
            store
                .write_bytes(&random_bytes(1_000_000 + i, 1_000))
                .unwrap();
        }
        row(
            &format!("write {blobs} blobs (fsync each)"),
            start.elapsed(),
            blobs,
            1_000 * blobs,
        );
        drop(store);

        let reps = 20;
        let start = Instant::now();
        for _ in 0..reps {
            let store = Store::open(&base).unwrap();
            unseal(&store, &identity);
        }
        row("open + unseal", start.elapsed(), reps, 0);

        let reps = 5;
        let start = Instant::now();
        let mut count = 0;
        for _ in 0..reps {
            count = Store::open(&base).unwrap().ids().unwrap().len();
        }
        row(
            &format!("ids() walk ({count} blobs)"),
            start.elapsed(),
            reps,
            0,
        );
    }
    std::io::stdout().flush().unwrap();
}
