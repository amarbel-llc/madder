package blob_store_configs

import (
	"strings"

	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

// secretFormatIds are piggy's markl formats whose bytes are private key
// material. piggy names them with a `_sec` suffix (nonce being the one
// exception), and this list is the authority here rather than the
// `markl.FormatSec` type: piggy registers `age_x25519_pub` as a FormatSec
// too, for its encrypt-only IO wrapper, so that type says "has crypto
// functions", not "is a secret".
var secretFormatIds = map[string]struct{}{
	markl.FormatIdAgeX25519Sec: {},
	markl.FormatIdEd25519Sec:   {},
	markl.FormatIdNonceSec:     {},
}

// IsSecretKeyId reports whether id carries private key material, whatever
// purpose it carries. Such an id must never land in a remote
// blob_store-config, where it would sit beside the ciphertext it protects
// (ADR 0005 key-blind layering, madder#296).
func IsSecretKeyId(id markl.Id) bool {
	if id.IsNull() {
		return false
	}

	format := id.GetMarklFormat()
	if format == nil {
		return false
	}

	formatId := format.GetMarklFormatId()

	if _, known := secretFormatIds[formatId]; known {
		return true
	}

	return strings.HasSuffix(formatId, "_sec")
}

// FirstSecretKey reports whether any of keys IsSecretKeyId and names the
// first one without printing its bytes: purpose and format, as in
// "madder-private_key-v1@age_x25519_sec", or the bare format for an id
// with no purpose.
func FirstSecretKey(keys []markl.Id) (description string, found bool) {
	for _, key := range keys {
		if !IsSecretKeyId(key) {
			continue
		}

		description = key.GetMarklFormat().GetMarklFormatId()

		if purpose := key.GetPurposeId(); purpose != "" {
			description = purpose + "@" + description
		}

		return description, true
	}

	return "", false
}
