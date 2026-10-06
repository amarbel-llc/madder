package store_key

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/piggy/go/pkgs/pigpen"
	"code.linenisgreat.com/piggy/go/pkgs/pigpen_resolve"
)

// The kinds of pigpen source a sealed-key store can record (FDR 0011).
const (
	// SourceKindPath: Locator is the absolute path of a local piggy-ids
	// file, in any of its forms.
	SourceKindPath = "path"

	// SourceKindPapi: Locator is a PAPI identity domain whose published
	// pigpen is fetched by `pigpen-resolver-papi-http`. Nothing specific
	// to one machine is recorded.
	SourceKindPapi = "papi"

	// SourceKindEmbedded: the pigpen document embedded in the store's
	// sidecar is itself the source. It may be a recipient set or a pointer.
	SourceKindEmbedded = "embedded"
)

// papiPointerKind is the pigpen-pointer kind PAPI's resolver answers to
// (papi RFC-0001 section 14, piggy RFC 0010).
const papiPointerKind = "papi-http"

// Source says where a sealed-key store's recipients come from.
type Source struct {
	Kind    string
	Locator string

	// Pigpen is the pigpen document embedded in the sidecar. For
	// SourceKindEmbedded it is the source of truth; for SourceKindPapi it
	// is the pointer; for SourceKindPath it is a snapshot of the
	// recipients at seal time, kept for the reader's benefit only.
	Pigpen string
}

// MakeSource builds the Source for `-pigpen value -pigpen-kind kind`. For
// path and embedded, value is a file path; for papi it is the identity
// domain. Nothing is resolved here.
func MakeSource(kind, value string) (source Source, err error) {
	source.Kind = kind

	switch kind {
	case SourceKindPath:
		// Absolute, so the pigpen is found again from any working directory.
		if source.Locator, err = filepath.Abs(value); err != nil {
			return source, err
		}

	case SourceKindPapi:
		source.Locator = value

		pointer, err := (&pigpen.Pointer{
			Kind:    papiPointerKind,
			Locator: value,
		}).MarshalText()
		if err != nil {
			return source, fmt.Errorf("pigpen pointer for %q: %w", value, err)
		}

		source.Pigpen = string(pointer)

	case SourceKindEmbedded:
		raw, err := os.ReadFile(value)
		if err != nil {
			return source, fmt.Errorf("reading pigpen: %w", err)
		}

		source.Pigpen = string(raw)

	default:
		return source, fmt.Errorf(
			"unknown pigpen kind %q (want %q, %q or %q)",
			kind,
			SourceKindPath,
			SourceKindPapi,
			SourceKindEmbedded,
		)
	}

	return source, nil
}

// String describes the source for a human.
func (source Source) String() string {
	switch source.Kind {
	case SourceKindEmbedded:
		return "embedded in blob_store-key"

	default:
		return fmt.Sprintf("%s (%s)", source.Locator, source.Kind)
	}
}

// document returns the pigpen document the source currently names.
func (source Source) document() ([]byte, error) {
	switch source.Kind {
	case SourceKindPath:
		raw, err := os.ReadFile(source.Locator)
		if err != nil {
			return nil, fmt.Errorf("reading pigpen: %w", err)
		}

		return raw, nil

	case SourceKindPapi, SourceKindEmbedded:
		if source.Pigpen == "" {
			return nil, fmt.Errorf("%s pigpen source has no pigpen document", source.Kind)
		}

		return []byte(source.Pigpen), nil

	default:
		return nil, fmt.Errorf("unknown pigpen kind %q", source.Kind)
	}
}

// Recipients returns the source's current encryption recipients.
//
// A pointer document is answered by an external resolver that makes
// network requests. With live false, the last answer cached on this machine
// is used when there is one, and nothing is fetched; a cold cache resolves
// once and keeps the answer. With live true the resolver always runs and
// the cache is refreshed. A failed resolve is an error either way: a stale
// answer is never substituted for one (piggy RFC 0010 section 5).
//
// Non-pointer documents are parsed directly and never cached.
func (source Source) Recipients(live bool) ([]markl.Id, error) {
	raw, err := source.document()
	if err != nil {
		return nil, err
	}

	isPointer := pigpen.IsPointer(raw)

	if isPointer && !live {
		if cached, ok := readCachedRecipients(raw); ok {
			return cached, nil
		}
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		recipientResolveTimeout,
	)
	defer cancel()

	recipients, err := pigpen_resolve.LoadRecipients(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("loading recipients from %s: %w", source, err)
	}

	if len(recipients) == 0 {
		return nil, fmt.Errorf("pigpen %s names no encryption recipients", source)
	}

	if isPointer {
		// Best effort: a cache that cannot be written costs a resolve next
		// time, nothing more.
		_ = writeCachedRecipients(raw, recipients)
	}

	return recipients, nil
}

// Snapshot renders recipients as a recipient-set pigpen document, for
// embedding in a sidecar.
func Snapshot(recipients []markl.Id) (string, error) {
	entries := make([]pigpen.Recipient, len(recipients))

	for i, id := range recipients {
		entries[i] = pigpen.Recipient{ID: id}
	}

	text, err := pigpen.NewRecipientSet(entries).MarshalText()
	if err != nil {
		return "", fmt.Errorf("rendering recipient set: %w", err)
	}

	return string(text), nil
}

// cacheDir is where resolved pointers are remembered on this machine.
func cacheDir() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")

	if base == "" {
		var err error

		if base, err = os.UserCacheDir(); err != nil {
			return "", err
		}
	}

	return filepath.Join(base, "madder", "pigpen"), nil
}

// cachePath names the cache entry for a pointer document: keyed on the
// pointer's bytes, so a different kind or locator is a different entry.
func cachePath(pointer []byte) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}

	hash, repool := markl.FormatHashBlake2b256.GetHash() //repool:owned
	defer repool()

	if _, err = hash.Write(pointer); err != nil {
		return "", err
	}

	digest, repoolDigest := hash.GetMarklId() //repool:owned
	defer repoolDigest()

	return filepath.Join(dir, digest.String()), nil
}

func readCachedRecipients(pointer []byte) ([]markl.Id, bool) {
	path, err := cachePath(pointer)
	if err != nil {
		return nil, false
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	recipients, err := pigpen.ParseRecipients(raw)
	if err != nil || len(recipients) == 0 {
		return nil, false
	}

	return recipients, true
}

func writeCachedRecipients(pointer []byte, recipients []markl.Id) error {
	path, err := cachePath(pointer)
	if err != nil {
		return err
	}

	text, err := Snapshot(recipients)
	if err != nil {
		return err
	}

	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "tmp-*")
	if err != nil {
		return err
	}

	if _, err = temp.WriteString(text); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())

		return err
	}

	if err = temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}

	if err = os.Rename(temp.Name(), path); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}

	return nil
}
