package tree_capture_receipt

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
)

func TestWrite_HeaderExact(t *testing.T) {
	var buf bytes.Buffer

	if _, err := Write(&buf, nil); err != nil {
		t.Fatalf("Write empty: %v", err)
	}

	got := buf.String()
	if got != Header {
		t.Fatalf("empty receipt should be exactly Header; got %q", got)
	}

	const want = "---\n! madder-tree_capture-receipt-v1\n---\n\n"
	if got != want {
		t.Fatalf("Header drift; got %q want %q", got, want)
	}
}

func TestWrite_FileEntryShape(t *testing.T) {
	entries := []Entry{
		{
			Path:   "foo.go",
			Root:   "./src",
			Type:   TypeFile,
			Mode:   0o644,
			Size:   1234,
			BlobId: "blake3-x256-sha2-x256:deadbeef",
		},
	}

	var buf bytes.Buffer
	if _, err := Write(&buf, entries); err != nil {
		t.Fatalf("Write: %v", err)
	}

	body := strings.TrimPrefix(buf.String(), Header)
	const want = `{"path":"foo.go","root":"./src","type":"file","mode":"0644","size":1234,"blob_id":"blake3-x256-sha2-x256:deadbeef"}` + "\n"
	if body != want {
		t.Fatalf("file entry mismatch\ngot:  %q\nwant: %q", body, want)
	}
}

func TestWrite_DirEntryOmitsFileFields(t *testing.T) {
	entries := []Entry{
		{Path: ".", Root: "./src", Type: TypeDir, Mode: 0o755},
	}

	var buf bytes.Buffer
	if _, err := Write(&buf, entries); err != nil {
		t.Fatalf("Write: %v", err)
	}

	body := strings.TrimPrefix(buf.String(), Header)
	const want = `{"path":".","root":"./src","type":"dir","mode":"0755"}` + "\n"
	if body != want {
		t.Fatalf("dir entry mismatch\ngot:  %q\nwant: %q", body, want)
	}
}

func TestWrite_SymlinkEntryHasTargetNotBlob(t *testing.T) {
	entries := []Entry{
		{
			Path:   "link",
			Root:   "./src",
			Type:   TypeSymlink,
			Mode:   0o777,
			Target: "../bar",
		},
	}

	var buf bytes.Buffer
	if _, err := Write(&buf, entries); err != nil {
		t.Fatalf("Write: %v", err)
	}

	body := strings.TrimPrefix(buf.String(), Header)
	const want = `{"path":"link","root":"./src","type":"symlink","mode":"0777","target":"../bar"}` + "\n"
	if body != want {
		t.Fatalf("symlink entry mismatch\ngot:  %q\nwant: %q", body, want)
	}
}

func TestWrite_OtherEntryHasNoExtras(t *testing.T) {
	entries := []Entry{
		{Path: "fifo", Root: ".", Type: TypeOther, Mode: 0o600},
	}

	var buf bytes.Buffer
	if _, err := Write(&buf, entries); err != nil {
		t.Fatalf("Write: %v", err)
	}

	body := strings.TrimPrefix(buf.String(), Header)
	const want = `{"path":"fifo","root":".","type":"other","mode":"0600"}` + "\n"
	if body != want {
		t.Fatalf("other entry mismatch\ngot:  %q\nwant: %q", body, want)
	}
}

func TestWrite_ModePerm_LeadingZeros(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode fs.FileMode
		want string
	}{
		{"0644", 0o644, `"mode":"0644"`},
		{"0755", 0o755, `"mode":"0755"`},
		{"0600", 0o600, `"mode":"0600"`},
		{"0777", 0o777, `"mode":"0777"`},
		{"0007", 0o007, `"mode":"0007"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := mustWriteOne(t, &buf, Entry{
				Path:   "f",
				Root:   ".",
				Type:   TypeFile,
				Mode:   tc.mode,
				Size:   1,
				BlobId: "x",
			})
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			if !strings.Contains(buf.String(), tc.want) {
				t.Fatalf("missing %q in %q", tc.want, buf.String())
			}
		})
	}
}

func TestWrite_StripsHighModeBits(t *testing.T) {
	// fs.FileMode includes type bits (ModeDir, ModeSymlink) above the
	// permission bits. The receipt should render only the perm bits.
	entries := []Entry{
		{
			Path: ".",
			Root: ".",
			Type: TypeDir,
			Mode: fs.ModeDir | 0o755,
		},
	}

	var buf bytes.Buffer
	if _, err := Write(&buf, entries); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !strings.Contains(buf.String(), `"mode":"0755"`) {
		t.Fatalf("expected 0755 perm only, got %q", buf.String())
	}
}

func TestWrite_SortedByRootThenPath(t *testing.T) {
	entries := []Entry{
		{Path: "z.txt", Root: "./src", Type: TypeFile, Mode: 0o644, Size: 1, BlobId: "b1"},
		{Path: "a.txt", Root: "./src", Type: TypeFile, Mode: 0o644, Size: 2, BlobId: "b2"},
		{Path: "m.txt", Root: "./docs", Type: TypeFile, Mode: 0o644, Size: 3, BlobId: "b3"},
	}

	var buf bytes.Buffer
	if _, err := Write(&buf, entries); err != nil {
		t.Fatalf("Write: %v", err)
	}

	body := strings.TrimPrefix(buf.String(), Header)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), lines)
	}

	wantOrder := []string{
		`"root":"./docs"`,                  // ./docs sorts before ./src
		`"path":"a.txt","root":"./src"`,    // src/a before src/z
		`"path":"z.txt","root":"./src"`,
	}
	for i, want := range wantOrder {
		if !strings.Contains(lines[i], want) {
			t.Fatalf("line %d order: missing %q in %q", i, want, lines[i])
		}
	}
}

func TestWrite_DeterministicAcrossCalls(t *testing.T) {
	build := func() []Entry {
		return []Entry{
			{Path: "b", Root: ".", Type: TypeFile, Mode: 0o644, Size: 2, BlobId: "y"},
			{Path: "a", Root: ".", Type: TypeFile, Mode: 0o644, Size: 1, BlobId: "x"},
		}
	}

	var first, second bytes.Buffer
	if _, err := Write(&first, build()); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if _, err := Write(&second, build()); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("non-deterministic output:\n%q\n%q", first.String(), second.String())
	}
}

func TestWrite_RejectsUnknownType(t *testing.T) {
	entries := []Entry{
		{Path: "x", Root: ".", Type: "bogus", Mode: 0o644},
	}

	var buf bytes.Buffer
	if _, err := Write(&buf, entries); err == nil {
		t.Fatalf("expected error for unknown type, got nil")
	}
}

func mustWriteOne(t *testing.T, buf *bytes.Buffer, e Entry) error {
	t.Helper()
	_, err := Write(buf, []Entry{e})
	return err
}
