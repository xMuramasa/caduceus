package dispatch

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/xMuramasa/caduceus/internal/ext"
)

// checkEnv has hermes-relay's checkout with its settings and issue 27's blocker closed.
func checkEnv(t *testing.T) *env {
	e := setup(t)
	e.repo(t, "hermes-relay", e.relaySettings(t, ""))
	e.f.blocked[0].(map[string]any)["state"] = "closed"
	return e
}

func TestCheckEligible(t *testing.T) {
	e := checkEnv(t)
	ok, out := e.check(t, relay+"#27", false)
	if !ok {
		t.Error("not eligible")
	}
	contains(t, out, "ok   open: open", "ok   labels: none blocking", "ok   blocked-by: all closed",
		"ok   not running: no active unit", "ok   settings: hermes-relay: kind ", "ok   slots: 0 of 3 in use",
		"ok   locks: none needed", "ok   owns: no Owns", "ok   quota: session 13%",
		"verdict: xMuramasa/hermes-relay#27 can be dispatched")
	want := []string{"docker", "inspect", "-f", `{{(index .NetworkSettings.Networks "proxy").IPAddress}}`, "horus-eye"}
	if !slices.ContainsFunc(e.f.x.Calls, func(c []string) bool { return reflect.DeepEqual(c, want) }) ||
		!slices.ContainsFunc(e.f.x.Calls, func(c []string) bool { return reflect.DeepEqual(c, []string{"GET", summary}) }) {
		t.Errorf("calls %q", e.f.x.Calls)
	}
	if e.f.wroteToGitHub() {
		t.Error("check wrote to GitHub")
	}
}

func TestCheckReportsEveryFailure(t *testing.T) {
	e := checkEnv(t)
	e.f.blocked = fixture(t, "gh", "gh-blocked-by.json").([]any)
	e.f.issue["labels"] = append(e.f.issue["labels"].([]any), map[string]any{"name": "needs-design"})
	e.f.x.JSON[summary] = fixture(t, "horus", "horus-summary-na.json")
	e.f.active[unit27] = true
	ok, out := e.check(t, relay+"#27", false)
	if ok {
		t.Error("eligible")
	}
	contains(t, out, "FAIL labels: needs-design",
		"FAIL blocked-by: open: https://github.com/xMuramasa/hermes-relay/issues/27",
		"FAIL not running: "+unit27+" is active",
		"FAIL quota: session n/a, weekly n/a: usage unknown (horus-eye rate-limited) in 4 readings over 10 min, not "+
			"launching blind; --force-quota overrides",
		"verdict: xMuramasa/hermes-relay#27 is not eligible (labels, blocked-by, not running, quota)")
	// The gap core 2 left: one line per retry while usage is unknown.
	if n := strings.Count(out, "usage unknown, retrying in 200 s ("); n != 3 || !strings.Contains(out, "(3/3)\n") {
		t.Errorf("%d retry lines:\n%s", n, out)
	}
}

func TestCheckQuotaOverLimitNamesResetAndForceOverrides(t *testing.T) {
	e := checkEnv(t)
	e.f.x.JSON[summary] = map[string]any{"session": "73%", "weekly": "81%"}
	ok, out := e.check(t, relay+"#27", false)
	re := regexp.MustCompile(`FAIL quota: session 73%, weekly 81%: session over its limit, resets \w{3} \d\d:\d\d, ` +
		`weekly over its limit, resets \w{3} \d\d:\d\d; --force-quota overrides`)
	if ok || !re.MatchString(out) {
		t.Errorf("%v\n%s", ok, out)
	}
	if ok, out = e.check(t, relay+"#27", true); !ok || !strings.Contains(out, "(--force-quota)") {
		t.Errorf("forced: %v\n%s", ok, out)
	}
}

func TestCheckQuotaUnreachableRefuses(t *testing.T) {
	e := checkEnv(t)
	e.f.x.Commands["docker"] = func(a []string) (string, error) {
		return "", &ext.Error{Args: a, Code: 1, Msg: "connection refused"}
	}
	ok, out := e.check(t, relay+"#27", false)
	if ok {
		t.Error("eligible")
	}
	contains(t, out, "FAIL quota: horus-eye unreachable (connection refused) in 4 readings over 10 min, not launching blind")
}

func TestCheckMissingSettingsAndBadRef(t *testing.T) {
	e := checkEnv(t)
	_, out := e.check(t, "xMuramasa/endfield-dotfiles#3", false)
	contains(t, out, "FAIL settings: endfield-dotfiles: no .caduceus/project.toml on origin/HEAD",
		"verdict: xMuramasa/endfield-dotfiles#3 is not eligible (settings)")
	for _, bad := range []string{"hermes-relay 27", "xMuramasa/hermes-relay", "a/b#x", "a/b#1 x"} {
		if _, err := ParseRef(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if r, err := ParseRef(" xMuramasa/hermes-relay#27 "); err != nil || r != (Ref{relay, "hermes-relay", 27}) {
		t.Errorf("%v %v", r, err)
	}
}

func TestCheckSlots(t *testing.T) {
	e := checkEnv(t)
	for _, n := range []int{1, 2} {
		e.activeRun(t, "xMuramasa/news-reader", n)
	}
	if ok, out := e.check(t, relay+"#27", false); !ok || !strings.Contains(out, "ok   slots: 2 of 3 in use") {
		t.Errorf("%v\n%s", ok, out)
	}
	e.activeRun(t, "xMuramasa/news-reader", 3)
	ok, out := e.check(t, relay+"#27", false)
	if ok {
		t.Error("eligible with every slot taken")
	}
	contains(t, out, "FAIL slots: 3 of 3 in use: run_news-reader-issue-1, run_news-reader-issue-2, "+
		"run_news-reader-issue-3", "is not eligible (slots)")
}

func TestCheckLocks(t *testing.T) {
	e := setup(t)
	e.f.blocked[0].(map[string]any)["state"] = "closed"
	e.repo(t, "hermes-relay", e.relaySettings(t, "locks = [\"mac-build\", \"gradle\"]\n"))
	e.repo(t, "trismegistus-mac", "locks = [\"mac-build\"]\n")
	e.repo(t, "news-reader", "")
	e.activeRun(t, "xMuramasa/news-reader", 11)
	if ok, out := e.check(t, relay+"#27", false); !ok || !strings.Contains(out, "ok   locks: mac-build, gradle free") {
		t.Errorf("%v\n%s", ok, out)
	}
	e.activeRun(t, "xMuramasa/trismegistus-mac", 52)
	ok, out := e.check(t, relay+"#27", false)
	if ok {
		t.Error("eligible with a held lock")
	}
	contains(t, out, "FAIL locks: mac-build held by run_trismegistus-mac-issue-52")
	e.activeRun(t, "xMuramasa/homelab", 7) // no checkout: its locks can't be known, so nothing launches
	_, out = e.check(t, relay+"#27", false)
	contains(t, out, "FAIL locks: can't read the locks of run_homelab-issue-7: homelab: no .caduceus/project.toml")
}

func TestCheckOwnsOverlap(t *testing.T) {
	e := checkEnv(t)
	e.f.issue["body"] = e.f.issue["body"].(string) + "\nOwns: `agents/`, `relay/internal/server/agents*.go` (tests).\n"
	e.activeRun(t, "xMuramasa/news-reader", 30) // another repo: its Owns don't matter
	e.f.others[30] = map[string]any{"number": 30, "body": "Owns: `agents/`"}
	e.activeRun(t, relay, 31)
	e.f.others[31] = map[string]any{"number": 31, "body": "Owns: `term/`, `relay/internal/client/`"}
	if ok, out := e.check(t, relay+"#27", false); !ok ||
		!strings.Contains(out, "ok   owns: `agents/`, `relay/internal/server/agents*.go`; no overlap with 1 active run(s) in "+relay) {
		t.Errorf("%v\n%s", ok, out)
	}
	e.activeRun(t, relay, 32)
	e.f.others[32] = map[string]any{"number": 32, "body": "Part of #3.\n\n**Owns:** `relay/internal/server/`\n"}
	ok, out := e.check(t, relay+"#27", false)
	if ok {
		t.Error("eligible with overlapping Owns")
	}
	contains(t, out, "FAIL owns: `relay/internal/server/agents*.go` overlaps `relay/internal/server/` of "+
		"run_hermes-relay-issue-32 (#32)")
}

func TestOwnsAndOverlap(t *testing.T) {
	if got := Owns("x\n- Owns: `internal/dispatch/`, `cmd/caduceus` (wiring only), `./testdata/`.\nOwns: `later`"); !reflect.DeepEqual(got,
		[]string{"internal/dispatch/", "cmd/caduceus", "testdata/"}) {
		t.Errorf("%q", got)
	}
	if got := Owns("no line\nWho owns: `x`"); got != nil {
		t.Errorf("%q", got)
	}
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"internal/dispatch/", "internal/dispatch/render.go", true},
		{"cmd/caduceus", "cmd/caduceus/main.go", true},
		{"cmd/caduceus", "cmd/caduceus2/main.go", false},
		{"go.mod", "go.sum", false},
		{"go.mod", "go.mod", true},
		{"relay/*.go", "relay/internal/x.go", true},
		{"relay/x*", "relay/y.go", false},
		{"**", "anything", true},
		{"docs/**/*.md", "internal/", false},
		{"internal/run", "internal/runs/", false},
		{"internal/run*", "internal/runs/", true},
	} {
		if got := Overlap(tc.a, tc.b); got != tc.want {
			t.Errorf("Overlap(%q, %q) = %v", tc.a, tc.b, got)
		}
		if got := Overlap(tc.b, tc.a); got != tc.want {
			t.Errorf("Overlap(%q, %q) = %v", tc.b, tc.a, got)
		}
	}
}
