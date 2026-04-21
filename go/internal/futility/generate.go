package futility

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// GenerateAll writes all artifacts (manpages and shell completions) to
// standard paths under dir.
//
// Output layout:
//
//	{dir}/share/man/man1/{name}.1
//	{dir}/share/man/man1/{name}-{cmd}.1 (per visible command)
//	{dir}/share/bash-completion/completions/{name}
//	{dir}/share/zsh/site-functions/_{name}
//	{dir}/share/fish/vendor_completions.d/{name}.fish
func (u *Utility) GenerateAll(dir string) error {
	if err := u.GenerateManpages(dir); err != nil {
		return err
	}

	if err := u.InstallExtraManpages(dir); err != nil {
		return err
	}

	return u.GenerateCompletions(dir)
}

// InstallExtraManpages copies each ExtraManpages entry from its source fs.FS
// to {dir}/share/man/man{Section}/{Name}. The framework does not parse or
// modify the file contents — bytes are written verbatim.
func (u *Utility) InstallExtraManpages(dir string) error {
	for i, mf := range u.ExtraManpages {
		if mf.Source == nil {
			return fmt.Errorf("ExtraManpages[%d]: Source is nil", i)
		}
		if mf.Path == "" {
			return fmt.Errorf("ExtraManpages[%d]: Path is empty", i)
		}
		if mf.Section <= 0 {
			return fmt.Errorf("ExtraManpages[%d]: Section must be > 0", i)
		}
		if mf.Name == "" {
			return fmt.Errorf("ExtraManpages[%d]: Name is empty", i)
		}

		data, err := fs.ReadFile(mf.Source, mf.Path)
		if err != nil {
			return fmt.Errorf("ExtraManpages[%d]: reading %s: %w", i, mf.Path, err)
		}

		manDir := filepath.Join(dir, "share", "man", fmt.Sprintf("man%d", mf.Section))
		if err := os.MkdirAll(manDir, 0o755); err != nil {
			return fmt.Errorf("ExtraManpages[%d]: creating %s: %w", i, manDir, err)
		}

		dst := filepath.Join(manDir, mf.Name)
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("ExtraManpages[%d]: writing %s: %w", i, dst, err)
		}
	}
	return nil
}
