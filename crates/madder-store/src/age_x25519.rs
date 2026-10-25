//! The subset of the age v1 file format a sealed-key store uses: one
//! X25519 recipient stanza, the header MAC, and the STREAM payload. Written
//! from the age specification (age-encryption.org/v1) against the Go
//! `filippo.io/age` output in the fixtures; anything outside the subset
//! (another stanza type, several stanzas, a missing MAC) is refused rather
//! than guessed.
//!
//! Everything here is pure Rust on the RustCrypto crates piggy-pigpen
//! already depends on, so the dependency closure is the one proven on
//! wasm32.

use std::io::{self, BufRead, BufReader, Read, Write};

use base64::Engine;
use base64::engine::general_purpose::STANDARD_NO_PAD as B64;
use chacha20poly1305::aead::{Aead, AeadInPlace, KeyInit};
use chacha20poly1305::{ChaCha20Poly1305, Key, Nonce};
use hkdf::Hkdf;
use hmac::{Hmac, Mac};
use rand_core::{OsRng, RngCore};
use sha2::Sha256;
use x25519_dalek::{PublicKey, StaticSecret};
use zeroize::Zeroize;

use crate::{Error, Result};

const INTRO: &[u8] = b"age-encryption.org/v1\n";
const STANZA_X25519: &str = "X25519";
const MAC_PREFIX: &[u8] = b"--- ";
const FILE_KEY_LEN: usize = 16;
const NONCE_LEN: usize = 16;
const TAG_LEN: usize = 16;
/// Plaintext bytes per STREAM chunk.
pub const CHUNK: usize = 64 * 1024;
const INFO_X25519: &[u8] = b"age-encryption.org/v1/X25519";
const INFO_HEADER: &[u8] = b"header";
const INFO_PAYLOAD: &[u8] = b"payload";

fn hkdf32(ikm: &[u8], salt: Option<&[u8]>, info: &[u8]) -> [u8; 32] {
    let hk = Hkdf::<Sha256>::new(salt, ikm);
    let mut okm = [0u8; 32];
    hk.expand(info, &mut okm)
        .expect("32 is a valid HKDF length");
    okm
}

fn aead(key: &[u8; 32]) -> ChaCha20Poly1305 {
    ChaCha20Poly1305::new(Key::from_slice(key))
}

/// Refuse a recipient that is a low-order point: every exchange with it
/// yields the same all-zero secret (RFC 7748 section 6.1), so anyone could
/// read what is encrypted to it. Go's `rejectLowOrderX25519` does the same.
pub fn check_recipient(recipient: &[u8; 32]) -> Result<()> {
    let probe = StaticSecret::from([1u8; 32]);
    if !probe
        .diffie_hellman(&PublicKey::from(*recipient))
        .was_contributory()
    {
        return Err(Error::Age(
            "age_x25519_pub key is a low-order point and cannot be encrypted to".into(),
        ));
    }
    Ok(())
}

/// The public half of an X25519 secret scalar.
pub fn public_key(secret: &[u8; 32]) -> [u8; 32] {
    PublicKey::from(&StaticSecret::from(*secret)).to_bytes()
}

// --- writing --------------------------------------------------------------

/// Encrypts a stream to one X25519 recipient. Write plaintext, then call
/// [`Encryptor::finish`]; dropping it without `finish` leaves a truncated
/// file no reader accepts.
pub struct Encryptor<W: Write> {
    out: Option<W>,
    aead: ChaCha20Poly1305,
    counter: u128,
    buffer: Vec<u8>,
}

impl<W: Write> Encryptor<W> {
    pub fn new(recipient: &[u8; 32], mut out: W) -> Result<Self> {
        check_recipient(recipient)?;

        let mut file_key = [0u8; FILE_KEY_LEN];
        OsRng.fill_bytes(&mut file_key);
        let mut nonce = [0u8; NONCE_LEN];
        OsRng.fill_bytes(&mut nonce);
        let mut ephemeral = [0u8; 32];
        OsRng.fill_bytes(&mut ephemeral);

        let header = Self::header(&file_key, recipient, &ephemeral)?;
        ephemeral.zeroize();
        out.write_all(&header)?;
        out.write_all(&nonce)?;

        let mut payload_key = hkdf32(&file_key, Some(&nonce), INFO_PAYLOAD);
        file_key.zeroize();
        let aead = aead(&payload_key);
        payload_key.zeroize();

        Ok(Encryptor {
            out: Some(out),
            aead,
            counter: 0,
            buffer: Vec::with_capacity(CHUNK),
        })
    }

    fn header(
        file_key: &[u8; FILE_KEY_LEN],
        recipient: &[u8; 32],
        ephemeral: &[u8; 32],
    ) -> Result<Vec<u8>> {
        let secret = StaticSecret::from(*ephemeral);
        let epk = PublicKey::from(&secret).to_bytes();
        let shared = secret.diffie_hellman(&PublicKey::from(*recipient));
        if !shared.was_contributory() {
            return Err(Error::Age("X25519 shared secret is all zeros".into()));
        }

        let mut salt = [0u8; 64];
        salt[..32].copy_from_slice(&epk);
        salt[32..].copy_from_slice(recipient);
        let mut wrap_key = hkdf32(shared.as_bytes(), Some(&salt), INFO_X25519);
        let wrapped = aead(&wrap_key)
            .encrypt(Nonce::from_slice(&[0u8; 12]), &file_key[..])
            .map_err(|e| Error::Age(format!("wrapping the file key: {e}")))?;
        wrap_key.zeroize();

        let mut header = Vec::with_capacity(200);
        header.extend_from_slice(INTRO);
        header.extend_from_slice(format!("-> {STANZA_X25519} {}\n", B64.encode(epk)).as_bytes());
        header.extend_from_slice(B64.encode(&wrapped).as_bytes());
        header.extend_from_slice(b"\n---");
        let mac = header_mac(file_key, &header);
        header.extend_from_slice(format!(" {}\n", B64.encode(mac)).as_bytes());
        Ok(header)
    }

    fn seal_chunk(&mut self, last: bool) -> io::Result<()> {
        let nonce = stream_nonce(self.counter, last);
        self.counter += 1;
        let sealed = self
            .aead
            .encrypt(Nonce::from_slice(&nonce), &self.buffer[..])
            .map_err(|e| io::Error::other(format!("age: sealing a chunk: {e}")))?;
        self.buffer.clear();
        self.out
            .as_mut()
            .expect("finish takes the writer")
            .write_all(&sealed)
    }

    /// Seal the final chunk and return the writer. Nothing is flushed or
    /// synced beyond what the writer does itself.
    pub fn finish(mut self) -> Result<W> {
        self.seal_chunk(true)?;
        let mut out = self.out.take().expect("finish runs once");
        out.flush()?;
        Ok(out)
    }
}

impl<W: Write> Write for Encryptor<W> {
    fn write(&mut self, mut buf: &[u8]) -> io::Result<usize> {
        let n = buf.len();
        while !buf.is_empty() {
            // A full buffer is sealed only once more bytes arrive: whether
            // it is the last chunk is not known until then.
            if self.buffer.len() == CHUNK {
                self.seal_chunk(false)?;
            }
            let take = (CHUNK - self.buffer.len()).min(buf.len());
            self.buffer.extend_from_slice(&buf[..take]);
            buf = &buf[take..];
        }
        Ok(n)
    }

    fn flush(&mut self) -> io::Result<()> {
        // Chunks cannot be flushed early without deciding they are not last;
        // the caller's data reaches the writer at finish.
        Ok(())
    }
}

fn stream_nonce(counter: u128, last: bool) -> [u8; 12] {
    let mut nonce = [0u8; 12];
    nonce[..11].copy_from_slice(&counter.to_be_bytes()[5..]);
    nonce[11] = u8::from(last);
    nonce
}

fn header_mac(file_key: &[u8; FILE_KEY_LEN], header_through_dashes: &[u8]) -> [u8; 32] {
    let key = hkdf32(file_key, None, INFO_HEADER);
    let mut mac = <Hmac<Sha256> as Mac>::new_from_slice(&key).expect("any key length works");
    mac.update(header_through_dashes);
    let mut out = [0u8; 32];
    out.copy_from_slice(&mac.finalize().into_bytes());
    out
}

// --- reading --------------------------------------------------------------

/// Decrypts an age file written to one X25519 recipient. The header is
/// read and authenticated in [`Decryptor::new`]; payload chunks are
/// authenticated as they are read, so a truncated or altered file fails
/// before its bytes are handed on, never after.
pub struct Decryptor<R: Read> {
    input: BufReader<R>,
    aead: ChaCha20Poly1305,
    counter: u128,
    /// The next ciphertext chunk, read ahead so the last one is known.
    pending: Vec<u8>,
    /// The current chunk, decrypted in place. Once consumed, its buffer is
    /// reused for the next read-ahead, so the two rotate and a stream costs
    /// no allocation per chunk.
    plaintext: Vec<u8>,
    plaintext_pos: usize,
    done: bool,
    started: bool,
}

impl<R: Read> Decryptor<R> {
    pub fn new(secret: &[u8; 32], input: R) -> Result<Self> {
        let mut input = BufReader::new(input);
        let mut file_key = parse_header(&mut input, secret)?;

        let mut nonce = [0u8; NONCE_LEN];
        input
            .read_exact(&mut nonce)
            .map_err(|_| Error::Age("payload is missing its nonce".into()))?;
        let mut payload_key = hkdf32(&file_key, Some(&nonce), INFO_PAYLOAD);
        file_key.zeroize();
        let aead = aead(&payload_key);
        payload_key.zeroize();

        Ok(Decryptor {
            input,
            aead,
            counter: 0,
            pending: Vec::new(),
            plaintext: Vec::new(),
            plaintext_pos: 0,
            done: false,
            started: false,
        })
    }

    /// Fill `chunk` (reusing its allocation) with the next ciphertext
    /// chunk; it comes back shorter at the end of the stream.
    fn read_chunk(&mut self, chunk: &mut Vec<u8>) -> io::Result<()> {
        chunk.clear();
        chunk.resize(CHUNK + TAG_LEN, 0);
        let mut filled = 0;
        while filled < chunk.len() {
            let n = self.input.read(&mut chunk[filled..])?;
            if n == 0 {
                break;
            }
            filled += n;
        }
        chunk.truncate(filled);
        Ok(())
    }

    /// Decrypt the next chunk. Called only once `plaintext` is consumed,
    /// so its buffer is free to become the read-ahead.
    fn open_next(&mut self) -> io::Result<()> {
        let mut spare = std::mem::take(&mut self.plaintext);
        if !self.started {
            self.read_chunk(&mut spare)?;
            self.pending = std::mem::take(&mut spare);
            self.started = true;
        }
        let mut current = std::mem::take(&mut self.pending);
        if current.len() == CHUNK + TAG_LEN {
            self.read_chunk(&mut spare)?;
        } else {
            spare.clear();
        }
        let last = spare.is_empty();

        if current.len() < TAG_LEN {
            return Err(age_err("payload chunk is shorter than its tag"));
        }
        if current.len() == TAG_LEN && self.counter > 0 {
            return Err(age_err("payload ends in an empty chunk"));
        }

        let nonce = stream_nonce(self.counter, last);
        self.counter += 1;
        self.aead
            .decrypt_in_place(Nonce::from_slice(&nonce), &[], &mut current)
            .map_err(|_| age_err("payload chunk failed to authenticate (altered or truncated)"))?;

        self.plaintext = current;
        self.plaintext_pos = 0;
        self.pending = spare;
        self.done = last;
        Ok(())
    }
}

impl<R: Read> Read for Decryptor<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        if buf.is_empty() {
            return Ok(0);
        }
        while self.plaintext_pos == self.plaintext.len() {
            if self.done {
                return Ok(0);
            }
            self.open_next()?;
        }
        let n = (self.plaintext.len() - self.plaintext_pos).min(buf.len());
        buf[..n].copy_from_slice(&self.plaintext[self.plaintext_pos..self.plaintext_pos + n]);
        self.plaintext_pos += n;
        Ok(n)
    }
}

fn age_err(msg: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, Error::Age(msg.into()))
}

fn read_line<R: BufRead>(input: &mut R) -> Result<Vec<u8>> {
    let mut line = Vec::new();
    input.read_until(b'\n', &mut line)?;
    if line.last() != Some(&b'\n') {
        return Err(Error::Age("header ended before its MAC line".into()));
    }
    line.pop();
    Ok(line)
}

fn b64_decode(field: &str, s: &[u8]) -> Result<Vec<u8>> {
    B64.decode(s)
        .map_err(|e| Error::Age(format!("{field} is not canonical base64: {e}")))
}

/// Parse and authenticate the header, returning the file key.
fn parse_header<R: BufRead>(input: &mut R, secret: &[u8; 32]) -> Result<[u8; FILE_KEY_LEN]> {
    let mut intro = vec![0u8; INTRO.len()];
    input
        .read_exact(&mut intro)
        .map_err(|_| Error::Age("not an age file (too short)".into()))?;
    if intro != INTRO {
        return Err(Error::Age(
            "not an age file (missing age-encryption.org/v1 intro)".into(),
        ));
    }

    let mut header = INTRO.to_vec();
    let mut stanza: Option<(Vec<u8>, Vec<u8>)> = None;
    let mac_line;

    loop {
        let line = read_line(input)?;
        if let Some(rest) = line.strip_prefix(b"-> ") {
            if stanza.is_some() {
                return Err(Error::Unsupported(
                    "age file has more than one recipient stanza; a store blob has exactly one"
                        .into(),
                ));
            }
            let args: Vec<&[u8]> = rest.split(|&b| b == b' ').collect();
            if args.first().copied() != Some(STANZA_X25519.as_bytes()) {
                return Err(Error::Unsupported(format!(
                    "age recipient stanza {:?} (this crate reads X25519 only)",
                    String::from_utf8_lossy(args.first().copied().unwrap_or(b""))
                )));
            }
            if args.len() != 2 {
                return Err(Error::Age(
                    "X25519 stanza does not carry exactly one argument".into(),
                ));
            }
            let epk = b64_decode("X25519 ephemeral share", args[1])?;
            header.extend_from_slice(&line);
            header.push(b'\n');

            // The body is base64 wrapped at 64 columns; it ends at the first
            // line shorter than 64 characters (possibly empty).
            let mut body = Vec::new();
            loop {
                let body_line = read_line(input)?;
                header.extend_from_slice(&body_line);
                header.push(b'\n');
                let full = body_line.len() == 64;
                body.extend_from_slice(&body_line);
                if !full {
                    break;
                }
            }
            let wrapped = b64_decode("X25519 stanza body", &body)?;
            stanza = Some((epk, wrapped));
        } else if line.starts_with(MAC_PREFIX) {
            mac_line = line;
            break;
        } else if line == b"---" {
            return Err(Error::Age("header MAC is missing".into()));
        } else {
            return Err(Error::Age(format!(
                "unexpected header line {:?}",
                String::from_utf8_lossy(&line)
            )));
        }
    }

    let (epk, wrapped) =
        stanza.ok_or_else(|| Error::Age("age file has no recipient stanza".into()))?;
    if epk.len() != 32 {
        return Err(Error::Age("X25519 ephemeral share is not 32 bytes".into()));
    }
    if wrapped.len() != FILE_KEY_LEN + TAG_LEN {
        return Err(Error::Age("X25519 stanza body is not 32 bytes".into()));
    }
    let mac = b64_decode("header MAC", &mac_line[MAC_PREFIX.len()..])?;
    if mac.len() != 32 {
        return Err(Error::Age("header MAC is not 32 bytes".into()));
    }

    let mut epk_bytes = [0u8; 32];
    epk_bytes.copy_from_slice(&epk);
    let identity = StaticSecret::from(*secret);
    let recipient = PublicKey::from(&identity).to_bytes();
    let shared = identity.diffie_hellman(&PublicKey::from(epk_bytes));
    if !shared.was_contributory() {
        return Err(Error::Age("X25519 shared secret is all zeros".into()));
    }
    let mut salt = [0u8; 64];
    salt[..32].copy_from_slice(&epk_bytes);
    salt[32..].copy_from_slice(&recipient);
    let mut wrap_key = hkdf32(shared.as_bytes(), Some(&salt), INFO_X25519);
    let file_key_vec = aead(&wrap_key)
        .decrypt(Nonce::from_slice(&[0u8; 12]), &wrapped[..])
        .map_err(|_| Error::Age("the file key does not unwrap with this identity".into()));
    wrap_key.zeroize();
    let mut file_key_vec = file_key_vec?;
    let mut file_key = [0u8; FILE_KEY_LEN];
    file_key.copy_from_slice(&file_key_vec);
    file_key_vec.zeroize();

    header.extend_from_slice(b"---");
    let expected = header_mac(&file_key, &header);
    if !subtle_eq(&expected, &mac) {
        file_key.zeroize();
        return Err(Error::Age("header MAC mismatch".into()));
    }

    Ok(file_key)
}

fn subtle_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for (x, y) in a.iter().zip(b) {
        diff |= x ^ y;
    }
    diff == 0
}

#[cfg(test)]
mod tests {
    use super::*;

    fn keypair(seed: u8) -> ([u8; 32], [u8; 32]) {
        let secret = [seed; 32];
        (secret, public_key(&secret))
    }

    fn encrypt(recipient: &[u8; 32], plaintext: &[u8]) -> Vec<u8> {
        let mut enc = Encryptor::new(recipient, Vec::new()).unwrap();
        // Write in odd-sized pieces to exercise chunk boundaries.
        for piece in plaintext.chunks(7_001) {
            enc.write_all(piece).unwrap();
        }
        enc.finish().unwrap()
    }

    fn decrypt(secret: &[u8; 32], file: &[u8]) -> Result<Vec<u8>> {
        let mut dec = Decryptor::new(secret, file)?;
        let mut out = Vec::new();
        dec.read_to_end(&mut out)?;
        Ok(out)
    }

    #[test]
    fn round_trips_across_chunk_boundaries() {
        let (secret, recipient) = keypair(7);
        for len in [
            0usize,
            1,
            13,
            CHUNK - 1,
            CHUNK,
            CHUNK + 1,
            2 * CHUNK,
            2 * CHUNK + 5,
            163_840,
        ] {
            let plaintext: Vec<u8> = (0..len).map(|i| (i % 251) as u8).collect();
            let file = encrypt(&recipient, &plaintext);
            assert!(file.starts_with(INTRO));
            assert_eq!(decrypt(&secret, &file).unwrap(), plaintext, "length {len}");
        }
    }

    #[test]
    fn the_wrong_identity_does_not_open() {
        let (_, recipient) = keypair(7);
        let (other, _) = keypair(8);
        let file = encrypt(&recipient, b"secret");
        assert!(matches!(decrypt(&other, &file), Err(Error::Age(_))));
    }

    #[test]
    fn tampering_is_detected() {
        let (secret, recipient) = keypair(7);
        let file = encrypt(&recipient, &vec![42u8; CHUNK + 10]);
        let dashes = file.windows(4).position(|w| w == b"--- ").unwrap();
        let payload_start =
            file[dashes..].iter().position(|&b| b == b'\n').unwrap() + dashes + 1 + NONCE_LEN;

        // A flipped payload byte in the first chunk.
        let mut altered = file.clone();
        altered[payload_start + 100] ^= 1;
        assert!(decrypt(&secret, &altered).is_err());

        // A flipped byte in the last chunk.
        let mut altered = file.clone();
        let last = altered.len() - 1;
        altered[last] ^= 1;
        assert!(decrypt(&secret, &altered).is_err());

        // Truncation: dropping the last chunk entirely leaves a full chunk
        // marked as not-last, which does not authenticate as last.
        let truncated = &file[..payload_start + CHUNK + TAG_LEN];
        assert!(decrypt(&secret, truncated).is_err());

        // A header byte.
        let mut altered = file.clone();
        altered[INTRO.len() + 5] ^= 1;
        assert!(decrypt(&secret, &altered).is_err());
    }

    #[test]
    fn refuses_what_is_outside_the_subset() {
        let (secret, recipient) = keypair(7);
        let file = encrypt(&recipient, b"x");
        let text =
            String::from_utf8_lossy(&file[..file.windows(4).position(|w| w == b"--- ").unwrap()])
                .into_owned();

        let scrypt = file.clone();
        let scrypt = String::from_utf8_lossy(&scrypt).replace("-> X25519", "-> scrypt");
        assert!(matches!(
            decrypt(&secret, scrypt.as_bytes()),
            Err(Error::Unsupported(_))
        ));

        let mut two = Vec::new();
        two.extend_from_slice(INTRO);
        two.extend_from_slice(&text.as_bytes()[INTRO.len()..]);
        two.extend_from_slice(&text.as_bytes()[INTRO.len()..]);
        two.extend_from_slice(&file[text.len()..]);
        assert!(matches!(decrypt(&secret, &two), Err(Error::Unsupported(_))));

        let no_mac = String::from_utf8_lossy(&file).replacen("--- ", "---\n", 1);
        assert!(decrypt(&secret, no_mac.as_bytes()).is_err());

        assert!(decrypt(&secret, b"not an age file at all").is_err());
    }

    #[test]
    fn low_order_recipients_are_refused() {
        assert!(check_recipient(&[0u8; 32]).is_err());
        let (_, recipient) = keypair(3);
        assert!(check_recipient(&recipient).is_ok());
    }
}
