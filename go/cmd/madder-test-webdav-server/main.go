// Package main is the test-only WebDAV server described in RFC 0001.
// Normally invoked by bats helpers via MADDER_PLUGIN_COOKIE; refuses
// to start without the envelope so accidental direct invocation on a
// shared machine fails loudly.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"golang.org/x/net/webdav"
)

const (
	programName     = "madder-test-webdav-server"
	protocolVersion = "1"
)

func main() {
	tlsEnabled := flag.Bool("tls", false, "serve over TLS with a self-signed cert (subprotocol 'https')")
	flag.Parse()

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

	var (
		tlsConfig   *tls.Config
		certPath    string
		subprotocol = "http"
	)

	// metadataParts accumulates the subprotocol_metadata key/value
	// pairs per RFC 0001 (joined with `&`). `root=` is always
	// present so bats helpers can locate the on-disk filesystem the
	// server is vending — used for on-disk-shape assertions like
	// the zstd-magic-after-write check (issue #187). `cert=` is
	// added in TLS mode so bats can pin the CA via -tls-ca-path.
	metadataParts := []string{"root=" + rootDir}

	if *tlsEnabled {
		var cert tls.Certificate
		var certPEM []byte
		cert, certPEM, err = generateSelfSignedCert()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] cert: %v\n", programName, err)
			_ = listener.Close()
			_ = os.RemoveAll(rootDir)
			os.Exit(1)
		}

		certPath, err = writeCertPEM(certPEM)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] write cert: %v\n", programName, err)
			_ = listener.Close()
			_ = os.RemoveAll(rootDir)
			os.Exit(1)
		}

		tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		listener = tls.NewListener(listener, tlsConfig)
		subprotocol = "https"
		metadataParts = append(metadataParts, "cert="+certPath)
	}

	metadataLine := strings.Join(metadataParts, "&")

	fmt.Printf(
		"%s|%s|tcp|%s|%s|%s\n",
		cookie,
		protocolVersion,
		addr.String(),
		metadataLine,
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
	if certPath != "" {
		_ = os.Remove(certPath)
	}
}

func generateSelfSignedCert() (tls.Certificate, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("ecdsa generate: %w", err)
	}

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "madder-test-webdav-server"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("create cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	cert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}
	return cert, certPEM, nil
}

func writeCertPEM(pemBytes []byte) (name string, err error) {
	var f *os.File
	if f, err = os.CreateTemp("", "madder-test-webdav-server-cert-*.pem"); err != nil {
		return "", err
	}
	defer errors.DeferredCloser(&err, f)
	if _, err = f.Write(pemBytes); err != nil {
		err = errors.Join(err, os.Remove(f.Name()))
		return "", err
	}
	return f.Name(), nil
}
