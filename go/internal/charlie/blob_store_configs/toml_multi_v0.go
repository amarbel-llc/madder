package blob_store_configs

import (
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

//go:generate tommy generate
type TomlMultiV0 struct {
	Mode         string         `toml:"mode"`
	WriteStore   scoped_id.Id   `toml:"write-store,omitempty"`
	ReadStores   []scoped_id.Id `toml:"read-stores,omitempty"`
	MirrorStores []scoped_id.Id `toml:"mirror-stores,omitempty"`
	// ReadFill is a pointer so an absent key (nil) can default to
	// true (FDR-0009). A present `read-fill = false` disables tee.
	ReadFill *bool `toml:"read-fill,omitempty"`
}

func (TomlMultiV0) GetBlobStoreType() string {
	return "multi"
}

func (cfg TomlMultiV0) GetMode() string {
	return cfg.Mode
}

func (cfg TomlMultiV0) GetWriteStore() scoped_id.Id {
	return cfg.WriteStore
}

func (cfg TomlMultiV0) GetReadStores() []scoped_id.Id {
	return cfg.ReadStores
}

func (cfg TomlMultiV0) GetMirrorStores() []scoped_id.Id {
	return cfg.MirrorStores
}

// GetReadFill defaults to true when the key is absent (FDR-0009).
func (cfg TomlMultiV0) GetReadFill() bool {
	return cfg.ReadFill == nil || *cfg.ReadFill
}

// Validate enforces the FDR-0009 invariant that every reference inside
// a multi config is digest-bearing. It is called by the hyphence Coder
// at decode time (Task 3), so a hand-edited config with a bare
// reference fails to read. Malformed digests are caught earlier by
// scoped_id.Id.UnmarshalText during decode.
func (cfg TomlMultiV0) Validate() error {
	check := func(role string, id scoped_id.Id) error {
		if !id.HasDigest() {
			return errors.BadRequestf(
				"multi %s reference %q must be digest-bearing "+
					"(name@blake2b256-…); bare references are forbidden "+
					"inside a multi config", role, id.String(),
			)
		}
		return nil
	}
	if !cfg.WriteStore.IsEmpty() {
		if err := check("write-store", cfg.WriteStore); err != nil {
			return err
		}
	}
	for _, id := range cfg.ReadStores {
		if err := check("read-store", id); err != nil {
			return err
		}
	}
	for _, id := range cfg.MirrorStores {
		if err := check("mirror-store", id); err != nil {
			return err
		}
	}
	return nil
}

// SetFlagDefinitions makes TomlMultiV0 a ConfigMutable so the generic
// init machinery accepts it. init-multi assembles the config from its
// own typed flags (Task 6) rather than these, so this is intentionally
// minimal; the fields are not user-settable via the generic flag path.
func (cfg *TomlMultiV0) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
}
