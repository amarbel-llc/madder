//! The hand-rolled age X25519 subset against the reference `age` CLI: a
//! file this crate writes must decrypt with `age -d`, and a file `age -r`
//! writes must decrypt here. Runs when `MADDER_STORE_AGE_BIN` names the
//! binary (the nix lane sets it) or `age` is on PATH; otherwise it is
//! skipped with a notice.

use std::io::{Read, Write};
use std::process::{Command, Stdio};

use madder_store::age_x25519::{Decryptor, Encryptor, public_key};
use madder_store::piggy_markl::blech32;

fn age_bin() -> Option<String> {
    if let Ok(bin) = std::env::var("MADDER_STORE_AGE_BIN") {
        return Some(bin);
    }
    let found = Command::new("age").arg("--version").output().ok()?;
    found.status.success().then(|| "age".to_owned())
}

/// age's bech32 strings share blech32's alphabet and checksum; only the
/// separator differs (`1` for `-`). The identity string is upper-case.
fn age_recipient(public: &[u8; 32]) -> String {
    blech32::encode("age", public)
        .unwrap()
        .replacen('-', "1", 1)
}

fn age_identity(secret: &[u8; 32]) -> String {
    // blech32 refuses a hyphen inside the HRP, so build the data part
    // under a placeholder HRP whose checksum is wrong, then recompute? No:
    // the checksum covers the HRP, so encode under the lower-case HRP age
    // uses and swap the separator, as for the recipient.
    let encoded = blech32::encode("age_secret_key_", secret).unwrap();
    let data = &encoded["age_secret_key_-".len()..];
    // The HRP age uses is "age-secret-key-"; recompute the checksum for it
    // by encoding through a tiny local bech32, since blech32 cannot carry
    // a hyphenated HRP.
    let _ = data;
    bech32_encode("age-secret-key-", secret).to_uppercase()
}

// A minimal BIP173 bech32 encoder, enough for the age identity string
// (whose HRP contains hyphens, which piggy-markl's blech32 refuses).
fn bech32_encode(hrp: &str, data: &[u8]) -> String {
    const CHARSET: &[u8] = b"qpzry9x8gf2tvdw0s3jn54khce6mua7l";
    fn polymod(values: &[u8]) -> u32 {
        const GEN: [u32; 5] = [
            0x3b6a_57b2,
            0x2650_8e6d,
            0x1ea1_19fa,
            0x3d42_33dd,
            0x2a14_62b3,
        ];
        let mut chk: u32 = 1;
        for &v in values {
            let b = chk >> 25;
            chk = ((chk & 0x1ff_ffff) << 5) ^ u32::from(v);
            for (i, g) in GEN.iter().enumerate() {
                if (b >> i) & 1 == 1 {
                    chk ^= g;
                }
            }
        }
        chk
    }
    let mut values: Vec<u8> = Vec::new();
    let mut acc: u32 = 0;
    let mut bits = 0;
    for &byte in data {
        acc = (acc << 8) | u32::from(byte);
        bits += 8;
        while bits >= 5 {
            bits -= 5;
            values.push(((acc >> bits) & 31) as u8);
        }
    }
    if bits > 0 {
        values.push(((acc << (5 - bits)) & 31) as u8);
    }
    let mut expanded: Vec<u8> = hrp.bytes().map(|b| b >> 5).collect();
    expanded.push(0);
    expanded.extend(hrp.bytes().map(|b| b & 31));
    expanded.extend_from_slice(&values);
    expanded.extend_from_slice(&[0; 6]);
    let pm = polymod(&expanded) ^ 1;
    let mut out = String::from(hrp);
    out.push('1');
    for v in &values {
        out.push(CHARSET[*v as usize] as char);
    }
    for i in 0..6 {
        out.push(CHARSET[((pm >> (5 * (5 - i))) & 31) as usize] as char);
    }
    out
}

#[test]
fn bech32_encoder_matches_blech32_for_a_plain_hrp() {
    let data = [7u8; 32];
    let ours = bech32_encode("age", &data);
    let theirs = blech32::encode("age", &data).unwrap().replacen('-', "1", 1);
    assert_eq!(ours, theirs);
}

#[test]
fn rust_written_files_decrypt_with_the_age_cli() {
    let Some(age) = age_bin() else {
        eprintln!("skipping: no age binary (set MADDER_STORE_AGE_BIN)");
        return;
    };
    let secret = [0x42u8; 32];
    let recipient = public_key(&secret);
    let scratch = std::env::temp_dir().join(format!("madder-store-age-{}", std::process::id()));
    std::fs::create_dir_all(&scratch).unwrap();
    let identity_file = scratch.join("identity.txt");
    std::fs::write(&identity_file, format!("{}\n", age_identity(&secret))).unwrap();

    for len in [0usize, 1, 100, 65_536, 65_537, 200_000] {
        let plaintext: Vec<u8> = (0..len).map(|i| (i * 31 % 253) as u8).collect();
        let mut enc = Encryptor::new(&recipient, Vec::new()).unwrap();
        enc.write_all(&plaintext).unwrap();
        let file = enc.finish().unwrap();

        let mut child = Command::new(&age)
            .args(["-d", "-i"])
            .arg(&identity_file)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap();
        let mut stdin = child.stdin.take().unwrap();
        let feeder = std::thread::spawn(move || stdin.write_all(&file));
        let out = child.wait_with_output().unwrap();
        feeder.join().unwrap().unwrap();
        assert!(
            out.status.success(),
            "age -d failed for {len} bytes: {}",
            String::from_utf8_lossy(&out.stderr)
        );
        assert_eq!(out.stdout, plaintext, "age -d output for {len} bytes");
    }
    let _ = std::fs::remove_dir_all(&scratch);
}

#[test]
fn age_cli_written_files_decrypt_here() {
    let Some(age) = age_bin() else {
        eprintln!("skipping: no age binary (set MADDER_STORE_AGE_BIN)");
        return;
    };
    let secret = [0x17u8; 32];
    let recipient = age_recipient(&public_key(&secret));

    for len in [0usize, 1, 100, 65_536, 65_537, 200_000] {
        let plaintext: Vec<u8> = (0..len).map(|i| (i * 17 % 251) as u8).collect();
        let mut child = Command::new(&age)
            .args(["-r", &recipient])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap();
        // Feed stdin from a thread: age streams its output, and a pipe
        // written to completion before stdout is drained deadlocks past
        // the pipe buffer.
        let mut stdin = child.stdin.take().unwrap();
        let feeder = std::thread::spawn(move || stdin.write_all(&plaintext).map(|()| plaintext));
        let out = child.wait_with_output().unwrap();
        let plaintext = feeder.join().unwrap().unwrap();
        assert!(
            out.status.success(),
            "age -r failed for {len} bytes: {}",
            String::from_utf8_lossy(&out.stderr)
        );

        let mut dec = Decryptor::new(&secret, &out.stdout[..]).unwrap();
        let mut got = Vec::new();
        dec.read_to_end(&mut got).unwrap();
        assert_eq!(got, plaintext, "decrypting age -r output of {len} bytes");
    }
}
