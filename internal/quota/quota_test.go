package quota

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
)

const (
	base     = "http://172.18.0.9:8088"
	summary  = base + "/api/summary"
	usageURL = base + "/api/usage"
)

var day = config.Limits{Window: 70, Weekly: 80}

func fixture(t *testing.T, name string) any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "horus", name))
	var v any
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// checker answers docker inspect with the container's address and serves the summary and usage given; its sleeps
// are recorded, and onSleep can change the answers between readings.
func checker(t *testing.T, sum, use any) (*Checker, *exttest.Fake, *[]time.Duration) {
	f := &exttest.Fake{T: t, JSON: map[string]any{summary: sum, usageURL: use},
		Commands: map[string]func([]string) (string, error){
			"docker": func([]string) (string, error) { return "172.18.0.9\n", nil },
		}}
	var slept []time.Duration
	c := &Checker{X: f, URL: "docker://horus-eye:8088", Sleep: func(d time.Duration) { slept = append(slept, d) }}
	return c, f, &slept
}

func TestOK(t *testing.T) {
	c, f, slept := checker(t, fixture(t, "horus-summary-ok.json"), nil)
	ok, detail := c.Check(day, false)
	if !ok || detail != "session 13%, weekly 25% (limits 70% / 80%)" {
		t.Errorf("%v %q", ok, detail)
	}
	want := [][]string{
		{"docker", "inspect", "-f", `{{(index .NetworkSettings.Networks "proxy").IPAddress}}`, "horus-eye"},
		{"GET", summary},
	}
	if !reflect.DeepEqual(f.Calls, want) || len(*slept) != 0 {
		t.Errorf("calls %q, slept %v", f.Calls, *slept)
	}
}

func TestBaseURL(t *testing.T) {
	c, f, _ := checker(t, nil, nil)
	f.Commands["docker"] = func(a []string) (string, error) {
		if a[2] != `{{(index .NetworkSettings.Networks "lab").IPAddress}}` {
			return "", nil
		}
		return "10.0.0.5\n", nil
	}
	for _, tc := range []struct{ url, want, err string }{
		{"http://horus.example.invalid:8088/", "http://horus.example.invalid:8088", ""},
		{"docker://horus-eye:9000?network=lab", "http://10.0.0.5:9000", ""},
		{"docker://horus-eye:9000", "", "horus-eye has no address on the proxy network"},
		{"docker://horus-eye", "", "want http(s)://host[:port] or docker://"},
		{"ftp://horus", "", "want http(s)://"},
	} {
		c.URL = tc.url
		got, err := c.base()
		if got != tc.want || (err == nil) != (tc.err == "") || (err != nil && !strings.Contains(err.Error(), tc.err)) {
			t.Errorf("%s: %q %v", tc.url, got, err)
		}
	}
}

func TestOverLimitNamesResetsAndForceOverrides(t *testing.T) {
	sum := map[string]any{"session": "73%", "weekly": "81%"}
	c, _, _ := checker(t, sum, fixture(t, "horus-usage-live-ok.json"))
	session := time.Date(2026, 11, 2, 19, 50, 0, 0, time.UTC).Local().Format("Mon 15:04")
	weekly := time.Date(2026, 11, 8, 14, 0, 0, 0, time.UTC).Local().Format("Mon 15:04")
	ok, detail := c.Check(day, false)
	want := "session 73%, weekly 81%: session over its limit, resets " + session + ", weekly over its limit, resets " +
		weekly + "; --force-quota overrides"
	if ok || detail != want {
		t.Errorf("%v %q\nwant %q", ok, detail, want)
	}
	ok, detail = c.Check(day, true)
	if !ok || !strings.HasSuffix(detail, " (--force-quota)") {
		t.Errorf("forced: %v %q", ok, detail)
	}
}

func TestLimits(t *testing.T) {
	for _, tc := range []struct {
		sum    map[string]any
		lim    config.Limits
		use    string
		ok     bool
		detail string
	}{
		{map[string]any{"session": "35%", "weekly": "10%"}, day, "", true, "session 35%, weekly 10% (limits 70% / 80%)"},
		{map[string]any{"session": "35%", "weekly": "10%"}, config.Limits{Window: 30, Weekly: 80}, "horus-usage-live-429.json",
			false, "session 35%, weekly 10%: session over its limit; --force-quota overrides"},
		{map[string]any{"session": "70%", "weekly": "10%"}, day, "horus-usage-live-429.json", false,
			"session 70%, weekly 10%: session over its limit; --force-quota overrides"},
	} {
		var use any
		if tc.use != "" {
			use = fixture(t, tc.use)
		}
		c, _, _ := checker(t, tc.sum, use)
		if ok, detail := c.Check(tc.lim, false); ok != tc.ok || detail != tc.detail {
			t.Errorf("%v %v: %v %q", tc.sum, tc.lim, ok, detail)
		}
	}
}

func TestNAIsRetriedThenRefused(t *testing.T) {
	c, f, slept := checker(t, fixture(t, "horus-summary-na.json"), nil)
	ok, detail := c.Check(day, false)
	want := "session n/a, weekly n/a: usage unknown (horus-eye rate-limited) in 4 readings over 10 min, " +
		"not launching blind; --force-quota overrides"
	if ok || detail != want {
		t.Errorf("%v %q", ok, detail)
	}
	if !reflect.DeepEqual(*slept, []time.Duration{Wait, Wait, Wait}) || len(f.Calls) != 8 {
		t.Errorf("slept %v, calls %q", *slept, f.Calls)
	}

	c, _, slept = checker(t, fixture(t, "horus-summary-na.json"), nil)
	ok, detail = c.Check(day, true)
	if !ok || len(*slept) != 0 || detail != "session n/a, weekly n/a: usage unknown (horus-eye rate-limited), "+
		"not launching blind (--force-quota)" {
		t.Errorf("forced: %v %q %v", ok, detail, *slept)
	}
}

func TestNAThenAnAnswer(t *testing.T) {
	c, f, slept := checker(t, fixture(t, "horus-summary-na.json"), nil)
	c.Sleep = func(d time.Duration) {
		*slept = append(*slept, d)
		if len(*slept) == 2 {
			f.JSON[summary] = fixture(t, "horus-summary-ok.json")
		}
	}
	if ok, detail := c.Check(day, false); !ok || detail != "session 13%, weekly 25% (limits 70% / 80%)" {
		t.Errorf("%v %q", ok, detail)
	}
	if len(*slept) != 2 {
		t.Errorf("slept %v", *slept)
	}
}

type down struct{ *exttest.Fake }

func (d down) GetJSON(url string, v any) error {
	d.Calls = append(d.Calls, []string{"GET", url})
	return errors.New("connection refused")
}

func TestUnreachableIsRetriedThenRefused(t *testing.T) {
	c, f, slept := checker(t, nil, nil)
	c.X = down{f}
	ok, detail := c.Check(day, false)
	want := "horus-eye unreachable (connection refused) in 4 readings over 10 min, not launching blind; " +
		"--force-quota overrides"
	if ok || detail != want || len(*slept) != 3 {
		t.Errorf("%v %q %v", ok, detail, *slept)
	}
}

func TestPercent(t *testing.T) {
	for v, want := range map[any]int{"43%": 43, " 0% ": 0, 7.0: 7, "n/a": -1, "": -1, nil: -1, "-3%": -1, "4.5%": -1} {
		n, ok := Percent(v)
		if (want < 0 && ok) || (want >= 0 && (!ok || n != want)) {
			t.Errorf("Percent(%v) = %d %v", v, n, ok)
		}
	}
}

func TestNew(t *testing.T) {
	cfg := &config.Config{Path: "/cfg.toml"}
	if _, err := New(nil, cfg); err == nil || err.Error() != "set horus_url in /cfg.toml" {
		t.Errorf("no horus_url: %v", err)
	}
	cfg.HorusURL = "horus-eye:8088"
	if _, err := New(nil, cfg); err == nil || !strings.HasPrefix(err.Error(), "/cfg.toml: horus_url") {
		t.Errorf("bad horus_url: %v", err)
	}
	cfg.HorusURL = "docker://horus-eye:8088"
	if c, err := New(nil, cfg); err != nil || c.URL != cfg.HorusURL || c.Sleep == nil {
		t.Errorf("%v %v", c, err)
	}
}
