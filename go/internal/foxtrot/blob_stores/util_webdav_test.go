//go:build test

package blob_stores

import (
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
)

// TestValidateWebdavAuth_AcceptsExactlyOneMode covers the four
// auth-mode shapes the design declares legal: anonymous, basic,
// bearer, TLS-client-cert. Each must validate without error.
func TestValidateWebdavAuth_AcceptsExactlyOneMode(t *testing.T) {
	cases := []struct {
		name   string
		config blob_store_configs.TomlWebDAVV0
	}{
		{"anonymous", blob_store_configs.TomlWebDAVV0{URL: "https://h/"}},
		{"basic", blob_store_configs.TomlWebDAVV0{URL: "https://h/", User: "u", Password: "p"}},
		{"bearer", blob_store_configs.TomlWebDAVV0{URL: "https://h/", BearerToken: "t"}},
		{"tls-cert", blob_store_configs.TomlWebDAVV0{URL: "https://h/", TLSClientCertPath: "/c", TLSClientKeyPath: "/k"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateWebdavAuth(&tc.config); err != nil {
				t.Errorf("%s rejected: %v", tc.name, err)
			}
		})
	}
}

// TestValidateWebdavAuth_RejectsMultipleModes pins the design's
// mutual-exclusivity rule. RFC 7235 doesn't define merging across
// Authorization sources; refusing at construction-time surfaces the
// error well before the first request.
func TestValidateWebdavAuth_RejectsMultipleModes(t *testing.T) {
	cases := []struct {
		name   string
		config blob_store_configs.TomlWebDAVV0
	}{
		{
			"basic + bearer",
			blob_store_configs.TomlWebDAVV0{URL: "https://h/", Password: "p", BearerToken: "t"},
		},
		{
			"basic + tls-cert",
			blob_store_configs.TomlWebDAVV0{URL: "https://h/", Password: "p", TLSClientCertPath: "/c"},
		},
		{
			"bearer + tls-cert",
			blob_store_configs.TomlWebDAVV0{URL: "https://h/", BearerToken: "t", TLSClientCertPath: "/c"},
		},
		{
			"all three",
			blob_store_configs.TomlWebDAVV0{URL: "https://h/", Password: "p", BearerToken: "t", TLSClientCertPath: "/c"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWebdavAuth(&tc.config)
			if err == nil {
				t.Fatalf("%s validated; want rejection", tc.name)
			}
			if !strings.Contains(err.Error(), "at most one of") {
				t.Errorf("error %q missing 'at most one of' anchor", err.Error())
			}
		})
	}
}
