package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xMuramasa/caduceus/internal/config"
)

func write(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CADUCEUS_CONFIG", "")
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string][2]string{
		"path":      {c.Path, filepath.Join(home, ".config/caduceus/config.toml")},
		"projects":  {c.Projects, filepath.Join(home, "Projects")},
		"runs":      {c.Runs, filepath.Join(home, ".local/state/caduceus/runs")},
		"checkout":  {c.Checkout("relay"), filepath.Join(home, "Projects/relay")},
		"worktrees": {c.Worktrees("relay"), filepath.Join(home, "Projects/relay-wt")},
	}
	for name, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s = %q, want %q", name, v[0], v[1])
		}
	}
	if c.Slots != 3 || c.Quota.Day != (config.Limits{Window: 70, Weekly: 80}) ||
		c.Quota.Night != (config.Limits{Window: 30, Weekly: 80}) {
		t.Errorf("slots/quota defaults: %+v %+v", c.Slots, c.Quota)
	}
	for _, key := range []string{"owner", "horus_url", "board.id", "board.status_field"} {
		err := c.Require(key)
		if want := "set " + key + " in " + c.Path; err == nil || err.Error() != want {
			t.Errorf("Require(%s) = %v, want %q", key, err, want)
		}
	}
}

func TestFileOverridesDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := write(t, `
projects = "/srv/src"
worktree_root = "/srv/wt/{repo}"
runs = "/srv/runs"
owner = "someone"
horus_url = "http://horus.invalid:8088"
slots = 2
[board]
id = "PVT_FAKE"
status_field = "PVTSSF_FAKE"
status = { Todo = "aaaa", Ready = "bbbb" }
[quota.night]
window = 25
`)
	t.Setenv("CADUCEUS_CONFIG", p)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != p || c.Projects != "/srv/src" || c.Worktrees("relay") != "/srv/wt/relay" || c.Runs != "/srv/runs" ||
		c.Slots != 2 || c.Board.Status["Ready"] != "bbbb" {
		t.Errorf("got %+v", c)
	}
	if c.Quota.Night != (config.Limits{Window: 25, Weekly: 80}) || c.Quota.Day.Window != 70 {
		t.Errorf("quota %+v", c.Quota)
	}
	if err := c.Require("owner", "horus_url", "board.id", "board.status_field"); err != nil {
		t.Error(err)
	}
}

func TestBadConfigs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tests := []struct{ text, want string }{
		{`projets = "/x"`, "unknown keys projets"},
		{"[board]\nstatus_feld = \"x\"", "unknown keys board.status_feld"},
		{`slots = "3"`, "slots"},
		{`slots = 0`, "slots must be at least 1"},
		{"[quota.day]\nwindow = 170", "quota.day limits"},
		{`worktree_root = "/wt"`, "worktree_root must contain {repo}"},
		{`x =`, "config.toml"},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			_, err := config.LoadFile(write(t, tc.text))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
