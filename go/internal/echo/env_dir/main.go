package env_dir

//go:generate dagnabit export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/xdg"
)

type Env interface {
	interfaces.ActiveContextGetter
	interfaces.EnvVarsAdder

	IsDryRun() bool
	GetCwd() string

	GetXDG() xdg.XDG
	GetXDGForBlobStores() xdg.XDG
	GetXDGForBlobStoresWithoutOverride() xdg.XDG
	GetXDGForBlobStoresWithOverridePath(overridePath string) xdg.XDG
	GetXDGForBlobStoreId(scoped_id.Id) xdg.XDG

	GetExecPath() string
	GetTempLocal() TemporaryFS
	MakeDirs(dirs ...string) (err error)
	MakeDirsPerms(perms os.FileMode, dirs ...string) (err error)
	Rel(p string) (out string)
	RelToCwdOrSame(p string) (p1 string)
	MakeCommonEnv() map[string]string
	MakeRelativePathStringFormatWriter() interfaces.StringEncoderTo[string]
	AbsFromCwdOrSame(p string) (p1 string)

	// GetVerifyOnCollisionOverride returns true when the runtime env var
	// named by EnvVarNames.VerifyOnCollision (default
	// MADDER_VERIFY_ON_COLLISION) is set to a truthy value. It is OR'd
	// with the per-store config field by callers that publish blobs;
	// see issue #31 and ADR 0003 for rationale. See #38 for the eventual
	// migration from env var to CLI global flag.
	GetVerifyOnCollisionOverride() bool

	// GetBlobWriteObserver returns the observer wired at env-construction
	// time by the command layer (based on the --no-inventory-log global
	// flag and the MADDER_INVENTORY_LOG env var). Concrete blob stores fetch it
	// from here and plumb it into blob_io.MoveOptions. Nil means no
	// observer is attached — the mover then skips its call sites. See
	// ADR 0004.
	//
	// The corresponding setter lives on the concrete env type (not on
	// this interface) because it needs a pointer receiver to mutate
	// after construction; callers with the concrete value use
	// (&dir).SetBlobWriteObserver(...).
	GetBlobWriteObserver() domain_interfaces.BlobWriteObserver

	Delete(paths ...string) (err error)
}

type env struct {
	errors.Context
	beforeXDG

	// repoName, when non-empty, nests the blob-store XDG under
	// repos/<repoName>/ (madder#240). Set from Config.RepoName.
	repoName string

	TempLocal, TempOS TemporaryFS

	verifyOnCollisionOverride bool

	blobWriteObserver domain_interfaces.BlobWriteObserver

	// uiErrPrinter is the optional per-env sink for env_dir's own
	// chatter (the dry-run "would delete" notice, #232). Wired at
	// env-construction time like blobWriteObserver — the setter lives
	// on the concrete type because it needs a pointer receiver. Nil
	// means fall back to the process-global stderr printer.
	uiErrPrinter ui.Printer

	xdg.XDG
}

var _ Env = &env{}

// sets XDG and creates tmp local
func (env *env) initializeXDG() (err error) {
	env.TempLocal.BasePath = env.Cache.MakePath(
		fmt.Sprintf("tmp-%d", env.GetPid()),
	).String()

	if err = env.MakeDirs(env.GetTempLocal().BasePath); err != nil {
		err = errors.Wrap(err)
		return err
	}

	env.verifyOnCollisionOverride = parseBoolEnv(
		os.Getenv(env.envVarNames.VerifyOnCollision),
	)

	return err
}

// parseBoolEnv returns true if the env-var value is a truthy string
// ("1", "true", "yes", "on" — case-insensitive). Everything else,
// including empty, is false. The accepted set mirrors what other
// env-var driven toggles in the Go ecosystem typically accept.
func parseBoolEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (env env) GetVerifyOnCollisionOverride() bool {
	return env.verifyOnCollisionOverride
}

func (env env) GetBlobWriteObserver() domain_interfaces.BlobWriteObserver {
	return env.blobWriteObserver
}

func (env *env) SetBlobWriteObserver(observer domain_interfaces.BlobWriteObserver) {
	env.blobWriteObserver = observer
}

// SetUIErrPrinter wires the err sink env_dir's own chatter routes
// through (#232). Callers with the concrete value set it after
// construction — (&dir).SetUIErrPrinter(envUI.GetErr()) — mirroring
// SetBlobWriteObserver.
func (env *env) SetUIErrPrinter(printer ui.Printer) {
	env.uiErrPrinter = printer
}

// getUIErrPrinter resolves the chatter sink: the wired per-env
// printer when one was set, the process-global stderr printer
// otherwise (default behavior unchanged).
func (env env) getUIErrPrinter() ui.Printer {
	if env.uiErrPrinter != nil {
		return env.uiErrPrinter
	}

	return ui.Err()
}

func (env env) GetActiveContext() interfaces.ActiveContext {
	return env.Context
}

func (env env) GetDebug() debug.Options {
	return env.debugOptions
}

func (env env) IsDryRun() bool {
	return env.dryRun
}

func (env env) GetPid() int {
	return env.xdgInitArgs.Pid
}

func (env env) AddToEnvVars(envVars interfaces.EnvVars) {
	if env.envVarNames.Binary == "" {
		return
	}
	envVars[env.envVarNames.Binary] = env.GetExecPath()
}

func (env env) GetExecPath() string {
	return env.xdgInitArgs.ExecPath
}

func (env env) GetCwd() string {
	return env.XDG.Cwd.ActualValue
}

// GetXDG returns the env's metadata XDG, nested under repos/<repoName>/
// for a named FDR-0019 repo (madder#241) so the repo's metadata tree
// (data/config/state/cache/runtime) is isolated — matching the blob-store
// accessors below. No-op for the default/unnamed env.
//
// The nest is applied on read off the raw env.XDG field. The blob-store
// accessors derive from that same raw field via xdgForBlobStoresBase (not
// via GetXDG), so each re-applies the nest exactly once — reading the base
// metadata XDG through this method never double-nests the blob path.
func (env env) GetXDG() xdg.XDG {
	return env.nestForRepo(env.XDG)
}

// nestForRepo appends repos/<repoName> to an XDG's category dirs when this
// env addresses a named FDR-0019 repo, isolating both the repo's metadata
// tree (via GetXDG, madder#241) and its blob pool (via the blob-store
// accessors, madder#240). No-op for the default/unnamed env.
//
// At the blob-store boundaries it MUST be applied as the final step after
// the XDG re-derivation (CloneWithUtilityName / CloneWithoutOverride /
// CloneWithOverridePath), because those rebuild every category ActualValue
// from templates and would discard the suffix. That is why each blob-XDG
// accessor (and GetXDGForBlobStoreId) re-applies it after its own clone
// rather than relying on a single nested base. GetXDG applies it to the
// raw env.XDG field, which the blob base re-derives from — so the two
// paths nest independently and exactly once each.
//
// Direction: env_dir is slated to move upstream into dewey as a generic
// xdg utility base; at that point this repo-name nesting is a candidate
// to fold into dewey's XDG itself so clones preserve it natively (closing
// the "future call site forgets to re-nest" gap). See
// project_env_dir_upstream_to_dewey.
func (env env) nestForRepo(x xdg.XDG) xdg.XDG {
	if env.repoName == "" {
		return x
	}

	x.Data.ActualValue = filepath.Join(x.Data.ActualValue, "repos", env.repoName)
	x.Config.ActualValue = filepath.Join(x.Config.ActualValue, "repos", env.repoName)
	x.State.ActualValue = filepath.Join(x.State.ActualValue, "repos", env.repoName)
	x.Cache.ActualValue = filepath.Join(x.Cache.ActualValue, "repos", env.repoName)
	x.Runtime.ActualValue = filepath.Join(x.Runtime.ActualValue, "repos", env.repoName)

	return x
}

// xdgForBlobStoresBase is the un-nested blob-store XDG: a fresh derivation
// keyed by the utility name (preserving any cwd/ancestor override). Repo
// nesting is layered on at each public boundary via nestForRepo.
func (env env) xdgForBlobStoresBase() xdg.XDG {
	return env.XDG.CloneWithUtilityName(env.XDG.UtilityName)
}

func (env env) GetXDGForBlobStores() xdg.XDG {
	return env.nestForRepo(env.xdgForBlobStoresBase())
}

// GetXDGForBlobStoresWithoutOverride drops any cwd/ancestor override
// (resolving against $HOME) and re-applies repo nesting. Discovery uses
// this for non-cwd (XDG user) stores (madder#240).
func (env env) GetXDGForBlobStoresWithoutOverride() xdg.XDG {
	return env.nestForRepo(env.xdgForBlobStoresBase().CloneWithoutOverride())
}

// GetXDGForBlobStoresWithOverridePath roots the blob-store XDG at a
// specific ancestor/cwd path and re-applies repo nesting. Discovery (per
// ancestor) and init (cwd) use this (madder#240).
func (env env) GetXDGForBlobStoresWithOverridePath(overridePath string) xdg.XDG {
	return env.nestForRepo(
		env.xdgForBlobStoresBase().CloneWithOverridePath(overridePath),
	)
}

func (env env) GetXDGForBlobStoreId(id scoped_id.Id) xdg.XDG {
	base := env.xdgForBlobStoresBase()

	switch id.GetLocationType() {
	case scoped_id.LocationTypeXDGUser, scoped_id.LocationTypeXDGCache:
		return env.nestForRepo(base.CloneWithoutOverride())

	default:
		// Cwd (and other) ids keep the ancestor/cwd override.
		return env.nestForRepo(base)
	}
}

func (env *env) SetXDG(x xdg.XDG) {
	env.XDG = x
}

func (env env) GetTempLocal() TemporaryFS {
	return env.TempLocal
}

func (env env) AbsFromCwdOrSame(p string) (p1 string) {
	var err error
	p1, err = filepath.Abs(p)
	if err != nil {
		p1 = p
	}

	return p1
}

func (env env) RelToCwdOrSame(p string) (p1 string) {
	var err error

	if p1, err = filepath.Rel(env.GetCwd(), p); err != nil {
		p1 = p
	}

	return p1
}

func (env env) Rel(
	p string,
) (out string) {
	out = p

	p1, _ := filepath.Rel(env.GetCwd(), p)

	if p1 != "" {
		out = p1
	}

	return out
}

func (env env) MakeCommonEnv() map[string]string {
	if env.envVarNames.Binary == "" {
		return nil
	}
	return map[string]string{
		env.envVarNames.Binary: env.GetExecPath(),
	}
}

func (env env) MakeDirs(ds ...string) (err error) {
	return env.MakeDirsPerms(0o755, ds...)
}

func (env env) MakeDirsPerms(perms os.FileMode, ds ...string) (err error) {
	for _, d := range ds {
		if err = os.MkdirAll(d, os.ModeDir|perms); err != nil {
			err = errors.Wrapf(err, "Dir: %q", d)
			return err
		}
	}

	return err
}
