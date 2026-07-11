package commands

import "testing"

func TestAbbreviatePath(t *testing.T) {
	cases := []struct {
		name string
		home string
		path string
		want string
	}{
		{
			name: "home prefixed XDG path",
			home: "/home/sasha",
			path: "/home/sasha/.local/share/madder/config/blob_store.toml",
			want: "~/.l/s/m/c/blob_store.toml",
		},
		{
			name: "bare home",
			home: "/home/sasha",
			path: "/home/sasha",
			want: "~",
		},
		{
			name: "absolute path outside home",
			home: "/home/sasha",
			path: "/etc/xdg/madder/config.toml",
			want: "/e/x/m/config.toml",
		},
		{
			name: "cwd-relative path with parent traversal",
			home: "/home/sasha",
			path: "../other/config.toml",
			want: "../o/config.toml",
		},
		{
			name: "single component keeps whole leaf",
			home: "/home/sasha",
			path: "madder.toml",
			want: "madder.toml",
		},
		{
			name: "empty home skips tilde substitution",
			home: "",
			path: "/home/sasha/.local/share/madder/config.toml",
			want: "/h/s/.l/s/m/config.toml",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := abbreviatePath(c.home, c.path); got != c.want {
				t.Errorf("abbreviatePath(%q, %q) = %q, want %q", c.home, c.path, got, c.want)
			}
		})
	}
}
