// Package main is the test-only SFTP server described in RFC 0001.
// Normally invoked by bats helpers via MADDER_PLUGIN_COOKIE; refuses
// to start without the envelope so accidental direct invocation on a
// shared machine fails loudly.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	programName     = "madder-test-sftp-server"
	protocolVersion = "1"
	subprotocol     = "ssh"
)

func main() {
	cookie := os.Getenv("MADDER_PLUGIN_COOKIE")
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "[%s] magic cookie mismatch\n", programName)
		os.Exit(1)
	}

	hostSigner, err := generateECDSAHostKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] host key: %v\n", programName, err)
		os.Exit(1)
	}

	knownHostsPath, err := writeKnownHosts(hostSigner.PublicKey())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] known_hosts: %v\n", programName, err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] listen: %v\n", programName, err)
		_ = os.Remove(knownHostsPath)
		os.Exit(1)
	}

	fmt.Printf(
		"%s|%s|tcp|%s|known_hosts=%s|%s\n",
		cookie,
		protocolVersion,
		listener.Addr().String(),
		knownHostsPath,
		subprotocol,
	)
	_ = os.Stdout.Sync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		// Per RFC 0001 Lifecycle: a closed stdin (EOF) is the sole
		// normative shutdown signal. Drain any input the parent
		// happens to send and treat EOF as graceful shutdown.
		_, _ = io.Copy(io.Discard, os.Stdin)
		cancel()
	}()

	served := make(chan struct{})
	go func() {
		serve(listener, hostSigner)
		close(served)
	}()

	<-ctx.Done()
	_ = listener.Close()
	<-served
	_ = os.Remove(knownHostsPath)
}

func generateECDSAHostKey() (ssh.Signer, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ecdsa key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("ssh signer: %w", err)
	}
	return signer, nil
}

// writeKnownHosts writes the host public key into a temp file in
// OpenSSH known_hosts format scoped to 127.0.0.1 (any port). The
// `[127.0.0.1]:*` host pattern means OpenSSH clients accept the key
// regardless of which ephemeral port we bound.
func writeKnownHosts(publicKey ssh.PublicKey) (string, error) {
	f, err := os.CreateTemp("", "madder-test-sftp-server-known_hosts-*")
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck

	line := fmt.Sprintf(
		"[127.0.0.1]:* %s %s\n",
		publicKey.Type(),
		base64.StdEncoding.EncodeToString(publicKey.Marshal()),
	)
	if _, err := f.WriteString(line); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func serve(listener net.Listener, hostSigner ssh.Signer) {
	config := &ssh.ServerConfig{
		PasswordCallback: func(
			conn ssh.ConnMetadata,
			password []byte,
		) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	config.AddHostKey(hostSigner)

	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go handleConnection(conn, config)
	}
}

func handleConnection(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close() //nolint:errcheck

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] ssh handshake failed: %v\n", programName, err)
		return
	}
	defer sshConn.Close() //nolint:errcheck

	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go serveSFTPChannel(channel, requests)
	}
}

func serveSFTPChannel(channel ssh.Channel, requests <-chan *ssh.Request) {
	for req := range requests {
		if req.Type == "subsystem" &&
			len(req.Payload) >= 4 &&
			string(req.Payload[4:]) == "sftp" {
			_ = req.Reply(true, nil)

			server, err := sftp.NewServer(channel)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%s] sftp server init failed: %v\n", programName, err)
				return
			}

			if err := server.Serve(); err != nil && err != io.EOF {
				fmt.Fprintf(os.Stderr, "[%s] sftp server: %v\n", programName, err)
			}

			_ = channel.Close()
			return
		}

		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}
}
