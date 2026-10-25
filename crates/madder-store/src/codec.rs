//! Blob compression (`compression-type`): `none` and `zstd`. A blob's id
//! is the digest of its plaintext, so compression never changes an id;
//! what it changes is the bytes on disk, which only ever need to be a
//! frame the other side's decoder accepts.

use std::io::{self, Read, Write};

use crate::{Error, Result};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Compression {
    None,
    Zstd,
}

impl Compression {
    /// The on-disk `compression-type` string. Go maps the empty string to
    /// `none` (the v1/v2 default). `gzip` and `zlib` exist in Go; pigsty
    /// does not need them, so they are refused loudly rather than read
    /// wrong.
    pub fn parse(s: &str) -> Result<Self> {
        match s {
            "" | "none" => Ok(Compression::None),
            "zstd" => Ok(Compression::Zstd),
            other => Err(Error::Unsupported(format!("compression type {other:?}"))),
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Compression::None => "none",
            Compression::Zstd => "zstd",
        }
    }

    /// Compress everything `src` yields into `dst`, to EOF.
    pub fn compress<R: Read, W: Write>(self, src: R, dst: &mut W) -> Result<()> {
        match self {
            Compression::None => {
                let mut src = src;
                io::copy(&mut src, dst)?;
                Ok(())
            }
            Compression::Zstd => {
                // ruzstd's encoder unwraps every I/O error, so the source and
                // drain are wrapped in adapters that record the first error
                // and report EOF / success to it; the error is raised after.
                let mut src = Fallible::new(src);
                let mut dst = Fallible::new(dst);
                ruzstd::encoding::compress(
                    &mut src,
                    &mut dst,
                    ruzstd::encoding::CompressionLevel::Fastest,
                );
                if let Some(e) = src.error.take() {
                    return Err(e.into());
                }
                if let Some(e) = dst.error.take() {
                    return Err(e.into());
                }
                Ok(())
            }
        }
    }

    /// A reader that yields the decompressed bytes of `src`.
    pub fn decompress<'a, R: Read + 'a>(self, src: R) -> Result<Box<dyn Read + 'a>> {
        match self {
            Compression::None => Ok(Box::new(src)),
            Compression::Zstd => {
                let decoder = ruzstd::decoding::StreamingDecoder::new(src)
                    .map_err(|e| Error::Codec(format!("zstd frame header: {e}")))?;
                Ok(Box::new(decoder))
            }
        }
    }
}

/// Wraps a reader or writer so that an I/O error is remembered instead of
/// returned, for a consumer that cannot handle one.
struct Fallible<T> {
    inner: T,
    error: Option<io::Error>,
}

impl<T> Fallible<T> {
    fn new(inner: T) -> Self {
        Fallible { inner, error: None }
    }
}

impl<R: Read> Read for Fallible<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        if self.error.is_some() {
            return Ok(0);
        }
        loop {
            match self.inner.read(buf) {
                Ok(n) => return Ok(n),
                Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
                Err(e) => {
                    self.error = Some(e);
                    return Ok(0);
                }
            }
        }
    }
}

impl<W: Write> Write for Fallible<&mut W> {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        if self.error.is_some() {
            return Ok(buf.len());
        }
        match self.inner.write_all(buf) {
            Ok(()) => Ok(buf.len()),
            Err(e) => {
                self.error = Some(e);
                Ok(buf.len())
            }
        }
    }

    fn flush(&mut self) -> io::Result<()> {
        if self.error.is_some() {
            return Ok(());
        }
        if let Err(e) = self.inner.flush() {
            self.error = Some(e);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn round_trip(c: Compression, plaintext: &[u8]) -> Vec<u8> {
        let mut compressed = Vec::new();
        c.compress(plaintext, &mut compressed).unwrap();
        let mut out = Vec::new();
        c.decompress(&compressed[..])
            .unwrap()
            .read_to_end(&mut out)
            .unwrap();
        assert_eq!(
            out,
            plaintext,
            "{c:?} round trip of {} bytes",
            plaintext.len()
        );
        compressed
    }

    #[test]
    fn none_is_identity() {
        assert_eq!(round_trip(Compression::None, b"hello"), b"hello");
        assert_eq!(round_trip(Compression::None, b""), b"");
    }

    #[test]
    fn zstd_round_trips_across_block_sizes() {
        let compressed = round_trip(Compression::Zstd, b"");
        assert_eq!(&compressed[..4], &[0x28, 0xb5, 0x2f, 0xfd], "zstd magic");
        round_trip(Compression::Zstd, b"hello, pigsty\n");
        let big: Vec<u8> = (0..300_000u32)
            .map(|i| (i.wrapping_mul(2654435761) >> 13) as u8)
            .collect();
        round_trip(Compression::Zstd, &big);
        let repetitive = vec![b'a'; 200_000];
        let compressed = round_trip(Compression::Zstd, &repetitive);
        assert!(
            compressed.len() < repetitive.len() / 10,
            "zstd compressed nothing"
        );
    }

    #[test]
    fn unknown_compression_is_refused() {
        assert!(Compression::parse("gzip").is_err());
        assert!(Compression::parse("brotli").is_err());
        assert_eq!(Compression::parse("").unwrap(), Compression::None);
    }

    #[test]
    fn a_failing_source_surfaces_its_error() {
        struct Broken;
        impl Read for Broken {
            fn read(&mut self, _: &mut [u8]) -> io::Result<usize> {
                Err(io::Error::other("disk on fire"))
            }
        }
        let mut out = Vec::new();
        let err = Compression::Zstd.compress(Broken, &mut out).unwrap_err();
        assert!(err.to_string().contains("disk on fire"), "{err}");
    }
}
