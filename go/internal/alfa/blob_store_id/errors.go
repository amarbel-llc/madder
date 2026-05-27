package blob_store_id

import (
	"fmt"
)

// ErrIdDigestVsLegacyConfig is returned when a blob-store-id carries
// a digest suffix but the resolved store's config is legacy (no `@`
// line — pre-FDR-0008). Silently trusting an ID-supplied digest
// against an un-digestable config defeats the point of the suffix,
// so the error is hard and points the user at the migration command.
type ErrIdDigestVsLegacyConfig struct {
	Id string
}

func (err ErrIdDigestVsLegacyConfig) Error() string {
	return fmt.Sprintf(
		"blob-store-id %q supplied a digest but the store's config "+
			"is unmigrated. Run `madder config-pin_digest %s` to mint "+
			"a digest, then retry.",
		err.Id, err.Id,
	)
}

func (err ErrIdDigestVsLegacyConfig) Is(target error) bool {
	_, ok := target.(ErrIdDigestVsLegacyConfig)
	return ok
}
