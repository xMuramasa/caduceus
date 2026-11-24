package runs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestName(t *testing.T) {
	for _, tc := range []struct {
		repo string
		n    int
		want string
	}{
		{"news-reader", 11, "news-reader-issue-11"},
		{"-dash", 1, ""},
		{"a b", 1, ""},
		{strings.Repeat("x", 60), 1, ""},
	} {
		got, err := Name(tc.repo, tc.n)
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Errorf("Name(%q, %d) = %q, %v", tc.repo, tc.n, got, err)
		}
	}
	if Unit("news-reader-issue-11") != "caduceus-news-reader-issue-11.service" {
		t.Error(Unit("news-reader-issue-11"))
	}
}

func TestArchiveNumbersAttemptsAndDropsTheUnit(t *testing.T) {
	dir := t.TempDir()
	name := "news-reader-issue-11"
	for i := 1; i <= 2; i++ {
		write(t, filepath.Join(dir, name+".run.json"),
			`{"id": "run_`+name+`", "unit": "caduceus-`+name+`.service", "extra": 1}`)
		write(t, filepath.Join(dir, name+".stream.jsonl"), "{}\n")
		got, err := Archive(dir, name)
		want := name + "-attempt" + string(rune('0'+i))
		if err != nil || got != want {
			t.Fatalf("Archive #%d = %q, %v", i, got, err)
		}
		var d map[string]any
		b, _ := os.ReadFile(filepath.Join(dir, want+".run.json"))
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatal(err)
		}
		if d["id"] != "run_"+want || d["unit"] != nil || d["extra"] != 1.0 {
			t.Errorf("archived run.json %v", d)
		}
		if len(Files(dir, name)) != 0 || len(Files(dir, want)) != 2 {
			t.Errorf("files after archive: %v %v", Files(dir, name), Files(dir, want))
		}
	}
	if got, err := Archive(dir, name); got != "" || err != nil {
		t.Errorf("nothing to archive: %q %v", got, err)
	}
	write(t, filepath.Join(dir, name+".run.json"), "{bad")
	if got, _ := Archive(dir, name); got != name+"-attempt3" {
		t.Errorf("malformed run.json: %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, name+"-attempt3.run.json")); string(b) != "{bad" {
		t.Errorf("malformed run.json rewritten: %q", b)
	}
}

func TestLoadAllAndFind(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "b.run.json"), `{"id": "run_b", "started_at": "2026-11-01T00:00:00+00:00"}`)
	write(t, filepath.Join(dir, "a.run.json"), `{"id": "run_a", "started_at": "2026-11-02T00:00:00+00:00"}`)
	write(t, filepath.Join(dir, "bad.run.json"), `[1]`)
	var names []string
	for _, r := range LoadAll(dir) {
		names = append(names, r.Name)
	}
	if !reflect.DeepEqual(names, []string{"b", "a"}) {
		t.Errorf("LoadAll order %v", names)
	}
	for _, ref := range []string{"a", "run_a"} {
		if r, ok := Find(dir, ref); !ok || r.Name != "a" {
			t.Errorf("Find(%q) = %v %v", ref, r, ok)
		}
	}
	if _, ok := Find(dir, "nope"); ok {
		t.Error("found nope")
	}
	if LoadAll(filepath.Join(dir, "missing")) != nil {
		t.Error("missing folder has runs")
	}
}

func TestStream(t *testing.T) {
	dir := t.TempDir()
	stream := Stream(dir, "r")
	tool := `{"type": "assistant", "message": {"content": [{"type": "text", "text": "x"}, {"type": "tool_use"}]}}`
	write(t, stream, tool+"\n"+tool+"\n"+`{"type": "result", "subtype": "error_max_turns", "is_error": true}`+"\n"+
		`"a string"`+"\n"+`{"trunc`)
	if n := ToolCalls(stream); n != 2 {
		t.Errorf("ToolCalls = %d", n)
	}
	if o := Outcome(Result(stream)); o != "error_max_turns (error)" {
		t.Errorf("Outcome = %q", o)
	}
	missing := Stream(dir, "missing")
	if ToolCalls(missing) != 0 || Result(missing) != nil || Outcome(nil) != "no result" {
		t.Error("missing stream")
	}
	if o := Outcome(map[string]any{"type": "result"}); o != "result" {
		t.Errorf("Outcome without subtype = %q", o)
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	for started, want := range map[string]string{
		"2026-11-20T11:00:00+00:00": "3d", "2026-11-23T07:00:00Z": "5h", "2026-11-23T11:48:00+00:00": "12m",
		"2026-11-23T11:59:20+00:00": "40s", "2026-11-23T13:00:00+00:00": "0s", "": "?", "yesterday": "?",
	} {
		if got := Age(started, now); got != want {
			t.Errorf("Age(%q) = %q, want %q", started, got, want)
		}
	}
}

func TestPrunable(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	active := map[string]bool{"caduceus-live.service": true}
	x := &exttest.Fake{T: t, Commands: map[string]func([]string) (string, error){
		"systemctl": func(a []string) (string, error) {
			if active[a[2]] {
				return "active\n", nil
			}
			return "inactive\n", &ext.Error{Args: a, Code: 3, Msg: "systemctl --user is-active: exit 3: inactive"}
		},
	}}
	for name, days := range map[string]int{"old": 31, "new": 2, "live": 40} {
		p := filepath.Join(dir, name+".run.json")
		write(t, p, "{}")
		at := now.Add(-time.Duration(days) * 24 * time.Hour)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		r    Run
		want bool
	}{
		{Run{Name: "old", Unit: "caduceus-old.service"}, true},
		{Run{Name: "new"}, false},
		{Run{Name: "live", Unit: "caduceus-live.service"}, false},
		{Run{Name: "gone"}, false},
	} {
		if got := Prunable(x, dir, tc.r, now); got != tc.want {
			t.Errorf("Prunable(%s) = %v", tc.r.Name, got)
		}
	}
}
