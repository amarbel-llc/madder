//go:build test

package blob_store_configs

import "testing"

// TestConfigKeyValues_WebDAVDropsPassword pins the WebDAV-side
// redaction policy: GetURL and GetUser surface via ConfigKeyValues,
// the password does NOT. Without this guard a later refactor could
// accidentally route the field through `madder info-repo <store>
// password` and quietly leak the secret to anyone who can read the
// command's output. Bearer-token and TLS-client-key-path redactions
// will be pinned analogously when the auth/TLS expansion lands.
func TestConfigKeyValues_WebDAVDropsPassword(t *testing.T) {
	config := &TomlWebDAVV0{
		URL:      "https://host.example/dav/store-1/",
		User:     "alice",
		Password: "hunter2",
	}

	keyValues := ConfigKeyValues(config)

	if got := keyValues["url"]; got != config.URL {
		t.Errorf("url = %q, want %q", got, config.URL)
	}
	if got := keyValues["user"]; got != config.User {
		t.Errorf("user = %q, want %q", got, config.User)
	}
	if _, present := keyValues["password"]; present {
		t.Errorf("password must not surface via ConfigKeyValues; got %q", keyValues["password"])
	}

	// Spot-check that the configured password literal never appears
	// anywhere in the map. Anchors against an accidental-stringify
	// regression where a sibling field encodes the whole struct.
	for k, v := range keyValues {
		if v == config.Password {
			t.Errorf("password value leaked via key %q = %q", k, v)
		}
	}
}
