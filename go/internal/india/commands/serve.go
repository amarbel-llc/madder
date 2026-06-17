package commands

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

func init() {
	utility.AddCmd("serve", &Serve{})
}

// Serve runs an admin-scoped HTTP blob API over a unix socket, backed by
// the configured blob store(s):
//
//	GET  /blobs/<digest>  stream the blob's bytes (404 if no store has it)
//	HEAD /blobs/<digest>  existence check (the "HAS")
//	PUT  /blobs/<digest>  write the request body; the daemon verifies the
//	                      stored content's digest equals <digest>
//
// It is the cross-process coordination surface for clients that cannot
// embed madder's go/pkgs in-process (the bulk hot path embeds the library
// directly, with no transport hop). See the circus nix-cache backend
// (FDR-0007). madder-mcp remains madder's stdio browse/traverse surface;
// this is the blob transport, never MCP.
//
// Without -store, the daemon serves the ambient env's stores (reads search
// default-then-remaining, writes go to the default store). With -store
// <id> it serves exactly that store, opened by id from its on-disk config;
// a system-scope (`//name`) -store roots the env's link(2) staging temp
// under the system root too, so writes colocate with the store
// (EXDEV/ProtectSystem-safe) — see madder#230.
type Serve struct {
	command_components.EnvBlobStore

	SocketPath string
	Store      string
}

var (
	_ interfaces.CommandComponentWriter = (*Serve)(nil)
	_ futility.CommandWithDescription   = Serve{}
)

func (cmd Serve) GetDescription() futility.Description {
	return futility.Description{
		Short: "serve a blob HTTP API over a unix socket",
		Long: "Run a long-lived admin daemon exposing a blob store over a " +
			"small HTTP API bound to a unix socket: GET /blobs/<digest> " +
			"streams a blob's bytes (404 if absent), HEAD /blobs/<digest> " +
			"is an existence check, and PUT /blobs/<digest> writes the " +
			"request body and verifies the stored content's content-" +
			"addressed digest equals <digest> (409 on mismatch). Without " +
			"-store, serves the ambient env's stores (reads search the " +
			"default then the remaining stores; writes go to the default). " +
			"With -store <blob-store-id>, serves exactly that store, opened " +
			"by id from its on-disk config — including a system-scope " +
			"//name store (madder#230), whose link(2) staging temp is " +
			"rooted under the system root so writes don't cross filesystems. " +
			"This is an admin/coordination surface, not a bulk-throughput " +
			"path. Requires -socket. Shuts down gracefully on SIGINT/SIGTERM.",
	}
}

func (cmd *Serve) SetFlagDefinitions(flagSet interfaces.CLIFlagDefinitions) {
	flagSet.StringVar(
		&cmd.SocketPath,
		"socket",
		"",
		"unix socket path to bind the HTTP blob API to (required), "+
			"e.g. /run/madder/madder.sock",
	)

	flagSet.StringVar(
		&cmd.Store,
		"store",
		"",
		"blob-store-id to serve (e.g. //default). Empty serves the "+
			"ambient env's stores. A system-scope //name roots the daemon's "+
			"temp under the system root for EXDEV-safe writes.",
	)
}

func (cmd Serve) Run(req futility.Request) {
	if cmd.SocketPath == "" {
		errors.ContextCancelWithBadRequestf(req, "missing required -socket")
		return
	}

	backend := cmd.makeBackend(req)
	if backend == nil {
		return // makeBackend cancelled the request
	}

	handler := &blobAPI{backend: backend}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /blobs/{digest}", handler.handleGet)
	mux.HandleFunc("HEAD /blobs/{digest}", handler.handleHead)
	mux.HandleFunc("PUT /blobs/{digest}", handler.handlePut)

	// Clear a stale socket left by an unclean prior shutdown before binding;
	// net.Listen("unix", ...) fails on an existing path.
	_ = os.Remove(cmd.SocketPath)

	listener, err := net.Listen("unix", cmd.SocketPath)
	if err != nil {
		errors.ContextCancelWithError(req, errors.Wrap(err))
		return
	}

	defer func() { _ = listener.Close() }()

	// 0660 so the owning user and a shared group can reach the socket. The
	// hybrid access model has an in-process library client and this daemon
	// touching one store via a common group; the systemd unit owns the
	// socket's group (see circus's services.madder module).
	if err := os.Chmod(cmd.SocketPath, 0o660); err != nil {
		errors.ContextCancelWithError(req, errors.Wrap(err))
		return
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	req.SetCancelOnSignals(syscall.SIGINT, syscall.SIGTERM)

	served := make(chan struct{})
	go func() {
		_ = server.Serve(listener)
		close(served)
	}()

	<-req.Context.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	<-served
}

// makeBackend builds the blob backend the daemon serves. Without -store it
// is the ambient env (default-then-remaining search); with -store it is the
// single store addressed by that id. A system-scope -store roots the env
// (and its temp) under the system root. Returns nil after cancelling req on
// a bad -store id.
func (cmd Serve) makeBackend(req futility.Request) blobBackend {
	if cmd.Store == "" {
		return envBackend{env: cmd.MakeEnvBlobStore(req)}
	}

	var id scoped_id.Id
	if err := id.Set(cmd.Store); err != nil {
		errors.ContextCancelWithBadRequestf(
			req, "invalid -store %q: %s", cmd.Store, err,
		)
		return nil
	}

	var envBlobStore command_components.BlobStoreEnv
	if id.GetLocationType() == scoped_id.LocationTypeXDGSystem {
		// System store: root the env's link(2) temp under the system root
		// too, so staging colocates with the store (madder#230).
		envBlobStore = cmd.MakeEnvBlobStoreSystemScoped(req)
	} else {
		envBlobStore = cmd.MakeEnvBlobStore(req)
	}

	return storeBackend{
		store: (&command_components.BlobStore{}).MakeBlobStoreByScopedId(
			envBlobStore, id,
		),
	}
}

// blobBackend abstracts the store(s) the daemon serves: the ambient env
// (search default-then-remaining) or one explicit -store.
type blobBackend interface {
	hasBlob(domain_interfaces.MarklId) bool
	openBlob(domain_interfaces.MarklId) (domain_interfaces.BlobReader, error)
	makeWriter() (domain_interfaces.BlobWriter, error)
}

// envBackend serves whatever the ambient env resolves.
type envBackend struct {
	env command_components.BlobStoreEnv
}

func (b envBackend) hasBlob(id domain_interfaces.MarklId) bool {
	return b.env.HasBlobInAnyStore(id)
}

func (b envBackend) openBlob(
	id domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	return b.env.OpenBlob(id)
}

func (b envBackend) makeWriter() (domain_interfaces.BlobWriter, error) {
	return b.env.GetDefaultBlobStore().MakeBlobWriter(nil)
}

// storeBackend serves a single explicit store (-store).
type storeBackend struct {
	store blob_stores.BlobStoreInitialized
}

func (b storeBackend) hasBlob(id domain_interfaces.MarklId) bool {
	return b.store.HasBlob(id)
}

func (b storeBackend) openBlob(
	id domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	return b.store.MakeBlobReader(id)
}

func (b storeBackend) makeWriter() (domain_interfaces.BlobWriter, error) {
	return b.store.MakeBlobWriter(nil)
}

// blobAPI serves the /blobs/<digest> routes against a blobBackend. One
// backend is opened per daemon and shared across requests, mirroring the
// MCP server's single-env model.
type blobAPI struct {
	backend blobBackend
}

func (h *blobAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseDigest(w, r)
	if !ok {
		return
	}

	reader, err := h.backend.openBlob(id)
	if err != nil {
		http.Error(w, "blob not found", http.StatusNotFound)
		return
	}

	defer func() { _ = reader.Close() }()

	// Writing the header commits 200; a mid-stream error cannot change the
	// status, which is standard for a streamed body.
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = reader.WriteTo(w)
}

func (h *blobAPI) handleHead(w http.ResponseWriter, r *http.Request) {
	id, ok := parseDigest(w, r)
	if !ok {
		return
	}

	if h.backend.hasBlob(id) {
		return // 200
	}

	http.Error(w, "blob not found", http.StatusNotFound)
}

func (h *blobAPI) handlePut(w http.ResponseWriter, r *http.Request) {
	id, ok := parseDigest(w, r)
	if !ok {
		return
	}

	defer func() { _ = r.Body.Close() }()

	writer, err := h.backend.makeWriter()
	if err != nil {
		http.Error(w, "open blob writer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := io.Copy(writer, r.Body); err != nil {
		_ = writer.Close()
		http.Error(w, "write blob: "+err.Error(), http.StatusBadRequest)
		return
	}

	// GetMarklId reflects the hash of the written bytes (the store finalizes
	// the content-addressed move on Close); match write.go's ordering.
	stored := writer.GetMarklId()

	if err := writer.Close(); err != nil {
		http.Error(w, "commit blob: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Content-addressed integrity: the bytes are stored under their true
	// digest regardless, so reject (not corrupt) when the client addressed a
	// different one.
	if stored.String() != id.String() {
		http.Error(
			w,
			"digest mismatch: body hashed to "+stored.String(),
			http.StatusConflict,
		)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// parseDigest reads the {digest} path value and parses it into a MarklId,
// writing a 400 and returning ok=false on a malformed digest.
func parseDigest(
	w http.ResponseWriter,
	r *http.Request,
) (domain_interfaces.MarklId, bool) {
	var id markl.Id

	if err := id.Set(r.PathValue("digest")); err != nil {
		http.Error(w, "invalid digest: "+err.Error(), http.StatusBadRequest)
		return nil, false
	}

	return &id, true
}
