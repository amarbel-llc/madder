//! A store under a sync tool: a folder whose files arrive one at a time,
//! and a reader that must leave no trace. Every case has a distinct error
//! kind a vault UI can name, and nothing waits on a lock (the crate takes
//! none).

use std::fs;
use std::path::{Path, PathBuf};

use madder_store::{
    BlobId, Error, InitEncryption, InitOptions, OpenOptions, Store, Unsealer, X25519Identity, init,
    parse_recipients,
};

struct Scratch(PathBuf);

impl Scratch {
    fn new(label: &str) -> Scratch {
        let path = std::env::temp_dir().join(format!(
            "madder-store-half-synced-{label}-{}-{}",
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

/// A sealed store to one software identity, with two blobs in it.
fn sealed_store(base: &Path) -> (X25519Identity, Vec<(Vec<u8>, BlobId)>) {
    let secret = [0x5a; 32];
    let identity = X25519Identity {
        public: madder_store::age_x25519::public_key(&secret).to_vec(),
        secret: secret.to_vec(),
    };
    let id = madder_store::piggy_pigpen::recipient_id(
        madder_store::piggy_markl::FormatId::AgeX25519Pub,
        identity.public.clone(),
    )
    .unwrap();
    let pigpen = format!("{}\n", id.to_wire());
    let recipients = parse_recipients(&pigpen).unwrap();
    let store = init(
        base,
        InitOptions::embedded(&pigpen, recipients),
        OpenOptions::default(),
    )
    .unwrap();
    let blobs = [b"first entry".to_vec(), vec![7u8; 100_000]]
        .into_iter()
        .map(|plain| {
            let id = store.write_bytes(&plain).unwrap();
            (plain, id)
        })
        .collect();
    (identity, blobs)
}

/// Copy a tree file by file, as a sync tool does; a file already there is
/// left alone (the store's files are immutable and 0444).
fn copy_tree(from: &Path, to: &Path) {
    fs::create_dir_all(to).unwrap();
    for entry in fs::read_dir(from).unwrap() {
        let entry = entry.unwrap();
        let target = to.join(entry.file_name());
        if entry.file_type().unwrap().is_dir() {
            copy_tree(&entry.path(), &target);
        } else if !target.exists() {
            fs::copy(entry.path(), target).unwrap();
        }
    }
}

/// Every path under `dir`, relative, sorted: the folder's shape.
fn listing(dir: &Path) -> Vec<String> {
    fn walk(root: &Path, dir: &Path, out: &mut Vec<String>) {
        for entry in fs::read_dir(dir).unwrap() {
            let entry = entry.unwrap();
            let path = entry.path();
            out.push(path.strip_prefix(root).unwrap().display().to_string());
            if entry.file_type().unwrap().is_dir() {
                walk(root, &path, out);
            }
        }
    }
    let mut out = Vec::new();
    walk(dir, dir, &mut out);
    out.sort();
    out
}

#[test]
fn a_folder_with_no_config_is_not_a_store_yet() {
    let scratch = Scratch::new("no-config");
    let folder = scratch.path().join("vault");
    fs::create_dir_all(&folder).unwrap();

    let err = Store::open_read_only(&folder).err().unwrap();
    assert!(matches!(err, Error::NoConfig(_)), "{err:?}");
    // Nothing was created by the attempt.
    assert!(listing(&folder).is_empty());
    assert!(!scratch.path().join(".vault.tmp").exists());
}

#[test]
fn a_config_whose_sidecar_has_not_arrived_is_named_as_such() {
    let scratch = Scratch::new("no-sidecar");
    let source = scratch.path().join("source");
    let (identity, blobs) = sealed_store(&source);

    // The sync delivers the config first, as a tool copying in name order
    // does (blob_store-config sorts before blob_store-key).
    let synced = scratch.path().join("synced");
    fs::create_dir_all(&synced).unwrap();
    fs::copy(
        source.join("blob_store-config"),
        synced.join("blob_store-config"),
    )
    .unwrap();

    let err = Store::open_read_only(&synced).err().unwrap();
    assert!(matches!(err, Error::MissingSidecar(_)), "{err:?}");
    assert!(!matches!(err, Error::NoConfig(_)));

    // The sidecar arrives: the store opens and unseals, blobs still missing.
    fs::copy(source.join("blob_store-key"), synced.join("blob_store-key")).unwrap();
    let store = Store::open_read_only(&synced).unwrap();
    store
        .unseal(&Unsealer {
            oracle: None,
            identities: &[identity],
        })
        .unwrap();
    for (_, id) in &blobs {
        assert!(!store.has(id));
        let err = store.read(id).err().unwrap();
        assert!(matches!(err, Error::NotFound(_)), "{err:?}");
    }
    assert_eq!(store.ids().unwrap(), Vec::<BlobId>::new());

    // The blobs arrive: every read is whole.
    copy_tree(&source, &synced);
    for (plain, id) in &blobs {
        assert_eq!(store.read_to_vec(id).unwrap(), *plain);
    }
    let mut ids = store.ids().unwrap();
    ids.sort_by_key(|id| id.to_wire());
    let mut want: Vec<BlobId> = blobs.iter().map(|(_, id)| id.clone()).collect();
    want.sort_by_key(|id| id.to_wire());
    assert_eq!(ids, want);
}

#[test]
fn a_plain_store_needs_no_sidecar_and_a_sealed_one_cannot_lose_its() {
    let scratch = Scratch::new("plain");
    let plain = scratch.path().join("plain");
    init(
        &plain,
        InitOptions::with_encryption(InitEncryption::None),
        OpenOptions::default(),
    )
    .unwrap();
    assert!(Store::open_read_only(&plain).unwrap().is_unsealed());

    let sealed = scratch.path().join("sealed");
    sealed_store(&sealed);
    fs::remove_file(sealed.join("blob_store-key")).unwrap();
    let err = Store::open(&sealed).err().unwrap();
    assert!(matches!(err, Error::MissingSidecar(_)), "{err:?}");
}

#[test]
fn a_read_only_store_refuses_writes_and_leaves_the_folder_untouched() {
    let scratch = Scratch::new("read-only");
    let base = scratch.path().join("vault");
    let (identity, blobs) = sealed_store(&base);
    // The setup's writer left its (empty) temp dir; a reader must not
    // bring it back.
    fs::remove_dir_all(scratch.path().join(".vault.tmp")).unwrap();
    let before = listing(scratch.path());

    let store = Store::open_read_only(&base).unwrap();
    assert!(store.is_read_only());
    store
        .unseal(&Unsealer {
            oracle: None,
            identities: &[identity],
        })
        .unwrap();
    for (plain, id) in &blobs {
        assert_eq!(store.read_to_vec(id).unwrap(), *plain);
    }
    let err = store.write_bytes(b"a new entry").unwrap_err();
    assert!(matches!(err, Error::ReadOnly), "{err:?}");
    // Rewriting existing bytes is refused too, before anything is touched.
    let err = store.write_bytes(&blobs[0].0).unwrap_err();
    assert!(matches!(err, Error::ReadOnly), "{err:?}");
    assert_eq!(store.ids().unwrap().len(), blobs.len());

    // The folder and its parent are byte-for-byte the same shape: no temp
    // directory, no file, nothing for a sync tool to pick up.
    assert_eq!(listing(scratch.path()), before);
    assert!(!scratch.path().join(".vault.tmp").exists());
}

#[test]
fn a_writable_store_opened_on_the_same_folder_is_unaffected_by_a_read_only_one() {
    let scratch = Scratch::new("both");
    let base = scratch.path().join("vault");
    let (identity, _) = sealed_store(&base);

    let reader = Store::open_read_only(&base).unwrap();
    reader
        .unseal(&Unsealer {
            oracle: None,
            identities: std::slice::from_ref(&identity),
        })
        .unwrap();
    let writer = Store::open(&base).unwrap();
    writer
        .unseal(&Unsealer {
            oracle: None,
            identities: &[identity],
        })
        .unwrap();

    // No lock: the writer publishes while the reader is open, and the
    // reader sees the blob on its next read without reopening.
    let id = writer.write_bytes(b"written beside a reader").unwrap();
    assert_eq!(reader.read_to_vec(&id).unwrap(), b"written beside a reader");
}

#[test]
fn the_error_kinds_a_vault_shows_are_distinct() {
    // The kinds a UI maps to messages: not a store yet, key not synced yet,
    // this entry not synced yet, store opened read-only. Each has its own
    // variant and its own text.
    let kinds = [
        Error::NoConfig("x".into()),
        Error::MissingSidecar("x".into()),
        Error::NotFound("x".into()),
        Error::ReadOnly,
        Error::Locked,
    ];
    let texts: Vec<String> = kinds.iter().map(|e| e.to_string()).collect();
    for (i, a) in texts.iter().enumerate() {
        for (j, b) in texts.iter().enumerate() {
            assert!(i == j || a != b, "{a:?} == {b:?}");
        }
    }
    assert!(texts[0].contains("not synced yet"));
    assert!(texts[1].contains("has not arrived"));
}
