// Package main is the test-only WebDAV server described in RFC 0001.
// Normally invoked by bats helpers via MADDER_PLUGIN_COOKIE; refuses
// to start without the envelope so accidental direct invocation on a
// shared machine fails loudly.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/net/webdav"
)

const (
	programName     = "madder-test-webdav-server"
	protocolVersion = "1"
	subprotocol     = "http"
)

func main() {
	cookie := os.Getenv("MADDER_PLUGIN_COOKIE")
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "[%s] magic cookie mismatch\n", programName)
		os.Exit(1)
	}

	rootDir, err := os.MkdirTemp("", "madder-test-webdav-server-root-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] tmpdir: %v\n", programName, err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] listen: %v\n", programName, err)
		_ = os.RemoveAll(rootDir)
		os.Exit(1)
	}

	addr := listener.Addr().(*net.TCPAddr)

	fmt.Printf(
		"%s|%s|tcp|%s||%s\n",
		cookie,
		protocolVersion,
		addr.String(),
		subprotocol,
	)
	_ = os.Stdout.Sync()

	handler := &webdav.Handler{
		Prefix:     "/",
		FileSystem: webdav.Dir(rootDir),
		LockSystem: webdav.NewMemLS(),
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		// Per RFC 0001 Lifecycle: a closed stdin (EOF) is the sole
		// normative shutdown signal.
		_, _ = io.Copy(io.Discard, os.Stdin)
		cancel()
	}()

	served := make(chan struct{})
	go func() {
		_ = server.Serve(listener)
		close(served)
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	<-served
	_ = os.RemoveAll(rootDir)
}
