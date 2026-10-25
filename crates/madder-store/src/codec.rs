//! Blob compression (`compression-type`): `none` and `zstd`. A blob's id
//! is the digest of its plaintext, so compression never changes an id;
//! what it changes is the bytes on disk, which only ever need to be a
//! frame the other side's decoder accepts.

use std::cell::RefCell;
use std::io::{self, Read, Write};
use std::rc::Rc;

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

    /// A reader that yields the decompressed bytes of `src`. A damaged
    /// stream fails with an [`io::ErrorKind::InvalidData`] error whose
    /// source is [`Error::Codec`] (recover it with [`Error::from_io`]); an
    /// error from `src` itself is returned as `src` raised it.
    pub fn decompress<'a, R: Read + 'a>(self, src: R) -> Result<Box<dyn Read + 'a>> {
        match self {
            Compression::None => Ok(Box::new(src)),
            Compression::Zstd => {
                // ruzstd folds every failure, the frame's and the source's,
                // into one opaque io::Error. The source's errors are kept
                // aside on the way in, so afterwards the two can be told
                // apart: a kept source error is returned as it was, anything
                // else is the frame's fault.
                let source_error = Rc::new(RefCell::new(None));
                let src = KeepErrors {
                    inner: src,
                    error: Rc::clone(&source_error),
                };
                let decoder = match ruzstd::decoding::StreamingDecoder::new(src) {
                    Ok(decoder) => decoder,
                    Err(e) => {
                        let kept = source_error.borrow_mut().take();
                        return Err(match kept {
                            Some(io) => Error::from_io(io),
                            None => Error::Codec(format!("zstd frame header: {e}")),
                        });
                    }
                };
                Ok(Box::new(ZstdReader {
                    decoder: Box::new(decoder),
                    source_error,
                }))
            }
        }
    }
}

/// Passes reads through, keeping the first error aside for [`ZstdReader`]
/// (an `io::Error` cannot be cloned, so the decoder gets one of the same
/// kind).
struct KeepErrors<R> {
    inner: R,
    error: Rc<RefCell<Option<io::Error>>>,
}

impl<R: Read> Read for KeepErrors<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        match self.inner.read(buf) {
            Ok(n) => Ok(n),
            Err(e) if e.kind() == io::ErrorKind::Interrupted => Err(e),
            Err(e) => {
                let kind = e.kind();
                let message = e.to_string();
                if self.error.borrow().is_none() {
                    *self.error.borrow_mut() = Some(e);
                }
                Err(io::Error::new(kind, message))
            }
        }
    }
}

struct ZstdReader<'a> {
    decoder: Box<dyn Read + 'a>,
    source_error: Rc<RefCell<Option<io::Error>>>,
}

impl Read for ZstdReader<'_> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        self.decoder.read(buf).map_err(|e| {
            let kept = self.source_error.borrow_mut().take();
            kept.unwrap_or_else(|| {
                io::Error::new(
                    io::ErrorKind::InvalidData,
                    Error::Codec(format!("zstd: {e}")),
                )
            })
        })
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

    fn big_compressed() -> Vec<u8> {
        let big: Vec<u8> = (0..300_000u32)
            .map(|i| (i.wrapping_mul(2654435761) >> 13) as u8)
            .collect();
        let mut compressed = Vec::new();
        Compression::Zstd
            .compress(&big[..], &mut compressed)
            .unwrap();
        compressed
    }

    #[test]
    fn a_damaged_zstd_stream_is_a_codec_error() {
        let mut compressed = big_compressed();
        compressed.truncate(compressed.len() - 10);
        let mut out = Vec::new();
        let err = Compression::Zstd
            .decompress(&compressed[..])
            .unwrap()
            .read_to_end(&mut out)
            .unwrap_err();
        assert!(
            matches!(Error::from_io(err), Error::Codec(_)),
            "a truncated frame must be the codec's error, not an I/O error"
        );
    }

    #[test]
    fn a_failing_zstd_source_keeps_its_own_error() {
        /// The first bytes of a valid frame, then a disk error.
        struct Dying {
            head: io::Cursor<Vec<u8>>,
        }
        impl Read for Dying {
            fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
                match self.head.read(buf)? {
                    0 => Err(io::Error::other("disk on fire")),
                    n => Ok(n),
                }
            }
        }
        let compressed = big_compressed();
        let src = Dying {
            head: io::Cursor::new(compressed[..40].to_vec()),
        };
        let err = match Compression::Zstd.decompress(src) {
            Err(e) => e,
            Ok(mut reader) => {
                let mut out = Vec::new();
                Error::from_io(reader.read_to_end(&mut out).unwrap_err())
            }
        };
        assert!(
            matches!(&err, Error::Io(io) if io.to_string().contains("disk on fire")),
            "{err:?}"
        );
    }
}
