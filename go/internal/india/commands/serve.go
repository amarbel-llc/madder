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
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
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
type Serve struct {
	command_components.EnvBlobStore

	SocketPath string
}

var (
	_ interfaces.CommandComponentWriter = (*Serve)(nil)
	_ futility.CommandWithDescription   = Serve{}
)

func (cmd Serve) GetDescription() futility.Description {
	return futility.Description{
		Short: "serve a blob HTTP API over a unix socket",
		Long: "Run a long-lived admin daemon exposing the configured blob " +
			"store(s) over a small HTTP API bound to a unix socket: " +
			"GET /blobs/<digest> streams a blob's bytes (404 if absent), " +
			"HEAD /blobs/<digest> is an existence check, and " +
			"PUT /blobs/<digest> writes the request body and verifies the " +
			"stored content's content-addressed digest equals <digest> " +
			"(409 on mismatch). Reads search the default store then the " +
			"remaining configured stores; writes go to the default store. " +
			"This is an admin/coordination surface, not a bulk-throughput " +
			"path — bulk consumers embed madder's go/pkgs library directly. " +
			"Requires -socket. Shuts down gracefully on SIGINT/SIGTERM.",
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
}

func (cmd Serve) Run(req futility.Request) {
	if cmd.SocketPath == "" {
		errors.ContextCancelWithBadRequestf(req, "missing required -socket")
		return
	}

	envBlobStore := cmd.MakeEnvBlobStore(req)
	handler := &blobAPI{env: envBlobStore}

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

// blobAPI serves the /blobs/<digest> routes against a BlobStoreEnv. One
// env is opened per daemon and shared across requests, mirroring the MCP
// server's single-env model.
type blobAPI struct {
	env command_components.BlobStoreEnv
}

func (h *blobAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseDigest(w, r)
	if !ok {
		return
	}

	reader, err := h.env.OpenBlob(id)
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

	if h.env.HasBlobInAnyStore(id) {
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

	writer, err := h.env.GetDefaultBlobStore().MakeBlobWriter(nil)
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
