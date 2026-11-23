// Package runs is the flat runs folder (SPEC section 5): names, attempts, unit state, stream reading, pruning.
package runs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xMuramasa/caduceus/internal/ext"
)

// PruneDays is how long a finished run's files stay.
const PruneDays = 30

var idRE = regexp.MustCompile(`^run_[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`) // the relay's agentIDRe

// Suffixes are a run's files: <name><suffix> in the runs folder.
var Suffixes = []string{".run.json", "-brief.md", "-rules.md", "-launch.sh", ".stream.jsonl", ".stderr",
	"-progress.md", "-report.md"}

// Run is a <name>.run.json; Name is the file stem.
type Run struct {
	ID        string `json:"id"`
	Repo      string `json:"repo"`
	Issue     int    `json:"issue"`
	Title     string `json:"title"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	Worktree  string `json:"worktree"`
	Branch    string `json:"branch"`
	StartedAt string `json:"started_at"`
	Unit      string `json:"unit"` // "" (null in the file) for an archived attempt
	Name      string `json:"-"`
}

// Name is the run name <repo>-issue-<n>; it fails when run_<name> is outside the relay's id pattern.
func Name(repo string, n int) (string, error) {
	name := fmt.Sprintf("%s-issue-%d", repo, n)
	if !idRE.MatchString("run_" + name) {
		return "", fmt.Errorf("run id run_%s is outside the relay's agentIDRe", name)
	}
	return name, nil
}

// Unit is the transient user service a run's claude runs in.
func Unit(name string) string { return "caduceus-" + name + ".service" }

// IsActive asks systemctl --user whether unit is active; an empty unit never is.
func IsActive(x ext.Ext, unit string) bool {
	if unit == "" {
		return false
	}
	out, _ := x.Run([]string{"systemctl", "--user", "is-active", unit}) // inactive exits 3: the answer, not a failure
	return strings.TrimSpace(out) == "active"
}

// Files lists the run's files that exist, in Suffixes order.
func Files(dir, name string) []string {
	var out []string
	for _, s := range Suffixes {
		p := filepath.Join(dir, name+s)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// Archive renames an earlier run's files to <name>-attemptK.* (K = next free) and returns the new name, or "" when
// there was nothing to archive. The archived run.json gets the new id and no unit, so nothing mistakes the next
// attempt's live unit for this one.
func Archive(dir, name string) (string, error) {
	old := Files(dir, name)
	if len(old) == 0 {
		return "", nil
	}
	taken, _ := filepath.Glob(filepath.Join(dir, name+"-attempt*.run.json"))
	k := 0
	for _, p := range taken {
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), name+"-attempt"), ".run.json"))
		if err == nil && n > k {
			k = n
		}
	}
	nu := fmt.Sprintf("%s-attempt%d", name, k+1)
	for _, p := range old {
		if err := os.Rename(p, filepath.Join(dir, nu+filepath.Base(p)[len(name):])); err != nil {
			return "", fmt.Errorf("archive %s: %w", name, err)
		}
	}
	meta := filepath.Join(dir, nu+".run.json")
	b, err := os.ReadFile(meta)
	if err != nil {
		return nu, nil // no run.json among the files
	}
	var d map[string]any
	if json.Unmarshal(b, &d) != nil || d == nil {
		return nu, nil // a malformed run.json is still archived as-is
	}
	d["id"], d["unit"] = "run_"+nu, nil
	b, _ = json.MarshalIndent(d, "", "  ")
	if err := os.WriteFile(meta, append(b, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("archive %s: %w", name, err)
	}
	return nu, nil
}

// LoadAll reads every run with a readable run.json, oldest first.
func LoadAll(dir string) []Run {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.run.json"))
	var out []Run
	for _, p := range paths {
		b, err := os.ReadFile(p)
		var r Run
		if err != nil || json.Unmarshal(b, &r) != nil {
			continue
		}
		r.Name = strings.TrimSuffix(filepath.Base(p), ".run.json")
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out
}

// Find looks a run up by name or id.
func Find(dir, ref string) (*Run, bool) {
	name := strings.TrimPrefix(ref, "run_")
	for _, r := range LoadAll(dir) {
		if r.Name == name {
			return &r, true
		}
	}
	return nil, false
}

// Stream is the run's stream-json file.
func Stream(dir, name string) string { return filepath.Join(dir, name+".stream.jsonl") }

// events calls fn for each JSON object line; a missing file has none.
func events(stream string, fn func(e map[string]any)) {
	f, err := os.Open(stream)
	if err != nil {
		return
	}
	defer f.Close()
	r := bufio.NewReader(f) // not a Scanner: one line can be megabytes
	for {
		line, err := r.ReadBytes('\n')
		var e map[string]any
		if json.Unmarshal(line, &e) == nil && e != nil { // skips a half-written last line while the run is going
			fn(e)
		}
		if err != nil {
			return
		}
	}
}

// Result is the stream's last "result" event (claude writes one when the run ends), or nil.
func Result(stream string) map[string]any {
	var last map[string]any
	events(stream, func(e map[string]any) {
		if e["type"] == "result" {
			last = e
		}
	})
	return last
}

// ToolCalls counts the tool_use blocks in the stream's assistant messages.
func ToolCalls(stream string) int {
	n := 0
	events(stream, func(e map[string]any) {
		if e["type"] != "assistant" {
			return
		}
		msg, _ := e["message"].(map[string]any)
		content, _ := msg["content"].([]any)
		for _, c := range content {
			if c, ok := c.(map[string]any); ok && c["type"] == "tool_use" {
				n++
			}
		}
	})
	return n
}

// Outcome is the result's subtype ("success", "error_max_turns", ...), "(error)" appended when is_error.
func Outcome(res map[string]any) string {
	if res == nil {
		return "no result"
	}
	s, _ := res["subtype"].(string)
	if s == "" {
		s = "result"
	}
	if res["is_error"] == true {
		s += " (error)"
	}
	return s
}

// Describe is the status result line: outcome, turns, minutes, cost and the result text's first line.
func Describe(res map[string]any, alive bool) string {
	if res == nil {
		if alive {
			return "none yet"
		}
		return "none (ended without a result line)"
	}
	bits := []string{Outcome(res)}
	if v, ok := res["num_turns"]; ok {
		bits = append(bits, fmt.Sprintf("%v turns", v))
	}
	if v, ok := res["duration_ms"].(float64); ok {
		bits = append(bits, fmt.Sprintf("%.0f min", v/60000))
	}
	if v, ok := res["total_cost_usd"].(float64); ok {
		bits = append(bits, fmt.Sprintf("$%.2f", v))
	}
	text, _ := res["result"].(string)
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if r := []rune(line); len(r) > 200 {
		line = string(r[:200])
	}
	if line != "" {
		line = ": " + line
	}
	return strings.Join(bits, ", ") + line
}

// Age is how long ago started was ("3d", "5h", "12m", "40s"), or "?" when it doesn't parse.
func Age(started string, now time.Time) string {
	t, err := time.Parse(time.RFC3339Nano, started)
	if err != nil {
		return "?"
	}
	d := now.Sub(t)
	for _, u := range []struct {
		s    string
		size time.Duration
	}{{"d", 24 * time.Hour}, {"h", time.Hour}, {"m", time.Minute}} {
		if d >= u.size {
			return fmt.Sprintf("%d%s", d/u.size, u.s)
		}
	}
	return fmt.Sprintf("%ds", max(d, 0)/time.Second)
}

// Prunable: finished and untouched for PruneDays (newest file mtime, which is about when it ended).
func Prunable(x ext.Ext, dir string, r Run, now time.Time) bool {
	fs := Files(dir, r.Name)
	var newest time.Time
	for _, p := range fs {
		if st, err := os.Stat(p); err == nil && st.ModTime().After(newest) {
			newest = st.ModTime()
		}
	}
	return len(fs) > 0 && now.Sub(newest) > PruneDays*24*time.Hour && !IsActive(x, r.Unit)
}
