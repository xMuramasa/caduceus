package main

import (
	"strings"
	"testing"

	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
)

func TestDispatchUsage(t *testing.T) {
	home(t)
	for _, args := range [][]string{
		{"dispatch"}, {"dispatch", "go"}, {"dispatch", "check"}, {"dispatch", "check", "hermes-relay 27"},
		{"dispatch", "check", "a/b#1", "--model", "opus"}, {"dispatch", "start", "a/b#1", "--effort", "huge"},
		{"dispatch", "start", "a/b#1", "--out", "/tmp/x"}, {"dispatch", "start", "a/b#1", "c/d#2"},
	} {
		if code, _, _ := cli(t, nil, args...); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
	if code, out, _ := cli(t, nil, "dispatch", "start", "--help"); code != 0 || !strings.Contains(out, "--dry-run") {
		t.Errorf("%d %q", code, out)
	}
}

func TestDispatchReachesGitHub(t *testing.T) {
	home(t)
	fake := &exttest.Fake{T: t, Commands: map[string]func([]string) (string, error){
		"gh": func(a []string) (string, error) {
			return "", &ext.Error{Args: a, Code: 1, Msg: "gh issue view: exit 1: Could not resolve to an issue"}
		},
	}}
	code, out, _ := cli(t, fake, "dispatch", "start", "--dry-run", "xMuramasa/hermes-relay#27", "--model", "sonnet")
	if code != 1 || out != "[1/8] issue: FAIL gh issue view: exit 1: Could not resolve to an issue\n" {
		t.Errorf("%d %q", code, out)
	}
	code, _, errOut := cli(t, fake, "dispatch", "check", "xMuramasa/hermes-relay#27", "--force-quota")
	if code != 1 || errOut != "caduceus dispatch check: gh issue view: exit 1: Could not resolve to an issue\n" {
		t.Errorf("%d %q", code, errOut)
	}
}
