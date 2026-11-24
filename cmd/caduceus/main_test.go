package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
)

func cli(t *testing.T, x ext.Ext, args ...string) (int, string, string) {
	t.Helper()
	if x == nil {
		x = &exttest.Fake{T: t} // no external call expected
	}
	var out, errb bytes.Buffer
	code := run(args, x, &out, &errb)
	return code, out.String(), errb.String()
}

// home is a temporary home with the components the shipped profiles name in ~/Projects.
func home(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("CADUCEUS_CONFIG", "")
	for _, d := range []string{"hermes-dispatch", "hermes-relay", "endfield-dotfiles"} {
		if err := os.MkdirAll(filepath.Join(h, "Projects", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func TestHelpAndUnbuiltCommands(t *testing.T) {
	for _, c := range commands {
		t.Run(c.name, func(t *testing.T) {
			code, out, _ := cli(t, nil, c.name, "--help")
			if code != 0 || !strings.Contains(out, "usage: caduceus "+c.name) {
				t.Errorf("--help: %d %q", code, out)
			}
			if c.run != nil {
				return
			}
			code, _, errOut := cli(t, nil, c.name, "list", "x")
			if code != 2 || errOut != "caduceus "+c.name+": not built yet (see #3)\n" {
				t.Errorf("unbuilt: %d %q", code, errOut)
			}
		})
	}
	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, 2}, {[]string{"--help"}, 0}, {[]string{"help"}, 0}, {[]string{"nope"}, 2},
		{[]string{"component"}, 2}, {[]string{"component", "init", "--help"}, 0},
		{[]string{"component", "check", "-h"}, 0}, {[]string{"component", "init"}, 2},
		{[]string{"component", "init", "a", "b"}, 2}, {[]string{"component", "check", "a", "--from-profile", "p"}, 2},
		{[]string{"component", "init", "a", "--bogus"}, 2},
	} {
		if code, _, _ := cli(t, nil, tc.args...); code != tc.code {
			t.Errorf("%q: exit %d, want %d", tc.args, code, tc.code)
		}
	}
}

func TestInitFromEveryProfileThenCheck(t *testing.T) {
	h := home(t)
	profiles, err := filepath.Glob(filepath.Join("..", "..", "testdata", "profiles", "*.toml"))
	if err != nil || len(profiles) != 8 {
		t.Fatalf("profiles %v %v", profiles, err)
	}
	for _, p := range profiles {
		repo := strings.TrimSuffix(filepath.Base(p), ".toml")
		t.Run(repo, func(t *testing.T) {
			code, text, errOut := cli(t, nil, "component", "init", repo, "--from-profile", p)
			if code != 0 {
				t.Fatalf("init: %d %s", code, errOut)
			}
			if !strings.Contains(text, "# The profile's gates, all under test for now: split them by meaning") {
				t.Error("no split-by-meaning note")
			}
			f := filepath.Join(h, repo+".toml")
			if err := os.WriteFile(f, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			code, out, errOut := cli(t, nil, "component", "check", repo, "--file", f)
			if code != 0 || !strings.HasPrefix(out, repo+": kind unset; gates test ") || strings.Count(out, "\n") != 1 {
				t.Errorf("check: %d %q %q\n%s", code, out, errOut, text)
			}
		})
	}
}

func TestScaffoldThenCheck(t *testing.T) {
	h := home(t)
	code, text, _ := cli(t, nil, "component", "init", "relay")
	f := filepath.Join(h, "relay.toml")
	if err := os.WriteFile(f, []byte(text), 0o644); err != nil || code != 0 {
		t.Fatal(code, err)
	}
	code, out, errOut := cli(t, nil, "component", "check", "--file", f, "relay")
	if code != 0 || out != "relay: kind unset; gates none; review 0; allow 0, deny 0\n" {
		t.Errorf("%d %q %q", code, out, errOut)
	}
}

func TestCheckPrintsEveryError(t *testing.T) {
	h := home(t)
	f := filepath.Join(h, "bad.toml")
	if err := os.WriteFile(f, []byte("modle = 1\n[access]\nread_only = [\"missing\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := cli(t, nil, "component", "check", "relay", "--file", f)
	if code != 1 || out != "" || !strings.Contains(errOut, f+": unknown key modle\n") ||
		!strings.Contains(errOut, f+": access.read_only: "+filepath.Join(h, "Projects/missing")+" doesn't exist") {
		t.Errorf("%d %q %q", code, out, errOut)
	}
}

func TestCheckReadsOriginHEAD(t *testing.T) {
	h := home(t)
	fake := &exttest.Fake{T: t, Commands: map[string]func([]string) (string, error){
		"git": func([]string) (string, error) { return "kind = \"cli\"\n[gates]\nlint = [\"go vet ./...\"]\n", nil },
	}}
	code, out, errOut := cli(t, fake, "component", "check", "relay")
	if code != 0 || out != "relay: kind cli; gates lint 1; review 0; allow 0, deny 0\n" {
		t.Errorf("%d %q %q", code, out, errOut)
	}
	want := "git -C " + filepath.Join(h, "Projects/relay") + " show origin/HEAD:.caduceus/project.toml"
	if len(fake.Calls) != 1 || strings.Join(fake.Calls[0], " ") != want {
		t.Errorf("calls %q", fake.Calls)
	}
}
