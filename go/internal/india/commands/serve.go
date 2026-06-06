package commands

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ui"
)

func init() {
	utility.AddCmd("serve", &Serve{})
}

const (
	// defaultServeAddr binds loopback so a bare `madder serve` is
	// reachable for local development without exposing the blob store
	// to the network. Override with -addr ":8079" to bind all
	// interfaces.
	defaultServeAddr = "localhost:8079"

	// blobPathPrefix is the single route this server answers. A request
	// to /blobs/<markl-digest> streams the clear-text (decompressed,
	// decrypted) bytes of that blob.
	blobPathPrefix = "/blobs/"

	// serveShutdownTimeout bounds how long an in-flight blob transfer
	// has to finish after a SIGINT/SIGTERM before the server forces the
	// listener closed.
	serveShutdownTimeout = 5 * time.Second
)

// Serve runs a read-only HTTP server that streams blob contents by
// content-address. It is the HTTP sibling of `madder-mcp serve` (which
// speaks MCP over stdio): both expose the same blob stores, this one
// over `GET /blobs/<digest>` so an HTTP reverse proxy — e.g.
// linenisgreat's API fronting `/blobs` — can serve blobs without
// linking madder.
//
// Bytes are served as clear text: MakeBlobReader already decompresses
// and decrypts per the store's configured BlobIOWrapper, so the body is
// the original plaintext. Serving ciphertext (ebox/age) for client-side
// decryption is a deliberate future step, not this command.
type Serve struct {
	command_components.EnvBlobStore

	Addr string
}

var (
	_ futility.CommandWithDescription   = (*Serve)(nil)
	_ interfaces.CommandComponentWriter = (*Serve)(nil)
)

func (cmd Serve) GetDescription() futility.Description {
	return futility.Description{
		Short: "serve blob contents over HTTP by digest",
		Long: "Run a read-only HTTP server that streams the clear-text " +
			"contents of a blob in response to GET /blobs/<markl-digest> " +
			"(e.g. /blobs/blake2b256-...). Bytes are decompressed and " +
			"decrypted per the store's configuration before they leave " +
			"the wire, so the response body is the original plaintext. " +
			"When a digest is not held by the default store, the " +
			"remaining configured stores are searched automatically, " +
			"mirroring `madder cat`. Responses are marked immutable: a " +
			"content address never changes, so the bytes are safe to " +
			"cache forever. GET /healthz returns 200 for liveness probes. " +
			"This is the HTTP sibling of `madder-mcp serve`; it is " +
			"intended to sit behind a reverse proxy that adds TLS, CORS, " +
			"and access control. Bind address is set with -addr (default " +
			defaultServeAddr + ").",
	}
}

func (cmd *Serve) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(
		&cmd.Addr,
		"addr",
		defaultServeAddr,
		"host:port to bind the HTTP server (e.g. ':8079' for all interfaces)",
	)
}

func (cmd Serve) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	srv := &blobServer{source: envBlobSource{env: envBlobStore}}

	httpServer := &http.Server{
		Handler:           srv.mux(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	listener, err := net.Listen("tcp", cmd.Addr)
	if err != nil {
		errors.ContextCancelWithError(req, errors.Wrapf(err, "binding %q", cmd.Addr))
		return
	}

	ui.Err().Printf(
		"madder serve listening on http://%s%s<markl-digest>",
		listener.Addr(),
		blobPathPrefix,
	)

	req.SetCancelOnSignals(syscall.SIGINT, syscall.SIGTERM)

	// Translate context cancellation (signal or parent shutdown) into a
	// graceful HTTP shutdown so in-flight transfers get a chance to
	// drain before the listener closes.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-req.Context.Done()
		ctx, cancel := context.WithTimeout(
			context.Background(),
			serveShutdownTimeout,
		)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()

	if err := httpServer.Serve(listener); err != nil &&
		err != http.ErrServerClosed {
		errors.ContextCancelWithError(req, errors.Wrap(err))
	}

	<-shutdownDone
}

// blobSource is the minimal blob-lookup surface blobServer needs,
// extracted so HTTP routing can be unit-tested against an in-memory
// fake without standing up a real blob store env.
type blobSource interface {
	// Open returns a reader for the blob with the given id. found is
	// false (with a nil error) when no configured store holds the blob;
	// a non-nil error means a store reported the blob but could not be
	// read.
	Open(id domain_interfaces.MarklId) (
		reader io.ReadSeekCloser,
		found bool,
		err error,
	)
}

// envBlobSource resolves blobs against a real BlobStoreEnv, searching
// the default store first and then any remaining configured stores —
// the same default-then-rest walk `madder cat` and `madder-mcp serve`
// use.
type envBlobSource struct {
	env command_components.BlobStoreEnv
}

var _ blobSource = envBlobSource{}

func (s envBlobSource) Open(
	id domain_interfaces.MarklId,
) (io.ReadSeekCloser, bool, error) {
	def, remaining := s.env.GetDefaultBlobStoreAndRemaining()

	if reader, ok, err := tryOpenInStore(def, id); ok || err != nil {
		return reader, ok, err
	}

	for _, store := range remaining {
		if reader, ok, err := tryOpenInStore(store, id); ok || err != nil {
			return reader, ok, err
		}
	}

	return nil, false, nil
}

// tryOpenInStore reports whether one store holds the blob and, if so,
// returns a reader. A store backend that panics while answering (e.g.
// an unreachable SFTP store, see #134) is treated as "couldn't ask this
// store": ok=false with no error, so the caller continues to the next
// store instead of failing the request. Mirrors the recover discipline
// in commands_mcp.tryOpenInStore.
func tryOpenInStore(
	store blob_stores.BlobStoreInitialized,
	id domain_interfaces.MarklId,
) (reader io.ReadSeekCloser, ok bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			reader, ok, err = nil, false, nil
		}
	}()

	if !store.HasBlob(id) {
		return nil, false, nil
	}

	blobReader, err := store.MakeBlobReader(id)
	if err != nil {
		return nil, true, err
	}

	return blobReader, true, nil
}

// blobServer routes HTTP requests to blob lookups. It holds no mutable
// state, so a single instance serves all requests concurrently.
type blobServer struct {
	source blobSource
}

func (s *blobServer) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(blobPathPrefix, s.handleBlob)
	mux.HandleFunc("/healthz", s.handleHealth)
	return mux
}

func (s *blobServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

func (s *blobServer) handleBlob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}

	digest := strings.TrimPrefix(r.URL.Path, blobPathPrefix)
	if digest == "" || strings.Contains(digest, "/") {
		http.Error(
			w,
			"not found: expected "+blobPathPrefix+"<markl-digest>",
			http.StatusNotFound,
		)
		return
	}

	var id markl.Id
	if err := id.Set(digest); err != nil {
		http.Error(
			w,
			fmt.Sprintf("invalid digest %q: %v", digest, err),
			http.StatusBadRequest,
		)
		return
	}

	reader, found, err := s.source.Open(&id)
	if err != nil {
		ui.Err().Printf("madder serve: reading %s: %v", digest, err)
		http.Error(w, "blob store error", http.StatusBadGateway)
		return
	}
	if !found {
		http.Error(w, "blob not found: "+digest, http.StatusNotFound)
		return
	}
	defer func() { _ = reader.Close() }()

	// The local blob store's reader is forward-only — Seek reports
	// "seeker can't seek" — so http.ServeContent, which seeks to size
	// the body for Content-Length and Range support, is not usable
	// here. Stream instead. Reading the leading chunk up front does two
	// jobs: it feeds content-type sniffing, and it lets a backend read
	// error surface as a clean 502 before any status line is written.
	// Range support can be layered back on later by sizing the blob via
	// the store rather than the reader.
	head := make([]byte, 512)
	n, readErr := io.ReadFull(reader, head)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		ui.Err().Printf("madder serve: reading %s: %v", digest, readErr)
		http.Error(w, "blob store error", http.StatusBadGateway)
		return
	}
	head = head[:n]

	// A content address is immutable: the bytes for a digest never
	// change, so the response is safe to cache forever.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+digest+`"`)
	w.Header().Set("Content-Type", http.DetectContentType(head))

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	if _, err := w.Write(head); err != nil {
		return // client hung up mid-response
	}
	// io.Copy uses the BlobReader's WriteTo, resuming from where the
	// head read left off.
	if _, err := io.Copy(w, reader); err != nil {
		ui.Err().Printf("madder serve: streaming %s: %v", digest, err)
	}
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
