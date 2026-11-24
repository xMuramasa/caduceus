package main

import (
	"strings"
	"testing"

	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
)

func TestPRUsage(t *testing.T) {
	home(t)
	for _, args := range [][]string{
		{"pr"}, {"pr", "close"}, {"pr", "open", "a/b#1"}, {"pr", "open", "a/b#1", "c/d#2", "--review", "r.md"},
		{"pr", "merge"}, {"pr", "merge", "a/b#1", "--review", "r.md"}, {"pr", "merge", "a/b#1", "b 2"},
	} {
		if code, _, _ := cli(t, nil, args...); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
	if code, out, _ := cli(t, nil, "pr", "merge", "--help"); code != 0 || !strings.Contains(out, "priority order") {
		t.Errorf("%d %q", code, out)
	}
}

func TestPRMergeReachesGitHub(t *testing.T) {
	home(t)
	fake := &exttest.Fake{T: t, Commands: map[string]func([]string) (string, error){
		"gh": func(a []string) (string, error) {
			return "", &ext.Error{Args: a, Code: 4, Msg: "gh api user: exit 4: To get started with GitHub CLI, please run:  gh auth login"}
		},
	}}
	code, _, errOut := cli(t, fake, "pr", "merge", "xMuramasa/hermes-relay#27", "xMuramasa/hermes-relay#28")
	if code != 1 || !strings.HasPrefix(errOut, "caduceus pr merge: gh api user: exit 4") {
		t.Errorf("%d %q", code, errOut)
	}
}
