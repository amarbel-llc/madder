//! The store under the conditions a vault meets on a shared folder:
//! several writers at once, a writer that died mid-way, a publish that
//! was interrupted between its steps. In every case a reader sees either
//! nothing or the whole, correct blob, never partial or wrong bytes, and
//! nothing panics. Each thread opens its own `Store` on the same base, as
//! separate processes would (a `Store` is single-threaded by design).

use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;

use madder_store::{BlobId, Error, InitEncryption, InitOptions, OpenOptions, Store, init};

/// A scratch directory that removes itself.
struct Scratch(PathBuf);

impl Scratch {
    fn new(label: &str) -> Scratch {
        let path = std::env::temp_dir().join(format!(
            "madder-store-concurrency-{label}-{}-{}",
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
        let _ = fs::remove_dir_all(&self.0);
    }
}

/// A plain unencrypted store: the publish protocol is the same for every
/// store kind, and this one needs no key, so the test is about the files.
fn plain_store(scratch: &Scratch, name: &str) -> PathBuf {
    let base = scratch.path().join(name);
    init(
        &base,
        InitOptions::with_encryption(InitEncryption::None),
        OpenOptions::default(),
    )
    .unwrap();
    base
}

fn bytes(seed: u64, len: usize) -> Vec<u8> {
    let mut x = seed.wrapping_mul(0x9E37_79B9_7F4A_7C15) | 1;
    (0..len)
        .map(|_| {
            x ^= x >> 12;
            x ^= x << 25;
            x ^= x >> 27;
            (x.wrapping_mul(0x2545_F491_4F6C_DD1D) >> 24) as u8
        })
        .collect()
}

#[test]
fn concurrent_writers_of_the_same_and_different_blobs_agree() {
    let scratch = Scratch::new("writers");
    let base = plain_store(&scratch, "store");

    // Eight threads: all write the same large blob (the EEXIST race on
    // link(2)) and each writes two of its own.
    let shared = Arc::new(bytes(1, 300_000));
    let handles: Vec<_> = (0..8u64)
        .map(|t| {
            let base = base.clone();
            let shared = Arc::clone(&shared);
            thread::spawn(move || {
                let store = Store::open(&base).unwrap();
                let own_a = bytes(100 + t, 1 + (t as usize) * 1000);
                let own_b = bytes(200 + t, 70_000 + (t as usize) * 10);
                let shared_id = store.write_bytes(&shared).unwrap();
                let a = store.write_bytes(&own_a).unwrap();
                let b = store.write_bytes(&own_b).unwrap();
                (shared_id, a, own_a, b, own_b)
            })
        })
        .collect();

    let store = Store::open(&base).unwrap();
    let mut shared_ids = Vec::new();
    let mut expected: Vec<(BlobId, Vec<u8>)> = Vec::new();
    for handle in handles {
        let (shared_id, a, own_a, b, own_b) = handle.join().unwrap();
        shared_ids.push(shared_id);
        expected.push((a, own_a));
        expected.push((b, own_b));
    }
    assert!(
        shared_ids.windows(2).all(|w| w[0] == w[1]),
        "writers of the same bytes minted different ids"
    );
    assert_eq!(store.read_to_vec(&shared_ids[0]).unwrap(), *shared);
    for (id, plain) in &expected {
        assert_eq!(&store.read_to_vec(id).unwrap(), plain);
    }
    // One file per distinct blob, and nothing left in the temp dir.
    assert_eq!(store.ids().unwrap().len(), 1 + expected.len());
    let temp_dir = base.parent().unwrap().join(".store.tmp");
    assert_eq!(fs::read_dir(&temp_dir).unwrap().count(), 0);
}

#[test]
fn a_reader_never_sees_a_partial_blob_while_it_is_written() {
    let scratch = Scratch::new("reader");
    let base = plain_store(&scratch, "store");
    let plain = Arc::new(bytes(7, 2_000_000));
    // The id the writer will mint, computed up front from the bytes.
    let id = {
        let mut hasher = madder_store::HashFormat::Blake2b256.hasher();
        hasher.update(&plain);
        hasher.finish()
    };

    let done = Arc::new(AtomicBool::new(false));
    let reader = {
        let base = base.clone();
        let plain = Arc::clone(&plain);
        let done = Arc::clone(&done);
        let id = id.clone();
        thread::spawn(move || {
            let store = Store::open(&base).unwrap();
            let mut saw_it = false;
            let mut polls = 0usize;
            while !done.load(Ordering::SeqCst) || !saw_it {
                polls += 1;
                // One read decides (a `has` first would race the writer):
                // either the blob is not there yet, or it is whole and
                // correct. Nothing in between.
                match store.read_to_vec(&id) {
                    Ok(got) => {
                        assert_eq!(got, *plain);
                        saw_it = true;
                    }
                    Err(Error::NotFound(_)) => {}
                    Err(other) => panic!("a partial or wrong read: {other}"),
                }
                if polls > 2_000_000 {
                    panic!("the blob never became visible");
                }
            }
        })
    };

    // Write it ten times over from five threads while the reader polls.
    let writers: Vec<_> = (0..5)
        .map(|_| {
            let base = base.clone();
            let plain = Arc::clone(&plain);
            let id = id.clone();
            thread::spawn(move || {
                let store = Store::open(&base).unwrap();
                for _ in 0..2 {
                    assert_eq!(store.write_bytes(&plain).unwrap(), id);
                }
            })
        })
        .collect();
    for w in writers {
        w.join().unwrap();
    }
    done.store(true, Ordering::SeqCst);
    reader.join().unwrap();
}

#[test]
fn a_writer_that_died_leaves_the_store_intact() {
    let scratch = Scratch::new("died");
    let base = plain_store(&scratch, "store");
    let store = Store::open(&base).unwrap();
    let good = store.write_bytes(b"written before the crash").unwrap();
    let temp_dir = base.parent().unwrap().join(".store.tmp");

    // A writer that died mid-stream: a half-written temp file, still 0644.
    fs::write(temp_dir.join("tmp-99999-deadbeef"), b"half a blob").unwrap();
    // One that died after fsync + chmod but before link(2): read-only temp.
    let ready = temp_dir.join("tmp-99999-cafef00d");
    fs::write(&ready, b"a whole blob never published").unwrap();
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(&ready, fs::Permissions::from_mode(0o444)).unwrap();
    }

    // Nothing of that is a blob: listing, reading and writing go on.
    let again = Store::open(&base).unwrap();
    assert_eq!(again.ids().unwrap(), vec![good.clone()]);
    assert_eq!(
        again.read_to_vec(&good).unwrap(),
        b"written before the crash"
    );
    let next = again.write_bytes(b"written after the crash").unwrap();
    assert_eq!(
        again.read_to_vec(&next).unwrap(),
        b"written after the crash"
    );
    assert_eq!(again.ids().unwrap().len(), 2);

    // The leftovers are still there (nobody cleans another writer's temp
    // files) and still harmless.
    assert!(temp_dir.join("tmp-99999-deadbeef").exists());
    assert!(ready.exists());
}

#[test]
fn a_publish_interrupted_after_link_is_a_complete_blob() {
    let scratch = Scratch::new("linked");
    let base = plain_store(&scratch, "store");
    let store = Store::open(&base).unwrap();
    let plain = bytes(11, 150_000);
    let id = store.write_bytes(&plain).unwrap();
    let config = store.config();
    let final_path = base.join(id.relative_path(&config.hash_buckets, config.multi_hash));

    // Re-create the state between link(2) and unlink(temp): the published
    // file and its temp name are one inode.
    let temp_dir = base.parent().unwrap().join(".store.tmp");
    let temp = temp_dir.join("tmp-4242-0badf00d");
    fs::hard_link(&final_path, &temp).unwrap();

    // Readers see the complete blob, a rewrite is the EEXIST no-op and
    // leaves the published file alone, and the stray link is harmless.
    let again = Store::open(&base).unwrap();
    assert_eq!(again.read_to_vec(&id).unwrap(), plain);
    assert_eq!(again.write_bytes(&plain).unwrap(), id);
    assert_eq!(again.read_to_vec(&id).unwrap(), plain);
    assert_eq!(fs::read(&final_path).unwrap(), fs::read(&temp).unwrap());
    assert_eq!(again.ids().unwrap(), vec![id]);
}

#[test]
fn a_truncated_published_blob_is_refused_and_can_be_republished() {
    // Not a state this crate's publish can leave (link(2) is after fsync),
    // but a vault on a shared folder may meet one left by a copying tool:
    // the file exists at its path with part of its bytes.
    let scratch = Scratch::new("truncated");
    let base = plain_store(&scratch, "store");
    let store = Store::open(&base).unwrap();
    let plain = bytes(13, 90_000);
    let id = store.write_bytes(&plain).unwrap();
    let config = store.config();
    let final_path = base.join(id.relative_path(&config.hash_buckets, config.multi_hash));

    let whole = fs::read(&final_path).unwrap();
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(&final_path, fs::Permissions::from_mode(0o644)).unwrap();
    }
    fs::write(&final_path, &whole[..whole.len() / 2]).unwrap();

    assert!(store.has(&id));
    assert!(store.read_to_vec(&id).is_err(), "half a blob must not read");
    // A rewrite hits EEXIST and does not repair it: the publish protocol
    // takes an existing path as the same bytes (ADR 0002). Repair is
    // deleting the damaged file first, after which the write is a fresh
    // publish.
    assert_eq!(store.write_bytes(&plain).unwrap(), id);
    assert!(store.read_to_vec(&id).is_err());
    fs::remove_file(&final_path).unwrap();
    assert!(!store.has(&id));
    assert_eq!(store.write_bytes(&plain).unwrap(), id);
    assert_eq!(store.read_to_vec(&id).unwrap(), plain);
}
