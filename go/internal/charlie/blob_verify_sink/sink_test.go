package blob_verify_sink

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// syncBuffer mirrors the kernel's per-fd serialization of write(2)
// calls: each Write call lands in the buffer atomically, but ordering
// across goroutines is decided by who grabs the mutex first. Without
// this, a bare bytes.Buffer races and tears within a single Write — a
// stronger guarantee than what the kernel actually provides on a
// merged stdout/stderr fd, which would mask the bug we're testing for.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestJSONSink_ConcurrentWritesAreLineAtomic stresses the post-#154
// guarantee: every line on the (simulated) merged fd is either a
// complete JSON record or a "# "-prefixed TAP comment. Pre-fix, the
// bufio.Writer wrapping json.Encoder would split records across two
// underlying Write calls when the buffer wrapped, allowing a Notice
// to land mid-record. Post-fix, json.Encoder writes directly to the
// underlying writer in a single Write per record, so each Write
// syscall is a complete line.
func TestJSONSink_ConcurrentWritesAreLineAtomic(t *testing.T) {
	t.Parallel()

	var shared syncBuffer
	sink := NewJSON(&shared, &shared)

	const numGoroutines = 16
	const writesPerGoroutine = 200

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(2)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < writesPerGoroutine; i++ {
				sink.ReadError(
					fmt.Sprintf("store-%d", g),
					errors.New(fmt.Sprintf("err-%d-%d-with-some-padding-bytes-to-vary-length", g, i)),
				)
			}
		}(g)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < writesPerGoroutine; i++ {
				sink.Notice(fmt.Sprintf("(blob_store: store-%d) progress tick %d at %s", g, i, strings.Repeat("x", i%23)))
			}
		}(g)
	}
	wg.Wait()
	sink.Finalize()

	out := shared.String()
	lines := strings.Split(out, "\n")
	// strings.Split on "a\n" yields ["a", ""]; drop the trailing empty.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	expectedLines := numGoroutines * writesPerGoroutine * 2
	if len(lines) != expectedLines {
		t.Errorf(
			"expected %d lines, got %d (off-by-N usually means a Write was split across syscalls)",
			expectedLines, len(lines),
		)
	}

	jsonLines := 0
	noticeLines := 0
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") {
			noticeLines++
			continue
		}
		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf(
				"line %d is neither a JSON record nor a TAP comment: %q (err: %v)",
				i, line, err,
			)
		}
		if rec.State != StateReadError {
			t.Errorf("line %d: expected state=%q, got %q", i, StateReadError, rec.State)
		}
		jsonLines++
	}

	wantEach := numGoroutines * writesPerGoroutine
	if jsonLines != wantEach {
		t.Errorf("expected %d JSON records, got %d", wantEach, jsonLines)
	}
	if noticeLines != wantEach {
		t.Errorf("expected %d notice lines, got %d", wantEach, noticeLines)
	}
}

// TestJSONSink_NoticeIsTAPComment locks the wire format we contracted
// for in #154: every Notice line begins with "# " so a strict TAP14
// consumer reading the merged stream classifies it as a comment.
func TestJSONSink_NoticeIsTAPComment(t *testing.T) {
	t.Parallel()

	var out, errOut bytes.Buffer
	sink := NewJSON(&out, &errOut)

	sink.Notice("(blob_store: foo) hello")

	got := errOut.String()
	if got != "# (blob_store: foo) hello\n" {
		t.Errorf("Notice wire format: got %q, want %q", got, "# (blob_store: foo) hello\n")
	}
	if out.Len() != 0 {
		t.Errorf("Notice should not write to out; got %d bytes: %q", out.Len(), out.String())
	}
}
