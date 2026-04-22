package env_dir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/debug"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/xdg"
)

const (
	EnvDir               = "DIR_DODDER" // TODO chang to dodder-prefixed
	EnvBin               = "BIN_DODDER" // TODO change to dodder-prefixed
	XDGUtilityNameDodder = "dodder"
)

type Env interface {
	interfaces.ActiveContextGetter
	interfaces.EnvVarsAdder

	IsDryRun() bool
	GetCwd() string

	GetXDG() xdg.XDG
	GetXDGForBlobStores() xdg.XDG
	GetXDGForBlobStoreId(blob_store_id.Id) xdg.XDG

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
	// MADDER_VERIFY_ON_COLLISION is set to a truthy value. It is OR'd
	// with the per-store config field by callers that publish blobs;
	// see issue #31 and ADR 0003 for rationale. See #38 for the eventual
	// migration from env var to CLI global flag.
	GetVerifyOnCollisionOverride() bool

	Delete(paths ...string) (err error)
}

type env struct {
	errors.Context
	beforeXDG

	TempLocal, TempOS TemporaryFS

	verifyOnCollisionOverride bool

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
		os.Getenv("MADDER_VERIFY_ON_COLLISION"),
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
	envVars[EnvBin] = env.GetExecPath()
}

func (env env) GetExecPath() string {
	return env.xdgInitArgs.ExecPath
}

func (env env) GetCwd() string {
	return env.XDG.Cwd.ActualValue
}

func (env env) GetXDG() xdg.XDG {
	return env.XDG
}

func (env env) GetXDGForBlobStores() xdg.XDG {
	return env.XDG.CloneWithUtilityName(env.XDG.UtilityName)
}

func (env env) GetXDGForBlobStoreId(id blob_store_id.Id) xdg.XDG {
	xdg := env.GetXDGForBlobStores()

	switch id.GetLocationType() {
	case blob_store_id.LocationTypeXDGUser:
		return xdg.CloneWithoutOverride()

	case blob_store_id.LocationTypeXDGCache:
		return xdg.CloneWithoutOverride()

	case blob_store_id.LocationTypeCwd:
		return xdg

	default:
		return xdg
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
	return map[string]string{
		EnvBin: env.GetExecPath(),
		// TODO determine if EnvDir is kept
		// EnvDir: h.Dir(),
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
