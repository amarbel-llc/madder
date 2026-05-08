package commands

import (
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
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

// Run is the futility entrypoint. Phase C.1 stops at flag wiring;
// Phase C.2 onward fills in the connect → sample → verify →
// emit → bootstrap pipeline.
func (cmd SftpAnalyzeAndSuggestConfigs) Run(req futility.Request) {
	env := cmd.MakeEnvBlobStore(req)
	_ = env
	// TODO(phase C.2): SSH dial, remote-path validation.
}
