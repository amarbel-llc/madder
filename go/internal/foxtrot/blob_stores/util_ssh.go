package blob_stores

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/ui"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// defaultSSHDialTimeout bounds ssh.Dial when ssh.ClientConfig.Timeout is
// otherwise unset, so a wedged remote does not block CLI commands
// indefinitely. ssh.Dial honors ClientConfig.Timeout via its default
// net.Dialer.
const defaultSSHDialTimeout = 30 * time.Second

// sshConfigResolution holds the subset of ssh_config fields we care
// about. Fields are populated by resolveSSHConfig from `ssh -G`
// output; empty strings mean "use the URI's value".
type sshConfigResolution struct {
	Hostname           string
	Port               string
	User               string
	UserKnownHostsFile string
}

// resolveSSHConfig shells out to `ssh -G <host>` to expand
// ssh_config Host blocks (Match, Include, ProxyJump and friends
// included). Real ssh's resolver is the source of truth; we just
// consume its output. Returns an empty resolution when ssh isn't
// on PATH or the call errors — caller falls back to URI-literal
// dialing.
//
// Closes amarbel-llc/madder#142.
func resolveSSHConfig(host string) (sshConfigResolution, error) {
	if host == "" {
		return sshConfigResolution{}, nil
	}
	cmd := exec.Command("ssh", "-G", host)
	out, err := cmd.Output()
	if err != nil {
		return sshConfigResolution{}, err
	}

	var r sshConfigResolution
	for _, line := range strings.Split(string(out), "\n") {
		key, val, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch key {
		case "hostname":
			r.Hostname = val
		case "port":
			r.Port = val
		case "user":
			r.User = val
		case "userknownhostsfile":
			// `ssh -G` may emit a space-separated list; we use
			// the first entry (which is the highest-priority
			// match per ssh's own precedence rules).
			r.UserKnownHostsFile = strings.SplitN(val, " ", 2)[0]
		}
	}
	return r, nil
}

func MakeSSHAgent(
	ctx interfaces.ActiveContext,
	uiPrinter ui.Printer,
) (sshAgent agent.ExtendedAgent, err error) {
	socket := os.Getenv("SSH_AUTH_SOCK")

	if socket == "" {
		err = errors.Errorf("SSH_AUTH_SOCK empty or unset")
		return
	}

	var connSshSock net.Conn

	ui.Log().Print("connecting to SSH_AUTH_SOCK: %s", socket)
	if connSshSock, err = net.Dial("unix", socket); err != nil {
		err = errors.Wrapf(err, "failed to connect to SSH_AUTH_SOCK")
		return
	}

	ctx.After(errors.MakeFuncContextFromFuncErr(connSshSock.Close))

	ui.Log().Print("creating ssh-agent client")
	sshAgent = agent.NewClient(connSshSock)

	return
}

func makeHostKeyCallback(
	knownHostsFile string,
) (ssh.HostKeyCallback, []string, error) {
	var files []string

	if knownHostsFile != "" {
		files = append(files, knownHostsFile)
	} else {
		if sshHome := os.Getenv("SSH_HOME"); sshHome != "" {
			sshHomeKnownHosts := filepath.Join(sshHome, "known_hosts")
			if _, err := os.Stat(sshHomeKnownHosts); err == nil {
				files = append(files, sshHomeKnownHosts)
			}
		}

		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to determine home directory for known_hosts")
		}

		userKnownHosts := filepath.Join(homeDir, ".ssh", "known_hosts")
		if _, err := os.Stat(userKnownHosts); err == nil {
			files = append(files, userKnownHosts)
		}

		systemKnownHosts := "/etc/ssh/ssh_known_hosts"
		if _, err := os.Stat(systemKnownHosts); err == nil {
			files = append(files, systemKnownHosts)
		}
	}

	if len(files) == 0 {
		return nil, nil, errors.Errorf(
			"no known_hosts files found; create ~/.ssh/known_hosts, set $SSH_HOME, or specify --known-hosts-file",
		)
	}

	callback, err := knownhosts.New(files...)
	if err != nil {
		return nil, files, errors.Wrapf(err, "failed to parse known_hosts files: %v", files)
	}

	return callback, files, nil
}

// hostKeyAlgorithmsForKnownHosts returns the host-key algorithm names that
// are recorded for `addr` in the loaded known_hosts files. The result is
// suitable for assignment to ssh.ClientConfig.HostKeyAlgorithms so the SSH
// negotiation prefers a key type we have a verified entry for. Returns an
// empty slice when no entries match the host (let Go's defaults apply).
//
// Background: golang.org/x/crypto/ssh/knownhosts does not expose a direct
// enumeration API. The standard workaround (golang/go#29286, also used by
// github.com/skeema/knownhosts) is to invoke the callback with a synthetic
// placeholder key and read the Want slice off the returned *KeyError. See
// issue #99.
func hostKeyAlgorithmsForKnownHosts(
	callback ssh.HostKeyCallback,
	addr string,
) []string {
	err := callback(addr, &net.TCPAddr{}, hostKeyAlgorithmsProbe{})
	if err == nil {
		return nil
	}

	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) {
		return nil
	}

	seen := map[string]struct{}{}
	var algos []string

	for _, k := range keyErr.Want {
		t := k.Key.Type()
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		algos = append(algos, t)
	}

	return algos
}

// hostKeyAlgorithmsProbe is a sentinel ssh.PublicKey used only to probe the
// known_hosts callback for its recorded keys. Its Type and Marshal cannot
// match any real recorded entry, so the callback always reports a mismatch
// — exposing the list of trusted keys via *knownhosts.KeyError.Want.
type hostKeyAlgorithmsProbe struct{}

func (hostKeyAlgorithmsProbe) Type() string    { return "madder-host-key-algorithms-probe" }
func (hostKeyAlgorithmsProbe) Marshal() []byte { return nil }
func (hostKeyAlgorithmsProbe) Verify([]byte, *ssh.Signature) error {
	return errors.Errorf("probe key cannot verify signatures")
}

// TODO refactor `blob_store_configs.ConfigSFTP` for ssh-client-specific methods
func MakeSSHClientForExplicitConfig(
	ctx interfaces.ActiveContext,
	uiPrinter ui.Printer,
	config blob_store_configs.ConfigSFTPConfigExplicit,
) (sshClient *ssh.Client, err error) {
	var hostKeyCallback ssh.HostKeyCallback
	var knownHostsFiles []string

	if hostKeyCallback, knownHostsFiles, err = makeHostKeyCallback(
		config.GetKnownHostsFile(),
	); err != nil {
		err = errors.Wrap(err)
		return
	}

	clientConfig := &ssh.ClientConfig{
		User:            config.GetUser(),
		HostKeyCallback: hostKeyCallback,
	}

	// Configure authentication
	if config.GetPrivateKeyPath() != "" {
		var key ssh.Signer
		var keyBytes []byte

		if keyBytes, err = os.ReadFile(config.GetPrivateKeyPath()); err != nil {
			err = errors.Wrapf(err, "failed to read private key")
			return sshClient, err
		}

		if key, err = ssh.ParsePrivateKey(keyBytes); err != nil {
			err = errors.Wrapf(err, "failed to parse private key")
			return sshClient, err
		}

		clientConfig.Auth = []ssh.AuthMethod{ssh.PublicKeys(key)}
	} else if config.GetPassword() != "" {
		clientConfig.Auth = []ssh.AuthMethod{ssh.Password(config.GetPassword())}
	} else {
		var clientAgent agent.ExtendedAgent

		if clientAgent, err = MakeSSHAgent(ctx, uiPrinter); err != nil {
			err = errors.Wrap(err)
			return
		}

		clientConfig.Auth = []ssh.AuthMethod{
			ssh.PublicKeysCallback(clientAgent.Signers),
		}
	}

	addr := fmt.Sprintf(
		"%s:%d",
		config.GetHost(),
		config.GetPort(),
	)

	clientConfig.HostKeyAlgorithms = hostKeyAlgorithmsForKnownHosts(
		hostKeyCallback,
		addr,
	)

	uiPrinter.Printf(
		"verifying host key for %q against known_hosts: [%s]; algorithms: [%s]",
		addr,
		strings.Join(knownHostsFiles, ", "),
		strings.Join(clientConfig.HostKeyAlgorithms, ", "),
	)

	if sshClient, err = sshDial(ctx, uiPrinter, clientConfig, addr); err != nil {
		err = errors.Wrap(err)
		return sshClient, err
	}

	return sshClient, err
}

func MakeSSHClientFromSSHConfig(
	ctx interfaces.ActiveContext,
	uiPrinter ui.Printer,
	config blob_store_configs.ConfigSFTPUri,
) (sshClient *ssh.Client, err error) {
	var clientAgent agent.ExtendedAgent

	if clientAgent, err = MakeSSHAgent(ctx, uiPrinter); err != nil {
		err = errors.Wrap(err)
		return
	}

	uri := config.GetUri()
	url := uri.GetUrl()

	// Resolve via `ssh -G <host>` so ssh_config Host blocks are
	// honored. Best-effort: empty resolution means we fall back to
	// the URI's literal values. URI-explicit fields always
	// override resolved values.
	resolved, _ := resolveSSHConfig(url.Hostname())

	hostname := url.Hostname()
	if resolved.Hostname != "" {
		hostname = resolved.Hostname
	}
	port := url.Port()
	if port == "" {
		port = resolved.Port
	}
	if port == "" {
		port = "22"
	}
	user := url.User.Username()
	if user == "" {
		user = resolved.User
	}

	knownHostsPath := config.GetKnownHostsFile()
	if knownHostsPath == "" {
		knownHostsPath = resolved.UserKnownHostsFile
	}

	var hostKeyCallback ssh.HostKeyCallback
	var knownHostsFiles []string

	if hostKeyCallback, knownHostsFiles, err = makeHostKeyCallback(
		knownHostsPath,
	); err != nil {
		err = errors.Wrap(err)
		return
	}

	clientConfig := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeysCallback(clientAgent.Signers),
		},
		HostKeyCallback: hostKeyCallback,
	}

	addr := fmt.Sprintf("%s:%s", hostname, port)

	clientConfig.HostKeyAlgorithms = hostKeyAlgorithmsForKnownHosts(
		hostKeyCallback,
		addr,
	)

	uiPrinter.Printf(
		"verifying host key for %q against known_hosts: [%s]; algorithms: [%s]",
		addr,
		strings.Join(knownHostsFiles, ", "),
		strings.Join(clientConfig.HostKeyAlgorithms, ", "),
	)

	if sshClient, err = sshDial(ctx, uiPrinter, clientConfig, addr); err != nil {
		err = errors.Wrap(err)
		return sshClient, err
	}

	return sshClient, err
}

func sshDial(
	ctx interfaces.ActiveContext,
	uiPrinter ui.Printer,
	configClient *ssh.ClientConfig,
	addr string,
) (sshClient *ssh.Client, err error) {
	if configClient.Timeout == 0 {
		configClient.Timeout = defaultSSHDialTimeout
	}

	uiPrinter.Printf("dialing %q...", addr)
	if sshClient, err = ssh.Dial("tcp", addr, configClient); err != nil {
		err = errors.Wrapf(err, "failed to connect to SSH server")
		return sshClient, err
	}
	uiPrinter.Printf("connected to %q...", addr)

	ctx.After(errors.MakeFuncContextFromFuncErr(sshClient.Close))

	return sshClient, err
}
