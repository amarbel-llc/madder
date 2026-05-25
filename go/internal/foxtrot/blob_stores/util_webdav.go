package blob_stores

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"

	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ui"
)

// validateWebdavAuth enforces mutual exclusivity between the four
// supported auth shapes: basic (password set), bearer (token set),
// TLS client cert (cert path set), and anonymous (none of the above).
// RFC 7235 doesn't define merging semantics for multiple Authorization
// sources; refusing at construction-time surfaces the error well
// before the first request.
func validateWebdavAuth(config blob_store_configs.ConfigWebDAV) error {
	var modes []string
	if config.GetPassword() != "" {
		modes = append(modes, "password")
	}
	if config.GetBearerToken() != "" {
		modes = append(modes, "bearer-token")
	}
	if config.GetTLSClientCertPath() != "" {
		modes = append(modes, "tls-client-cert-path")
	}

	if len(modes) > 1 {
		return errors.Errorf(
			"WebDAV auth: at most one of {password, bearer-token, "+
				"tls-client-cert-path} may be set; got %v",
			modes,
		)
	}

	// tls-client-key-path is meaningless without tls-client-cert-path —
	// the key would be loaded into TLS material that's never sent. Catch
	// it here rather than letting it silently no-op into an anonymous
	// connection.
	if config.GetTLSClientKeyPath() != "" && config.GetTLSClientCertPath() == "" {
		return errors.Errorf(
			"WebDAV auth: tls-client-key-path set without tls-client-cert-path",
		)
	}

	return nil
}

// MakeHTTPClientForWebDAVConfig builds an http.Client for a WebDAV
// blob-store config. TLS material is wired here (cert/key, CA bundle,
// ServerName, InsecureSkipVerify); per-request auth (basic, bearer)
// is applied in applyWebdavAuth.
func MakeHTTPClientForWebDAVConfig(
	_ interfaces.ActiveContext,
	_ ui.Printer,
	config blob_store_configs.ConfigWebDAV,
) (*http.Client, error) {
	tlsConfig, err := buildTLSConfig(config)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	if tlsConfig == nil {
		return &http.Client{}, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport}, nil
}

func buildTLSConfig(config blob_store_configs.ConfigWebDAV) (*tls.Config, error) {
	caPath := config.GetTLSCAPath()
	certPath := config.GetTLSClientCertPath()
	keyPath := config.GetTLSClientKeyPath()
	serverName := config.GetTLSServerName()
	insecure := config.GetTLSInsecureSkipVerify()

	if caPath == "" && certPath == "" && serverName == "" && !insecure {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: insecure, //nolint:gosec
	}

	if caPath != "" {
		caBytes, err := os.ReadFile(caPath)
		if err != nil {
			return nil, errors.Wrapf(err, "read TLS CA %q", caPath)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, errors.Errorf("TLS CA %q contains no PEM certificates", caPath)
		}
		tlsConfig.RootCAs = pool
	}

	if certPath != "" {
		if keyPath == "" {
			return nil, errors.Errorf("tls-client-cert-path requires tls-client-key-path")
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, errors.Wrapf(err, "load TLS client cert %q / key %q", certPath, keyPath)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}
