package blob_store_configs

import (
	"bytes"
	"io"
	"os"

	"github.com/amarbel-llc/hyphence/go/hyphence"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/files"
)

// metadataBodySeparator is the byte sequence the hyphence encoder
// emits between the closing metadata boundary and the body section:
// `---\n` (boundary line) + `\n` (blank separator line).
var metadataBodySeparator = []byte(hyphence.Boundary + "\n\n")

// DecodeAndVerify decodes a blob_store-config from r and, if its
// metadata carries a BlobDigest, hashes the on-disk body bytes and
// asserts the digests match via markl.AssertEqual. A config with no @
// line is trusted silently (pre-FDR-0008 back-compat). A mismatch
// returns markl.ErrNotEqual carrying both digests.
//
// Implementation: buffer the whole input, run Coder.DecodeFrom on the
// buffered bytes (populates BlobDigest from the metadata), then locate
// the body offset by searching for the metadata-end boundary and hash
// the raw on-disk body bytes. Buffering the whole config is acceptable
// because blob_store-config files are bounded (KB-scale).
//
// Hashing the raw on-disk body bytes (rather than re-encoding the
// decoded Config) keeps the contract symmetric with EncodeWithDigest
// — the same bytes that were hashed at write time are hashed at read
// time — and makes the verification independent of any
// encoder-determinism caveats.
func DecodeAndVerify(
	typedConfig *TypedConfig,
	r io.Reader,
) (n int64, err error) {
	all, err := io.ReadAll(r)
	if err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	n = int64(len(all))

	if _, err = Coder.DecodeFrom(typedConfig, bytes.NewReader(all)); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	if typedConfig.BlobDigest.IsNull() {
		return n, err
	}

	// Locate the body. The hyphence encoder emits `Boundary + "\n\n"`
	// immediately before the body section, so the first occurrence
	// after the opening boundary marks the body start. Use LastIndex
	// to be robust against any future metadata content that happens
	// to start with the same separator bytes (today none do).
	idx := bytes.LastIndex(all, metadataBodySeparator)
	if idx < 0 {
		err = errors.Errorf("could not locate body boundary in config")
		return n, err
	}
	bodyStart := idx + len(metadataBodySeparator)
	body := all[bodyStart:]

	hash, hashRepool := DigestHash.Get()
	defer hashRepool()
	if _, err = hash.Write(body); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	// Use a stack-allocated Id (rather than the pooled one returned by
	// hash.GetMarklId) so the bytes survive being stored in
	// markl.ErrNotEqual — a pooled Id would be repooled on return and
	// inspecting the error after the fact would see cleared bytes.
	var computed markl.Id
	if err = computed.SetMarklId(
		DigestHash.GetMarklFormatId(),
		hash.Sum(nil),
	); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	if err = computed.SetPurposeId(DigestPurpose); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	if err = markl.AssertEqual(&typedConfig.BlobDigest, &computed); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	return n, err
}

// DecodeAndVerifyFromFile is the file-aware wrapper around
// DecodeAndVerify, mirroring hyphence.DecodeFromFile's shape: a "-"
// path reads from stdin, any other path is opened read-only with the
// exclusive helper and closed on return.
func DecodeAndVerifyFromFile(path string) (typedConfig TypedConfig, err error) {
	var file *os.File

	if path == "-" {
		file = os.Stdin
	} else {
		if file, err = files.OpenExclusiveReadOnly(path); err != nil {
			err = errors.Wrap(err)
			return typedConfig, err
		}

		defer errors.DeferredCloser(&err, file)
	}

	if _, err = DecodeAndVerify(&typedConfig, file); err != nil {
		err = errors.Wrap(err)
		return typedConfig, err
	}

	return typedConfig, err
}
