//go:build test

package blob_stores

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"

	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

// The benchmarks in this file target the specific knobs the SFTP-perf
// pass tuned: pkg/sftp's UseConcurrentReads/UseConcurrentWrites, the
// per-write MkdirAll cache, and the parallel bucket walk in
// allBlobsForBase. They run an in-process pkg/sftp server over an
// io.Pipe pair with optional per-packet latency injection, so each
// benchmark can show the win over a high-RTT link without needing a
// real network. The blob_io / hash / encryption layers are exercised
// in zz-tests_bats/sftp.bats end to end and are not the point here.

// benchLatency is the per-packet one-way "wire" delay the
// latencyWriter injects. 5 ms is comfortably above Linux's time.Sleep
// resolution (~1 ms) and gives a 10 ms nominal RTT — close enough to
// a real LAN-over-VPN link to be meaningful while keeping each
// benchmark iteration under a second. Round-trips per logical SFTP
// op stay modest (Open + N data packets + Close), so the gap between
// the sequential-default and the concurrent-options paths is clearly
// visible without making the bench slow.
const benchLatency = 5 * time.Millisecond

// latencyWriter wraps an io.WriteCloser to simulate a high-RTT link
// with proper pipelining. Each call to Write captures the packet
// payload, spawns an independent timer goroutine, and queues the
// packet for ordered delivery once the timer expires. Crucially the
// Write returns immediately so the *sftp.Client (which serializes
// outgoing packets behind a mutex) is unblocked to issue the next
// packet right away — multiple in-flight timers overlap, which is
// what enables UseConcurrentReads/Writes to show its win in a
// loopback bench. Without per-packet goroutines a serial Sleep would
// turn N pipelined requests into N * latency regardless of options.
type latencyWriter struct {
	dst     io.WriteCloser
	latency time.Duration

	queue  chan latencyPacket
	closed chan struct{}
}

type latencyPacket struct {
	data  []byte
	ready chan struct{}
}

func newLatencyWriter(dst io.WriteCloser, latency time.Duration) *latencyWriter {
	lw := &latencyWriter{
		dst:     dst,
		latency: latency,
		queue:   make(chan latencyPacket, 1024),
		closed:  make(chan struct{}),
	}
	go lw.deliver()
	return lw
}

func (lw *latencyWriter) Write(p []byte) (int, error) {
	data := append([]byte(nil), p...)
	pkt := latencyPacket{data: data, ready: make(chan struct{})}
	if lw.latency > 0 {
		go func() {
			time.Sleep(lw.latency)
			close(pkt.ready)
		}()
	} else {
		close(pkt.ready)
	}
	select {
	case lw.queue <- pkt:
	case <-lw.closed:
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func (lw *latencyWriter) Close() error {
	select {
	case <-lw.closed:
	default:
		close(lw.closed)
		close(lw.queue)
	}
	return nil
}

// deliver pops packets in FIFO order, waits for each packet's timer
// to fire, then writes the payload to the destination pipe. Order is
// preserved because the queue is FIFO; pipelining works because the
// timers run concurrently — the Nth packet's release time is set as
// soon as Write(N) returns, not after packet N-1 is delivered.
func (lw *latencyWriter) deliver() {
	for pkt := range lw.queue {
		select {
		case <-pkt.ready:
		case <-lw.closed:
			return
		}
		if _, err := lw.dst.Write(pkt.data); err != nil {
			return
		}
	}
	_ = lw.dst.Close()
}

// pipeRWC bundles an io.PipeReader and io.PipeWriter into the
// io.ReadWriteCloser pkg/sftp's NewServer expects.
type pipeRWC struct {
	r io.Reader
	w io.WriteCloser
}

func (p pipeRWC) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipeRWC) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipeRWC) Close() error                { return p.w.Close() }

// sftpBenchHarness holds an in-process sftp.Server and a connected
// sftp.Client, plus a temp directory the server is allowed to touch.
// The server runs in its own goroutine and shuts down when Close()
// breaks the pipes.
type sftpBenchHarness struct {
	client     *sftp.Client
	remotePath string
	closers    []io.Closer
}

func (h *sftpBenchHarness) Close() {
	for i := len(h.closers) - 1; i >= 0; i-- {
		_ = h.closers[i].Close()
	}
}

// newSFTPBenchHarness wires an in-process SFTP server to a client over
// io.Pipe pairs. Each Write on either side is delayed by `latency` so
// per-packet round trips approximate a 2*latency RTT link.
//
// The harness uses pkg/sftp's OS-backed NewServer, so the server reads
// and writes files on the host filesystem. remotePath is a per-bench
// t.TempDir() rooted under TMPDIR.
func newSFTPBenchHarness(
	tb testing.TB,
	latency time.Duration,
	clientOpts ...sftp.ClientOption,
) *sftpBenchHarness {
	tb.Helper()

	remotePath := tb.TempDir()

	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()

	var (
		clientWrite io.WriteCloser = c2sW
		serverWrite io.WriteCloser = s2cW
	)
	if latency > 0 {
		clientWrite = newLatencyWriter(c2sW, latency)
		serverWrite = newLatencyWriter(s2cW, latency)
	}

	serverRWC := pipeRWC{r: c2sR, w: serverWrite}
	server, err := sftp.NewServer(serverRWC)
	if err != nil {
		tb.Fatalf("NewServer: %v", err)
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = server.Serve()
	}()

	client, err := sftp.NewClientPipe(s2cR, clientWrite, clientOpts...)
	if err != nil {
		tb.Fatalf("NewClientPipe: %v", err)
	}

	h := &sftpBenchHarness{
		client:     client,
		remotePath: remotePath,
	}
	// Close in reverse order: client first (so the server sees EOF
	// and returns), then pipe ends, then wait for the server goroutine.
	h.closers = []io.Closer{
		client,
		c2sW, s2cW,
		c2sR, s2cR,
		closerFunc(func() error {
			select {
			case <-serverDone:
			case <-time.After(2 * time.Second):
			}
			return nil
		}),
	}
	return h
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// concurrentClientOptions mirrors the production initialize() path
// after the perf pass: UseConcurrentReads(true) is a no-op against
// pkg/sftp v1.13.10 defaults (disableConcurrentReads zeros to false,
// so concurrent reads are already on) but is set anyway as
// documentation of intent. UseConcurrentWrites(true) is the real
// behavior change — pkg/sftp defaults it to false because concurrent
// writes can leave holes on partial-failure paths, and we accept
// that trade for the throughput win on long-RTT links.
func concurrentClientOptions() []sftp.ClientOption {
	return []sftp.ClientOption{
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
	}
}

// sequentialReadsOptions explicitly disables concurrent reads so the
// GET benchmark can contrast "production concurrent" vs "old-school
// sequential" download paths. The production initialize() path never
// uses this; it exists only for the bench's A/B contrast.
func sequentialReadsOptions() []sftp.ClientOption {
	return []sftp.ClientOption{sftp.UseConcurrentReads(false)}
}

// writeBlobFile uploads a `size`-byte payload to remotePath/name via
// the SFTP client and returns the duration. The payload is a single
// allocation reused across iterations.
func writeBlobFile(
	tb testing.TB,
	client *sftp.Client,
	remoteFile string,
	payload []byte,
) {
	tb.Helper()
	f, err := client.Create(remoteFile)
	if err != nil {
		tb.Fatalf("Create(%q): %v", remoteFile, err)
	}
	if _, err := io.Copy(f, newRepeatReader(payload)); err != nil {
		_ = f.Close()
		tb.Fatalf("Copy to %q: %v", remoteFile, err)
	}
	if err := f.Close(); err != nil {
		tb.Fatalf("Close(%q): %v", remoteFile, err)
	}
}

// readBlobFile downloads remotePath/name to /dev/null via the SFTP
// client.
func readBlobFile(
	tb testing.TB,
	client *sftp.Client,
	remoteFile string,
) int64 {
	tb.Helper()
	f, err := client.Open(remoteFile)
	if err != nil {
		tb.Fatalf("Open(%q): %v", remoteFile, err)
	}
	defer f.Close()
	n, err := io.Copy(io.Discard, f)
	if err != nil {
		tb.Fatalf("Copy from %q: %v", remoteFile, err)
	}
	return n
}

// newRepeatReader wraps a payload in an io.Reader that streams it
// once and exhausts on EOF. It also implements Len() so pkg/sftp's
// (*File).ReadFrom can detect the source size and engage the
// concurrent-writes pipeline path; without Len/Size/Stat on the
// source pkg/sftp silently falls back to sequential writes
// regardless of UseConcurrentWrites — see client.go:2014-2092.
func newRepeatReader(payload []byte) *repeatReader {
	return &repeatReader{src: payload}
}

type repeatReader struct {
	src []byte
	pos int
}

func (r *repeatReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.src) {
		return 0, io.EOF
	}
	n := copy(p, r.src[r.pos:])
	r.pos += n
	return n, nil
}

func (r *repeatReader) Len() int { return len(r.src) - r.pos }

func benchBlobSizes(b *testing.B) []int {
	b.Helper()
	return []int{
		64 * 1024,
		1024 * 1024,
	}
}

// BenchmarkSFTPPut contrasts pkg/sftp's sequential default
// (UseConcurrentWrites=false) with the perf-pass setting
// (UseConcurrentWrites=true). With the latencyWriter injecting 5 ms
// per-packet RTT, the sequential path pays one RTT per MaxPacket-sized
// chunk; the concurrent path pipelines them and pays roughly one RTT
// for the whole transfer. The win grows with payload size.
func BenchmarkSFTPPut(b *testing.B) {
	for _, size := range benchBlobSizes(b) {
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i)
		}

		b.Run(fmt.Sprintf("Sequential/size=%d", size), func(b *testing.B) {
			h := newSFTPBenchHarness(b, benchLatency)
			defer h.Close()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				name := filepath.Join(
					h.remotePath, fmt.Sprintf("blob_seq_%d", i),
				)
				writeBlobFile(b, h.client, name, payload)
			}
		})

		b.Run(fmt.Sprintf("Concurrent/size=%d", size), func(b *testing.B) {
			h := newSFTPBenchHarness(
				b, benchLatency, concurrentClientOptions()...,
			)
			defer h.Close()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				name := filepath.Join(
					h.remotePath, fmt.Sprintf("blob_con_%d", i),
				)
				writeBlobFile(b, h.client, name, payload)
			}
		})
	}
}

// BenchmarkSFTPGet contrasts pkg/sftp's concurrent-reads default
// (the production path, also what
// initialize() now sets explicitly) with the historically-sequential
// fallback path callers hit on "read once" servers. For files larger
// than MaxPacket the concurrent path issues N read requests up to
// maxConcurrentRequests at a time; the sequential path issues one
// and waits.
func BenchmarkSFTPGet(b *testing.B) {
	for _, size := range benchBlobSizes(b) {
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i)
		}

		b.Run(fmt.Sprintf("Sequential/size=%d", size), func(b *testing.B) {
			h := newSFTPBenchHarness(
				b, benchLatency, sequentialReadsOptions()...,
			)
			defer h.Close()
			remoteFile := filepath.Join(h.remotePath, "blob")
			writeBlobFile(b, h.client, remoteFile, payload)
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				readBlobFile(b, h.client, remoteFile)
			}
		})

		b.Run(fmt.Sprintf("Concurrent/size=%d", size), func(b *testing.B) {
			h := newSFTPBenchHarness(
				b, benchLatency, concurrentClientOptions()...,
			)
			defer h.Close()
			remoteFile := filepath.Join(h.remotePath, "blob")
			writeBlobFile(b, h.client, remoteFile, payload)
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				readBlobFile(b, h.client, remoteFile)
			}
		})
	}
}

// populateBuckets seeds remotePath with a hash-bucketed tree of
// 1-byte placeholder files so AllBlobs has a realistic shape to
// walk. Returns the number of leaf files created.
func populateBuckets(tb testing.TB, client *sftp.Client, remotePath string) int {
	tb.Helper()
	// 16 top-level bucket dirs; 8 leaf files each = 128 blobs. With
	// buckets=[2] each leaf name is the rest of a sha256 digest hex
	// after the 2-char bucket prefix (62 hex chars). The walker only
	// checks shouldSkipBlobWalkEntry / IsDir; the actual byte content
	// of leaves doesn't matter for the walk benchmark.
	const topBuckets = 16
	const leavesPerBucket = 8
	leafSuffix := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	for i := 0; i < topBuckets; i++ {
		bucket := fmt.Sprintf("%02x", i)
		dir := filepath.Join(remotePath, bucket)
		if err := client.Mkdir(dir); err != nil && !os.IsExist(err) {
			tb.Fatalf("Mkdir(%q): %v", dir, err)
		}
		for j := 0; j < leavesPerBucket; j++ {
			leaf := filepath.Join(
				dir,
				fmt.Sprintf("%s%02x", leafSuffix[:len(leafSuffix)-2], j),
			)
			f, err := client.Create(leaf)
			if err != nil {
				tb.Fatalf("Create(%q): %v", leaf, err)
			}
			_ = f.Close()
		}
	}
	return topBuckets * leavesPerBucket
}

// BenchmarkSFTPAllBlobsWalk_Sequential runs the pre-pass behavior:
// a single goroutine calling sftpClient.Walk on the store root.
func BenchmarkSFTPAllBlobsWalk_Sequential(b *testing.B) {
	h := newSFTPBenchHarness(b, benchLatency, concurrentClientOptions()...)
	defer h.Close()
	populateBuckets(b, h.client, h.remotePath)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		walker := h.client.Walk(h.remotePath)
		for walker.Step() {
			if err := walker.Err(); err != nil {
				b.Fatalf("walker: %v", err)
			}
			if walker.Stat().IsDir() {
				continue
			}
			if shouldSkipBlobWalkEntry(filepath.Base(walker.Path())) {
				continue
			}
		}
	}
}

// BenchmarkSFTPAllBlobsWalk_Parallel runs the post-pass behavior:
// allBlobsForBase fans out across sftpAllBlobsWorkerCount workers.
// Uses a benchmark-only remoteSftp shell with the field set the
// parallel walk needs and the once gate latched so initialize() is
// skipped.
func BenchmarkSFTPAllBlobsWalk_Parallel(b *testing.B) {
	h := newSFTPBenchHarness(b, benchLatency, concurrentClientOptions()...)
	defer h.Close()
	populateBuckets(b, h.client, h.remotePath)

	store := newBenchStore(b, h.client, h.remotePath)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count := 0
		for _, err := range store.allBlobsForBase(
			h.remotePath,
			markl.FormatHashSha256,
		) {
			if err != nil {
				b.Fatalf("allBlobsForBase: %v", err)
			}
			count++
		}
		if count == 0 {
			b.Fatalf("walk yielded zero blobs")
		}
	}
}

// newBenchStore builds a remoteSftp wired to the benchmark's client.
// It latches once.Do with a no-op so initialize() (which needs an
// ssh.Client and a blob_store-config) never runs and the bench can
// call the post-init code paths directly.
func newBenchStore(
	tb testing.TB,
	client *sftp.Client,
	remotePath string,
) *remoteSftp {
	tb.Helper()
	var id blob_store_id.Id
	if err := id.Set("sftp-bench"); err != nil {
		tb.Fatalf("blob_store_id.Set: %v", err)
	}
	store := &remoteSftp{
		ctx:             benchContext{},
		id:              id,
		config:          benchRemotePathConfig{remotePath: remotePath},
		buckets:         defaultBuckets,
		defaultHashType: markl.FormatHashSha256,
		blobCache:       map[string]struct{}{},
		sftpClient:      client,
	}
	store.once.Do(func() {}) // latch: initialize() will not run
	return store
}

// benchRemotePathConfig is the minimal ConfigSFTPRemotePath
// implementation the store needs for GetRemotePath lookups. Other
// blob_store_configs.Config methods are unreachable in the bench
// paths (we don't call GetBlobStoreConfig or marshal).
type benchRemotePathConfig struct {
	remotePath string
}

func (c benchRemotePathConfig) GetRemotePath() string     { return c.remotePath }
func (c benchRemotePathConfig) GetKnownHostsFile() string { return "" }

// GetBlobStoreType satisfies domain_interfaces.BlobStoreConfig. The
// bench paths never serialize the config, but the production
// remoteSftp struct's `config` field is typed
// blob_store_configs.ConfigSFTPRemotePath which embeds
// BlobStoreConfig, so the method must exist for the struct literal
// to compile.
func (c benchRemotePathConfig) GetBlobStoreType() string { return "sftp-bench" }

// benchContext is a no-op interfaces.ActiveContext used by the bench
// store. The bench paths never trigger Cancel/After/Must.
type benchContext struct{}

func (benchContext) Deadline() (time.Time, bool)          { return time.Time{}, false }
func (benchContext) Done() <-chan struct{}                { return nil }
func (benchContext) Err() error                           { return nil }
func (benchContext) Value(_ any) any                      { return nil }
func (benchContext) Cause() error                         { return nil }
func (benchContext) GetState() interfaces.ContextState    { return interfaces.ContextStateStarted }
func (benchContext) Cancel(_ error)                       {}
func (benchContext) After(_ interfaces.FuncActiveContext) {}
func (benchContext) Must(_ interfaces.FuncActiveContext)  {}

var (
	_ context.Context          = benchContext{}
	_ interfaces.ActiveContext = benchContext{}
)
