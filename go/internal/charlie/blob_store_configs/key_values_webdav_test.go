//go:build test

package blob_store_configs

import "testing"

// TestConfigKeyValues_WebDAVRedactsSecrets pins the WebDAV-side
// redaction policy: URL, user, and the non-secret TLS material
// (cert path, CA path, server name, insecure-skip-verify) surface
// via ConfigKeyValues; password, bearer-token, and the private key
// path do NOT. Without this guard a later refactor could accidentally
// route a secret through `madder info-repo <store> <key>` and quietly
// leak it to anyone who can read the command's output.
func TestConfigKeyValues_WebDAVRedactsSecrets(t *testing.T) {
	config := &TomlWebDAVV0{
		URL:                   "https://host.example/dav/store-1/",
		User:                  "alice",
		Password:              "hunter2",
		BearerToken:           "bearer-secret-eyJhbGc",
		TLSClientCertPath:     "/etc/madder/client.crt",
		TLSClientKeyPath:      "/etc/madder/client.key",
		TLSCAPath:             "/etc/madder/ca.pem",
		TLSServerName:         "host.example",
		TLSInsecureSkipVerify: true,
	}

	keyValues := ConfigKeyValues(config)

	if got := keyValues["url"]; got != config.URL {
		t.Errorf("url = %q, want %q", got, config.URL)
	}
	if got := keyValues["user"]; got != config.User {
		t.Errorf("user = %q, want %q", got, config.User)
	}
	if got := keyValues["tls-client-cert-path"]; got != config.TLSClientCertPath {
		t.Errorf("tls-client-cert-path = %q, want %q", got, config.TLSClientCertPath)
	}
	if got := keyValues["tls-ca-path"]; got != config.TLSCAPath {
		t.Errorf("tls-ca-path = %q, want %q", got, config.TLSCAPath)
	}
	if got := keyValues["tls-server-name"]; got != config.TLSServerName {
		t.Errorf("tls-server-name = %q, want %q", got, config.TLSServerName)
	}
	if got := keyValues["tls-insecure-skip-verify"]; got != "true" {
		t.Errorf("tls-insecure-skip-verify = %q, want %q", got, "true")
	}

	for _, redactedKey := range []string{
		"password",
		"bearer-token",
		"tls-client-key-path",
	} {
		if v, present := keyValues[redactedKey]; present {
			t.Errorf("%s must not surface via ConfigKeyValues; got %q",
				redactedKey, v)
		}
	}

	// Spot-check that no configured secret literal appears anywhere
	// in the map. Anchors against an accidental-stringify regression
	// where a sibling field encodes the whole struct.
	secrets := []string{config.Password, config.BearerToken, config.TLSClientKeyPath}
	for k, v := range keyValues {
		for _, secret := range secrets {
			if v == secret {
				t.Errorf("secret value leaked via key %q = %q", k, v)
			}
		}
	}
}
