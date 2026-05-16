//go:build test

package blob_store_configs

import "testing"

// TestConfigKeyValues_S3RedactsSecrets pins the S3-side redaction
// policy: endpoint, region, bucket, prefix, access-key-id, and the
// boolean toggles surface via ConfigKeyValues; secret-access-key and
// session-token do NOT. Without this guard a later refactor could
// accidentally route a secret through `madder info-repo <store>
// <key>` and quietly leak it to anyone who can read the command's
// output.
func TestConfigKeyValues_S3RedactsSecrets(t *testing.T) {
	config := &TomlS3V0{
		Endpoint:               "https://s3.example.com",
		Region:                 "us-east-1",
		Bucket:                 "madder-blobs",
		Prefix:                 "store-1",
		AccessKeyId:            "AKIA0000000000000000",
		SecretAccessKey:        "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		SessionToken:           "session-token-secret",
		UsePathStyle:           true,
		InsecureSkipVerify:     true,
	}

	keyValues := ConfigKeyValues(config)

	if got := keyValues["endpoint"]; got != config.Endpoint {
		t.Errorf("endpoint = %q, want %q", got, config.Endpoint)
	}
	if got := keyValues["region"]; got != config.Region {
		t.Errorf("region = %q, want %q", got, config.Region)
	}
	if got := keyValues["bucket"]; got != config.Bucket {
		t.Errorf("bucket = %q, want %q", got, config.Bucket)
	}
	if got := keyValues["prefix"]; got != config.Prefix {
		t.Errorf("prefix = %q, want %q", got, config.Prefix)
	}
	if got := keyValues["access-key-id"]; got != config.AccessKeyId {
		t.Errorf("access-key-id = %q, want %q", got, config.AccessKeyId)
	}
	if got := keyValues["use-path-style"]; got != "true" {
		t.Errorf("use-path-style = %q, want %q", got, "true")
	}
	if got := keyValues["insecure-skip-tls-verify"]; got != "true" {
		t.Errorf("insecure-skip-tls-verify = %q, want %q", got, "true")
	}

	for _, redactedKey := range []string{
		"secret-access-key",
		"session-token",
	} {
		if v, present := keyValues[redactedKey]; present {
			t.Errorf("%s must not surface via ConfigKeyValues; got %q",
				redactedKey, v)
		}
	}

	// Spot-check that no configured secret literal appears anywhere
	// in the map. Anchors against an accidental-stringify regression
	// where a sibling field encodes the whole struct.
	secrets := []string{config.SecretAccessKey, config.SessionToken}
	for k, v := range keyValues {
		for _, secret := range secrets {
			if v == secret {
				t.Errorf("secret value leaked via key %q = %q", k, v)
			}
		}
	}
}
