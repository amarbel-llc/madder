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

	"github.com/mattn/go-isatty"
	"github.com/pkg/sftp"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/charlie/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	delta_blob_store_configs "github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/sftp_probe"
	"github.com/amarbel-llc/madder/go/internal/futility"
	huhwrap "github.com/amarbel-llc/madder/go/internal/futility/huh"
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
			"flow. Emits candidate files to $TMPDIR/madder-suggest-<runid>/ " +
			"that the user can scp into place. " +
			"\n\n" +
			"Sample-verifies blobs against a cross-product of compression " +
			"types {none, gzip, zlib, zstd} and any user-supplied age keys " +
			"({none} \\u222a {age+keyi}). When the remote already has a " +
			"blob_store-config, the existing config is treated as " +
			"candidate-01 and validated alongside; a verified existing " +
			"config skips the bootstrap flow. " +
			"\n\n" +
			"Exit codes: 0 on clean success, 1 on a real failure " +
			"(connect, no-candidate-verifies, bootstrap-step-errored). " +
			"Exit code 2 (bootstrapped with consented deep-verify failures) " +
			"is reserved for a follow-up cli_main change.",
	}
}

// GetExamples surfaces a few sample invocations in the man page's
// EXAMPLES section.
func (cmd SftpAnalyzeAndSuggestConfigs) GetExamples() []futility.Example {
	return []futility.Example{
		{
			Description: "Probe a legacy store and emit candidate configs to $TMPDIR.",
			Command: "madder sftp-analyze-and-suggest-configs \\\n" +
				"  -ssh-host pihole-zz-inbox \\\n" +
				"  -remote-path blob_store",
		},
		{
			Description: "Probe an age-encrypted store with a known private key.",
			Command: "madder sftp-analyze-and-suggest-configs \\\n" +
				"  -ssh-host rsync.net \\\n" +
				"  -remote-path Library/Madder \\\n" +
				"  -key ~/.config/madder/keys/legacy.txt",
		},
		{
			Description: "Auto-confirm every prompt for scripted runs (cron, CI).",
			Command: "madder sftp-analyze-and-suggest-configs \\\n" +
				"  -ssh-host pihole-zz-inbox \\\n" +
				"  -remote-path blob_store \\\n" +
				"  -yes-to-all",
		},
	}
}

// GetSeeAlso wires the man-page SEE ALSO section.
func (cmd SftpAnalyzeAndSuggestConfigs) GetSeeAlso() []string {
	return []string{
		"madder-init-sftp-explicit",
		"madder-init-sftp-ssh_config",
		"madder-info-repo",
		"madder-fsck",
		"blob-store",
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

	// Existing-config validation: if the remote already has a
	// blob_store-config, decode it and prepend as candidate-01.
	// Validates the existing config in addition to the synthesized
	// candidates — a wrong existing config will fail verification
	// alongside.
	existingHas, existingCandidate := cmd.tryReadExistingConfig(env, sftpClient)
	if existingHas {
		candidates = append([]sftp_probe.Candidate{existingCandidate}, candidates...)
	}

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
	// If the existing candidate is present, pin it to position #1
	// so the user always sees their current state first.
	if existingHas {
		ranked = pinExistingFirst(ranked)
	}

	runDir, err := cmd.makeRunDir()
	if err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}

	cmd.emitTAP(env, ranked, runDir, existingHas)

	// Phase C.7-C.9: interactive deep-verify + bootstrap. Skipped
	// when existing already verifies fully (no remediation needed)
	// or when no candidate fully verified.
	cmd.runInteractiveFlow(env, sftpClient, ranked, existingHas)
}

// pinExistingFirst keeps the "existing" candidate (if present) at
// position 0 regardless of Rank's verified-desc + diversity
// tiebreak. The user must always see "what's on the remote right
// now" first, even if a synthesized candidate verifies higher.
func pinExistingFirst(ranked []sftp_probe.Aggregate) []sftp_probe.Aggregate {
	for i, agg := range ranked {
		if agg.Candidate.Label == "existing" {
			if i == 0 {
				return ranked
			}
			out := make([]sftp_probe.Aggregate, len(ranked))
			out[0] = agg
			j := 1
			for k, other := range ranked {
				if k == i {
					continue
				}
				out[j] = other
				j++
			}
			return out
		}
	}
	return ranked
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
//
// Note: blob_stores.MakeSSHClientFromSSHConfig does not actually
// parse ~/.ssh/config; it dials url.Hostname():url.Port() directly.
// Aliases that resolve via ssh_config Host blocks will only work
// if the user manually expands them or DNS happens to know about
// them. Tests pass a literal "host:port" via -ssh-host as a
// workaround. Real ssh_config parsing is a follow-up.
func (cmd SftpAnalyzeAndSuggestConfigs) dialSFTP(
	env command_components.BlobStoreEnv,
) (*sftp.Client, func(), error) {
	host := cmd.sshHost
	// If -ssh-host doesn't carry a "user@" prefix, default to
	// $USER so the URL parser produces a non-empty url.User.
	if !strings.Contains(host, "@") {
		user := os.Getenv("USER")
		if user == "" {
			user = "ssh"
		}
		host = user + "@" + host
	}

	uriStr := fmt.Sprintf("sftp://%s/%s",
		host, strings.TrimPrefix(cmd.remotePath, "/"))

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
	existingHas bool,
) {
	out := env.GetUIFile()

	fmt.Fprintln(out, "TAP version 14")
	fmt.Fprintf(out, "1..%d\n", min(cmd.emitTop, len(ranked)))

	for i, agg := range ranked {
		if i >= cmd.emitTop {
			break
		}
		ok := agg.Verified == agg.Total && agg.Total > 0
		isExisting := agg.Candidate.Label == "existing"
		status := "ok"
		if !ok {
			status = "not ok"
		}

		// Existing candidate is the on-remote config; never emit a
		// file for it (the user doesn't need a copy of what's
		// already there). Synthesized candidates that verify get a
		// file emitted. Non-verifying synthesized candidates get
		// no file (clutter avoidance).
		filePath := ""
		if ok && !isExisting {
			filename := fmt.Sprintf("candidate-%02d-%s.hyphence",
				i+1, strings.ReplaceAll(agg.Candidate.Label, "/", "-"))
			filePath = filepath.Join(runDir, filename)
			if err := writeCandidateFile(filePath, agg.Candidate); err != nil {
				env.GetUI().Printf("write candidate %s failed: %v", filename, err)
				filePath = ""
			}
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
		if isExisting {
			fmt.Fprintf(out,
				"  note: this is the config currently on the remote at %s/blob_store-config\n",
				cmd.remotePath)
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
	_ = existingHas // reserved for header banner in a future iteration
}

// tryReadExistingConfig opens <remote>/blob_store-config and
// decodes it into a Candidate labeled "existing". Returns
// (false, _) when the file is absent, decode-error'd, or when
// the decoded config doesn't expose the IO-wrapper interface
// VerifySample needs. Errors are logged via env.GetUI but never
// fatal — a missing/corrupt remote config is fine; the
// synthesized candidates still run.
func (cmd SftpAnalyzeAndSuggestConfigs) tryReadExistingConfig(
	env command_components.BlobStoreEnv,
	sftpClient *sftp.Client,
) (bool, sftp_probe.Candidate) {
	configPath := path.Join(cmd.remotePath, directory_layout.FileNameBlobStoreConfig)

	configFile, err := sftpClient.Open(configPath)
	if err != nil {
		// Most legacy stores have no blob_store-config — that's
		// the whole reason this command exists. Silent skip.
		return false, sftp_probe.Candidate{}
	}
	defer configFile.Close()

	var typedConfig hyphence.TypedBlob[delta_blob_store_configs.Config]
	if _, err := delta_blob_store_configs.Coder.DecodeFrom(
		&typedConfig, configFile,
	); err != nil {
		env.GetUI().Printf(
			"existing remote config at %s did not decode: %v",
			configPath, err)
		return false, sftp_probe.Candidate{}
	}

	wrapper, ok := typedConfig.Blob.(domain_interfaces.BlobIOWrapper)
	if !ok {
		env.GetUI().Printf(
			"existing remote config at %s does not provide an IOWrapper",
			configPath)
		return false, sftp_probe.Candidate{}
	}

	hashFmt := blob_store_configs.DefaultHashType
	ioCfg := blob_io.MakeConfig(
		hashFmt,
		nil,
		wrapper.GetBlobCompression(),
		wrapper.GetBlobEncryption(),
	)

	return true, sftp_probe.Candidate{
		StoreConfig: typedConfig.Blob,
		IOConfig:    ioCfg,
		Label:       "existing",
	}
}

// runInteractiveFlow drives the post-emit huh prompts: deep-verify
// the top candidate, then bootstrap it. Skipped silently when no
// candidate fully verifies, when the top candidate is "existing"
// and verifies (no remediation needed), or when stdin is non-tty
// and -yes-to-all wasn't set.
//
// Exit-code 2 (consented bootstrap-with-deep-verify-failures)
// would require modifying cli_main; today, both happy and
// consented-failure paths exit 0, and a real failure exits 1 via
// errors.ContextCancel*. The TAP `not ok` lines plus the
// diagnostic make the residual state legible without an exit-code
// distinction.
func (cmd SftpAnalyzeAndSuggestConfigs) runInteractiveFlow(
	env command_components.BlobStoreEnv,
	sftpClient *sftp.Client,
	ranked []sftp_probe.Aggregate,
	existingHas bool,
) {
	if len(ranked) == 0 {
		return
	}

	// Pick the top *synthesized* candidate (skip existing) for
	// the deep-verify + bootstrap path. If existing already
	// verifies, no synthesized bootstrap is wanted — the user's
	// current config works.
	var top sftp_probe.Aggregate
	var topIdx int = -1
	if existingHas && ranked[0].Candidate.Label == "existing" &&
		ranked[0].Verified == ranked[0].Total {
		// Existing verifies fully. Skip the prompts.
		return
	}
	for i, agg := range ranked {
		if agg.Candidate.Label == "existing" {
			continue
		}
		if agg.Verified == agg.Total && agg.Total > 0 {
			top = agg
			topIdx = i
			break
		}
	}
	if topIdx < 0 {
		return
	}

	// Deep-verify prompt.
	wantDeep, err := cmd.confirm(fmt.Sprintf(
		"Deep-verify %s against the full store?", top.Candidate.Label))
	if err != nil {
		env.GetUI().Printf("deep-verify prompt: %v", err)
		return
	}

	deepFailed := 0
	deepWalked := 0
	if wantDeep {
		deepWalked, deepFailed = cmd.runDeepVerify(env, sftpClient, top.Candidate)
		fmt.Fprintf(env.GetUIFile(),
			"# deep-verify: candidate=%s walked=%d failed=%d\n",
			top.Candidate.Label, deepWalked, deepFailed)

		if deepFailed > 0 {
			wantAnyway, err := cmd.confirm(fmt.Sprintf(
				"Deep-verify found %d failures of %d. Bootstrap anyway?",
				deepFailed, deepWalked))
			if err != nil {
				env.GetUI().Printf("bootstrap-anyway prompt: %v", err)
				return
			}
			if !wantAnyway {
				return
			}
		}
	}

	// Bootstrap prompt.
	wantBootstrap, err := cmd.confirm(fmt.Sprintf(
		"Bootstrap %s to %s:%s?",
		top.Candidate.Label, cmd.sshHost, cmd.remotePath))
	if err != nil {
		env.GetUI().Printf("bootstrap prompt: %v", err)
		return
	}
	if !wantBootstrap {
		return
	}

	if err := cmd.runBootstrap(env, sftpClient, top.Candidate, existingHas); err != nil {
		env.GetUI().Printf("bootstrap failed: %v", err)
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}
}

// confirm dispatches a yes/no prompt to huh when stdin is a tty,
// returns cmd.yesToAll otherwise.
func (cmd SftpAnalyzeAndSuggestConfigs) confirm(msg string) (bool, error) {
	if cmd.yesToAll {
		return true, nil
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return false, nil
	}
	return huhwrap.Prompter{}.Confirm(msg)
}

// runDeepVerify walks the entire bucket tree under cmd.remotePath
// and runs VerifySample against `cand` for every blob. Returns
// (walked, failed). Streams progress to stderr every 100 blobs.
func (cmd SftpAnalyzeAndSuggestConfigs) runDeepVerify(
	env command_components.BlobStoreEnv,
	sftpClient *sftp.Client,
	cand sftp_probe.Candidate,
) (walked, failed int) {
	walker := sftpClient.Walk(cmd.remotePath)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			continue
		}
		if walker.Stat().IsDir() {
			continue
		}
		// Skip the blob_store-config itself if present.
		if walker.Path() == path.Join(
			cmd.remotePath, directory_layout.FileNameBlobStoreConfig) {
			continue
		}

		relPath, err := filepath.Rel(cmd.remotePath, walker.Path())
		if err != nil {
			continue
		}
		hexConcat := strings.ReplaceAll(relPath, "/", "")
		if _, err := hex.DecodeString(hexConcat); err != nil {
			continue
		}

		f, err := sftpClient.Open(walker.Path())
		if err != nil {
			continue
		}
		buf, err := io.ReadAll(io.LimitReader(f, int64(cmd.maxSampleBytes)+1))
		_ = f.Close()
		if err != nil || len(buf) > cmd.maxSampleBytes {
			continue
		}

		walked++
		r := sftp_probe.VerifySample(bytes.NewReader(buf), hexConcat, cand)
		if !r.Ok {
			failed++
		}
		if walked%100 == 0 {
			env.GetUI().Printf(
				"deep-verify progress: walked=%d failed=%d", walked, failed)
		}
	}
	return walked, failed
}

// runBootstrap writes the chosen candidate's StoreConfig to the
// remote at <remote>/blob_store-config (mode 0444 per ADR 0005).
// When existingHas is true, the existing config is overwritten
// via a chmod-0644 -> write -> chmod-0444 sequence; otherwise the
// stock WriteRemoteConfig path is used (which refuses to
// overwrite).
func (cmd SftpAnalyzeAndSuggestConfigs) runBootstrap(
	env command_components.BlobStoreEnv,
	sftpClient *sftp.Client,
	cand sftp_probe.Candidate,
	existingHas bool,
) error {
	configPath := path.Join(cmd.remotePath, directory_layout.FileNameBlobStoreConfig)

	if existingHas {
		// Make existing writable, write the new config, restore
		// 0444. fileMode 0644 is enough to truncate-overwrite.
		if err := sftpClient.Chmod(configPath, 0o644); err != nil {
			return errors.Wrapf(err, "chmod 0644 %q", configPath)
		}
		// Overwrite via os.Create-equivalent: open with O_TRUNC
		// semantics. sftp.Create truncates if the path exists.
		f, err := sftpClient.Create(configPath)
		if err != nil {
			return errors.Wrapf(err, "create %q", configPath)
		}
		typedConfig := &hyphence.TypedBlob[delta_blob_store_configs.Config]{
			Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
			Blob: cand.StoreConfig,
		}
		if _, err := delta_blob_store_configs.Coder.EncodeTo(typedConfig, f); err != nil {
			_ = f.Close()
			return errors.Wrapf(err, "encode to %q", configPath)
		}
		if err := f.Close(); err != nil {
			return errors.Wrap(err)
		}
		if err := sftpClient.Chmod(configPath, 0o444); err != nil {
			return errors.Wrapf(err, "chmod 0444 %q", configPath)
		}
		env.GetUI().Printf("overwrote %s (mode 0444)", configPath)
		return nil
	}

	// Fresh write — uses the existing helper that does a
	// tmp-write + atomic rename + chmod 0444 + refuse-on-existing.
	discovered := blob_stores.DiscoveredConfig{
		HashTypeId: string(blob_store_configs.HashTypeSha256),
		Buckets:    blob_store_configs.DefaultHashBuckets,
	}
	// WriteRemoteConfig builds its own DefaultType from
	// `discovered`; for a candidate-driven write we want the
	// candidate's config to land instead. So we replicate
	// WriteRemoteConfig's atomic-write dance with our typed
	// blob.
	typedConfig := &hyphence.TypedBlob[delta_blob_store_configs.Config]{
		Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
		Blob: cand.StoreConfig,
	}
	tmpPath := configPath + ".tmp"
	f, err := sftpClient.Create(tmpPath)
	if err != nil {
		return errors.Wrapf(err, "create %q", tmpPath)
	}
	if _, err := delta_blob_store_configs.Coder.EncodeTo(typedConfig, f); err != nil {
		_ = f.Close()
		_ = sftpClient.Remove(tmpPath)
		return errors.Wrapf(err, "encode to %q", tmpPath)
	}
	if err := f.Close(); err != nil {
		_ = sftpClient.Remove(tmpPath)
		return errors.Wrap(err)
	}
	if err := sftpClient.Chmod(tmpPath, 0o444); err != nil {
		_ = sftpClient.Remove(tmpPath)
		return errors.Wrapf(err, "chmod %q", tmpPath)
	}
	if err := sftpClient.Rename(tmpPath, configPath); err != nil {
		_ = sftpClient.Remove(tmpPath)
		return errors.Wrapf(err, "rename %q -> %q", tmpPath, configPath)
	}
	env.GetUI().Printf("wrote %s (mode 0444)", configPath)
	_ = discovered
	return nil
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
