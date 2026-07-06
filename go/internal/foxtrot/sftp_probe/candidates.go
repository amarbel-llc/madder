package sftp_probe

import (
	"fmt"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/plugins"
	_ "github.com/amarbel-llc/madder/go/internal/bravo/plugins/builtins"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
)

// legacyCompressionTypes is the fixed set of compression strings
// our candidates enumerate. Order matters: it determines the
// stable per-key ordering of the returned slice.
var legacyCompressionTypes = []string{"none", "gzip", "zlib", "zstd"}

// EnumerateCandidates returns the cross-product of legacy
// compression types and {no encryption, age + each user key}.
// All candidates use sha256 (the project default; the legacy
// stores we target are sha256-only per the design's
// non-goals).
//
// Order is deterministic. Outer loop is encryption (no-encryption
// first, then keys in input order). Inner loop is compression in
// legacyCompressionTypes order. The order is what later
// determines candidate-NN file naming.
func EnumerateCandidates(
	layout blob_stores.DiscoveredConfig,
	keys []markl.Id,
) []Candidate {
	out := make([]Candidate, 0, 4*(1+len(keys)))

	for _, comp := range legacyCompressionTypes {
		out = append(out, makeCandidate(layout, comp, nil, "none"))
	}

	for i := range keys {
		keyTag := fmt.Sprintf("age-key%d", i+1)
		// Take the address of the slice element to avoid
		// loop-variable aliasing on the captured pointer.
		k := &keys[i]
		for _, comp := range legacyCompressionTypes {
			out = append(out, makeCandidate(layout, comp, k, keyTag))
		}
	}

	return out
}

// makeCandidate constructs one Candidate from a (layout, comp, key)
// tuple. The StoreConfig is a hyphence-encodable DefaultType ready
// for emission as a blob_store-config file. The IOConfig is the
// reader-pipeline form for VerifySample.
func makeCandidate(
	layout blob_stores.DiscoveredConfig,
	comp string,
	key *markl.Id,
	keyTag string,
) Candidate {
	ref, err := plugins.LegacyCompressionRef(comp)
	if err != nil {
		// programming error: we control the input set
		panic(fmt.Sprintf("LegacyCompressionRef(%q): %v", comp, err))
	}
	wrapper, err := plugins.Resolve(ref)
	if err != nil {
		panic(fmt.Sprintf("plugins.Resolve(%q): %v", ref, err))
	}

	var enc domain_interfaces.MarklId
	if key != nil {
		enc = key
	}

	ioCfg := blob_io.MakeConfig(
		blob_store_configs.DefaultHashType,
		nil,
		wrapper,
		enc,
	)

	storeCfg := &blob_store_configs.DefaultType{
		HashTypeId:      blob_store_configs.HashType(layout.HashTypeId),
		HashBuckets:     layout.Buckets,
		CompressionType: comp,
		// Match the discovered layout: legacy single-hash stores
		// (`<root>/<bucket>/<rest>`) get SingleHash=true so the
		// emitted config doesn't claim a multi-hash layout the on-disk
		// tree doesn't actually have. Without this, fsck/info-repo/
		// sync would walk `<root>/<HashTypeId>/<bucket>/...` and miss
		// every blob.
		SingleHash: !layout.MultiHash,
	}
	if key != nil {
		storeCfg.Encryption = []markl.Id{*key}
	}

	return Candidate{
		StoreConfig: storeCfg,
		IOConfig:    ioCfg,
		Label:       comp + "/" + keyTag,
	}
}
