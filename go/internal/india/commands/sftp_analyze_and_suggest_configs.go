package commands

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	mathrand "math/rand"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/charlie/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	delta_blob_store_configs "github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/sftp_probe"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
)

func init() {
	utility.AddCmd("sftp-analyze-and-suggest-configs", &SftpAnalyzeAndSuggestConfigs{})
}

// SftpAnalyzeAndSuggestConfigs is a read-only probe of a legacy
// SFTP blob store. It samples blobs, generates candidate
// blob_store_configs.Config values that match the on-disk
// encoding, sample-verifies each candidate against the same
// sampled blobs, and offers an interactive deep-verify and
// bootstrap flow.
//
// See docs/plans/2026-05-08-sftp-analyze-and-suggest-configs-design.md
// for the full design and contract.
type SftpAnalyzeAndSuggestConfigs struct {
	command_components.EnvBlobStore

	sshHost        string
	remotePath     string
	knownHostsFile string
	keyPaths       []string
	limit          int
	maxSampleBytes int
	emitTop        int
	yesToAll       bool
}

func (cmd SftpAnalyzeAndSuggestConfigs) GetDescription() futility.Description {
	return futility.Description{
		Short: "analyze a legacy SFTP blob store and suggest blob_store-config candidates",
		Long: "Read-only probe of a legacy SFTP remote without a " +
			"blob_store-config file. Samples blobs, generates candidate " +
			"configs, sample-verifies them through the existing reader " +
			"pipeline, and offers an interactive deep-verify and bootstrap " +
			"flow. Emits candidate files to $TMPDIR. " +
			"See sftp-analyze-and-suggest-configs(1) for the full contract " +
			"and exit-code policy.",
	}
}

func (cmd *SftpAnalyzeAndSuggestConfigs) SetFlagDefinitions(
	flags interfaces.CLIFlagDefinitions,
) {
	flags.StringVar(&cmd.sshHost, "ssh-host", "",
		"ssh_config Host alias")
	flags.StringVar(&cmd.remotePath, "remote-path", "",
		"remote root containing buckets")
	flags.StringVar(&cmd.knownHostsFile, "known-hosts-file", "",
		"optional; default $HOME/.ssh/known_hosts")
	flags.IntVar(&cmd.limit, "limit", 10,
		"samples to draw")
	flags.IntVar(&cmd.maxSampleBytes, "max-sample-bytes", 1<<20,
		"skip blobs larger than this (bytes)")
	flags.IntVar(&cmd.emitTop, "emit-top", 5,
		"max candidate files to write")
	flags.BoolVar(&cmd.yesToAll, "yes-to-all", false,
		"auto-confirm every prompt; combine with non-tty for scripted runs")
	flags.Func("key", "age private key path; repeatable",
		func(v string) error {
			cmd.keyPaths = append(cmd.keyPaths, v)
			return nil
		})
}

func (cmd SftpAnalyzeAndSuggestConfigs) Run(req futility.Request) {
	env := cmd.MakeEnvBlobStore(req)

	if err := cmd.validateFlags(); err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}

	keys, err := loadAgeKeys(cmd.keyPaths)
	if err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}

	sftpClient, closeFn, err := cmd.dialSFTP(env)
	if err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}
	defer closeFn()

	if err := cmd.validateRemotePath(sftpClient); err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}

	layout, err := blob_stores.DiscoverRemoteConfig(
		sftpClient, cmd.remotePath, env.GetUI(),
	)
	if err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}

	samples, err := cmd.scatterSample(sftpClient, cmd.remotePath, layout)
	if err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}
	if len(samples) == 0 {
		errors.ContextCancelWithBadRequestf(env,
			"no blobs found at %q (sampled %d prefixes)",
			cmd.remotePath, 2*cmd.limit)
		return
	}

	candidates := sftp_probe.EnumerateCandidates(layout, keys)

	aggregates := make([]sftp_probe.Aggregate, len(candidates))
	for i := range candidates {
		aggregates[i].Candidate = candidates[i]
	}
	for _, s := range samples {
		for i := range candidates {
			r := sftp_probe.VerifySample(
				bytes.NewReader(s.buf),
				s.digestHex,
				candidates[i],
			)
			aggregates[i].Add(r)
		}
	}

	ranked := sftp_probe.Rank(aggregates)

	runDir, err := cmd.makeRunDir()
	if err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}

	cmd.emitTAP(env, ranked, runDir)
}

// sample is one buffered blob with its expected digest.
type sample struct {
	relPath   string
	digestHex string
	buf       []byte
}

func (cmd SftpAnalyzeAndSuggestConfigs) validateFlags() error {
	if cmd.sshHost == "" {
		return errors.Errorf("-ssh-host is required")
	}
	if cmd.remotePath == "" {
		return errors.Errorf("-remote-path is required")
	}
	if cmd.limit < 1 {
		return errors.Errorf("-limit must be >= 1")
	}
	if cmd.emitTop < 1 {
		return errors.Errorf("-emit-top must be >= 1")
	}
	if cmd.maxSampleBytes < 1024 {
		return errors.Errorf("-max-sample-bytes must be >= 1024")
	}
	for _, p := range cmd.keyPaths {
		if _, err := os.Stat(p); err != nil {
			return errors.Wrapf(err, "-key %q", p)
		}
	}
	return nil
}

func loadAgeKeys(paths []string) ([]markl.Id, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]markl.Id, 0, len(paths))
	for _, p := range paths {
		var key markl.Id
		if err := markl.SetFromPath(&key, p); err != nil {
			return nil, errors.Wrapf(err, "loading key %q", p)
		}
		out = append(out, key)
	}
	return out, nil
}

// dialSFTP connects via ssh_config-style transport. Constructs a
// throwaway TomlSFTPViaSSHConfigV0 from the cmd's flags so we can
// reuse blob_stores.MakeSSHClientFromSSHConfig directly.
func (cmd SftpAnalyzeAndSuggestConfigs) dialSFTP(
	env command_components.BlobStoreEnv,
) (*sftp.Client, func(), error) {
	uriStr := fmt.Sprintf("sftp://%s/%s",
		cmd.sshHost, strings.TrimPrefix(cmd.remotePath, "/"))

	var uri values.Uri
	if err := uri.Set(uriStr); err != nil {
		return nil, nil, errors.Wrapf(err, "parsing URI %q", uriStr)
	}

	cfg := &blob_store_configs.TomlSFTPViaSSHConfigV0{
		TomlUriV0: blob_store_configs.TomlUriV0{
			Uri: uri,
		},
		KnownHostsFile: cmd.knownHostsFile,
	}

	printer := ui.MakePrefixPrinter(
		ui.Err(),
		fmt.Sprintf("(sftp-analyze: %s) ", cmd.sshHost),
	)

	sshClient, err := blob_stores.MakeSSHClientFromSSHConfig(env, printer, cfg)
	if err != nil {
		return nil, nil, errors.Wrap(err)
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, errors.Wrapf(err, "sftp.NewClient")
	}

	closeFn := func() {
		_ = sftpClient.Close()
		_ = sshClient.Close()
	}

	return sftpClient, closeFn, nil
}

func (cmd SftpAnalyzeAndSuggestConfigs) validateRemotePath(
	sftpClient *sftp.Client,
) error {
	stat, err := sftpClient.Stat(cmd.remotePath)
	if err != nil {
		return errors.Wrapf(err, "stat %q", cmd.remotePath)
	}
	if !stat.IsDir() {
		return errors.Errorf(
			"-remote-path must be a directory; got file at %q",
			cmd.remotePath)
	}
	return nil
}

// scatterSample walks the bucket tree by picking random hex
// prefixes and returning up to cmd.limit blob samples buffered in
// memory. Stops when limit is reached or 2*limit attempts have
// failed (to bound work on sparse stores).
func (cmd SftpAnalyzeAndSuggestConfigs) scatterSample(
	sftpClient *sftp.Client,
	root string,
	layout blob_stores.DiscoveredConfig,
) ([]sample, error) {
	rng := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))

	topEntries, err := sftpClient.ReadDir(root)
	if err != nil {
		return nil, errors.Wrapf(err, "ReadDir %q", root)
	}

	// Filter the top level to bucket-shaped directory entries.
	var topBuckets []string
	for _, e := range topEntries {
		name := e.Name()
		if !e.IsDir() ||
			name == directory_layout.FileNameBlobStoreConfig ||
			strings.HasPrefix(name, "tmp_") ||
			name == "." || name == ".." {
			continue
		}
		topBuckets = append(topBuckets, name)
	}
	if len(topBuckets) == 0 {
		return nil, errors.Errorf(
			"no bucket-shaped entries at %q; not a blob store?", root)
	}

	out := make([]sample, 0, cmd.limit)
	maxAttempts := 2 * cmd.limit
	for attempts := 0; attempts < maxAttempts && len(out) < cmd.limit; attempts++ {
		s, ok := cmd.tryOneSample(sftpClient, root, topBuckets, layout, rng)
		if !ok {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (cmd SftpAnalyzeAndSuggestConfigs) tryOneSample(
	sftpClient *sftp.Client,
	root string,
	topBuckets []string,
	layout blob_stores.DiscoveredConfig,
	rng *mathrand.Rand,
) (sample, bool) {
	// Pick a random top-level bucket, then descend each subsequent
	// bucket level by random pick. layout.Buckets[0] is already
	// reflected in topBuckets; descend through the remaining levels.
	relSegments := []string{topBuckets[rng.Intn(len(topBuckets))]}

	for level := 1; level < len(layout.Buckets); level++ {
		dir := path.Join(append([]string{root}, relSegments...)...)
		entries, err := sftpClient.ReadDir(dir)
		if err != nil {
			return sample{}, false
		}
		var subdirs []string
		for _, e := range entries {
			if e.IsDir() {
				subdirs = append(subdirs, e.Name())
			}
		}
		if len(subdirs) == 0 {
			return sample{}, false
		}
		relSegments = append(relSegments, subdirs[rng.Intn(len(subdirs))])
	}

	// Now pick a file from the leaf directory.
	leafDir := path.Join(append([]string{root}, relSegments...)...)
	entries, err := sftpClient.ReadDir(leafDir)
	if err != nil {
		return sample{}, false
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), "tmp_") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return sample{}, false
	}
	file := files[rng.Intn(len(files))]
	relSegments = append(relSegments, file)
	relPath := strings.Join(relSegments, "/")
	fullPath := path.Join(root, relPath)

	// Reconstruct the expected digest by concatenating the bucket
	// path segments. The bucket layout slices a hex digest into
	// fixed-width chunks; the leaf filename is the remaining hex.
	// Any non-hex content means this isn't a real blob path.
	hexConcat := strings.ReplaceAll(relPath, "/", "")
	if _, err := hex.DecodeString(hexConcat); err != nil {
		return sample{}, false
	}
	digestHex := hexConcat

	// Buffer the file contents up to maxSampleBytes.
	f, err := sftpClient.Open(fullPath)
	if err != nil {
		return sample{}, false
	}
	defer f.Close()

	limited := io.LimitReader(f, int64(cmd.maxSampleBytes)+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return sample{}, false
	}
	if len(buf) > cmd.maxSampleBytes {
		// Oversized; skip.
		return sample{}, false
	}

	return sample{
		relPath:   relPath,
		digestHex: digestHex,
		buf:       buf,
	}, true
}

func (cmd SftpAnalyzeAndSuggestConfigs) makeRunDir() (string, error) {
	tmpdir := os.Getenv("TMPDIR")
	if tmpdir == "" {
		tmpdir = "/tmp"
	}
	var randBytes [8]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		return "", errors.Wrap(err)
	}
	runID := hex.EncodeToString(randBytes[:])
	dir := filepath.Join(tmpdir, "madder-suggest-"+runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.Wrap(err)
	}
	return dir, nil
}

func (cmd SftpAnalyzeAndSuggestConfigs) emitTAP(
	env command_components.BlobStoreEnv,
	ranked []sftp_probe.Aggregate,
	runDir string,
) {
	out := env.GetUIFile()

	// Bound the emitted candidates by emit-top, but only count
	// those that fully verified.
	emitted := 0
	fmt.Fprintln(out, "TAP version 14")
	fmt.Fprintf(out, "1..%d\n", min(cmd.emitTop, len(ranked)))

	for i, agg := range ranked {
		if i >= cmd.emitTop {
			break
		}
		emitted++
		ok := agg.Verified == agg.Total && agg.Total > 0
		status := "ok"
		if !ok {
			status = "not ok"
		}

		filename := fmt.Sprintf("candidate-%02d-%s.hyphence",
			i+1, strings.ReplaceAll(agg.Candidate.Label, "/", "-"))
		filePath := filepath.Join(runDir, filename)

		// Only emit a file for candidates that fully verified —
		// non-verifying candidates would just clutter $TMPDIR.
		if ok {
			if err := writeCandidateFile(filePath, agg.Candidate); err != nil {
				env.GetUI().Printf("write candidate %s failed: %v", filename, err)
				filePath = ""
			}
		} else {
			filePath = ""
		}

		fmt.Fprintf(out, "%s %d - %s verified=%d/%d\n",
			status, i+1, agg.Candidate.Label, agg.Verified, agg.Total)
		fmt.Fprintln(out, "  ---")
		if filePath != "" {
			fmt.Fprintf(out, "  path: %s\n", filePath)
		}
		fmt.Fprintf(out, "  label: %s\n", agg.Candidate.Label)
		fmt.Fprintf(out, "  verified: %d\n", agg.Verified)
		fmt.Fprintf(out, "  total: %d\n", agg.Total)
		fmt.Fprintln(out, "  stages:")
		for stage, count := range agg.Stages {
			fmt.Fprintf(out, "    %s: %d\n", stage, count)
		}
		if filePath != "" {
			fmt.Fprintln(out, "  bootstrap:")
			fmt.Fprintf(out,
				"    - ssh '%s' test ! -e '%s/blob_store-config' "+
					"|| { echo 'remote blob_store-config already exists; refusing'; exit 1; }\n",
				cmd.sshHost, cmd.remotePath)
			fmt.Fprintf(out,
				"    - scp '%s' '%s:%s/blob_store-config'\n",
				filePath, cmd.sshHost, cmd.remotePath)
			fmt.Fprintf(out,
				"    - ssh '%s' chmod 0444 '%s/blob_store-config'\n",
				cmd.sshHost, cmd.remotePath)
		}
		fmt.Fprintln(out, "  ...")
	}
}

func writeCandidateFile(filePath string, c sftp_probe.Candidate) error {
	typedConfig := &hyphence.TypedBlob[delta_blob_store_configs.Config]{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
		Blob: c.StoreConfig,
	}
	f, err := os.Create(filePath)
	if err != nil {
		return errors.Wrap(err)
	}
	defer f.Close()
	if _, err := delta_blob_store_configs.Coder.EncodeTo(typedConfig, f); err != nil {
		return errors.Wrap(err)
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
