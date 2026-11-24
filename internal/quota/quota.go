// Package quota reads the shared Claude limit from horus-eye's /api/summary (and /api/usage for reset times) and
// refuses a launch when usage is over the limits or unknown (SPEC section 11).
package quota

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/ext"
)

// Retries and Wait: an unknown reading is retried 3 times over about 10 minutes before refusing.
const (
	Retries = 3
	Wait    = 200 * time.Second
)

// Checker reads horus-eye at URL: http(s)://host:port, or docker://<container>:<port>[?network=<net>] (network
// defaults to proxy), resolved through docker on every reading because the container's address changes when it
// restarts.
type Checker struct {
	X     ext.Ext
	URL   string
	Sleep func(time.Duration)
}

// New is a Checker for the config's horus_url, sleeping for real.
func New(x ext.Ext, cfg *config.Config) (*Checker, error) {
	if err := cfg.Require("horus_url"); err != nil {
		return nil, err
	}
	if _, err := parse(cfg.HorusURL); err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.Path, err)
	}
	return &Checker{X: x, URL: cfg.HorusURL, Sleep: time.Sleep}, nil
}

func parse(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err == nil && (u.Hostname() == "" || (u.Scheme == "docker" && u.Port() == "") ||
		(u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "docker")) {
		err = fmt.Errorf("want http(s)://host[:port] or docker://<container>:<port>[?network=<net>]")
	}
	if err != nil {
		return nil, fmt.Errorf("horus_url %q: %w", raw, err)
	}
	return u, nil
}

// base is horus-eye's http address without a trailing slash.
func (c *Checker) base() (string, error) {
	u, err := parse(c.URL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "docker" {
		return strings.TrimRight(c.URL, "/"), nil
	}
	network := u.Query().Get("network")
	if network == "" {
		network = "proxy"
	}
	inspect := fmt.Sprintf(`{{(index .NetworkSettings.Networks %q).IPAddress}}`, network)
	out, err := c.X.Run([]string{"docker", "inspect", "-f", inspect, u.Hostname()})
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(out)
	if ip == "" {
		return "", fmt.Errorf("%s has no address on the %s network", u.Hostname(), network)
	}
	return "http://" + net.JoinHostPort(ip, u.Port()), nil
}

// Percent reads "43%" as 43; "n/a" (horus-eye's upstream answered 429) or anything else is not a reading.
func Percent(v any) (int, bool) {
	s := strings.TrimSuffix(strings.TrimSpace(fmt.Sprint(v)), "%")
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

type usage struct {
	Live struct {
		Limits []struct {
			Kind     string `json:"kind"`
			ResetsAt string `json:"resets_at"`
		} `json:"limits"`
	} `json:"live"`
}

// resets maps "session" and "weekly" to when they reset ("Mon 19:50", local time), from /api/usage's live limits.
func (u usage) resets() map[string]string {
	names := map[string]string{"session": "session", "weekly_all": "weekly"}
	out := map[string]string{}
	for _, l := range u.Live.Limits {
		t, err := time.Parse(time.RFC3339Nano, l.ResetsAt)
		if names[l.Kind] != "" && err == nil {
			out[names[l.Kind]] = t.Local().Format("Mon 15:04")
		}
	}
	return out
}

// Check reports whether a run may launch under lim, with a one-line detail. Unknown usage refuses like an exceeded
// limit (never launch blind), after Retries more readings Wait apart; force passes anyway, without waiting.
func (c *Checker) Check(lim config.Limits, force bool) (bool, string) {
	forced := "; --force-quota overrides"
	if force {
		forced = " (--force-quota)"
	}
	var base, desc string
	var s map[string]any
	var session, weekly int
	for i := 0; ; i++ {
		var why string
		var err error
		if base, err = c.base(); err == nil {
			s = nil
			err = c.X.GetJSON(base+"/api/summary", &s)
		}
		if err == nil {
			desc = fmt.Sprintf("session %v, weekly %v", s["session"], s["weekly"])
			var ok1, ok2 bool
			session, ok1 = Percent(s["session"])
			weekly, ok2 = Percent(s["weekly"])
			if ok1 && ok2 {
				break
			}
			why = desc + ": usage unknown (horus-eye rate-limited)"
		} else {
			why = fmt.Sprintf("horus-eye unreachable (%v)", err)
		}
		if force {
			return true, why + ", not launching blind" + forced
		}
		if i == Retries {
			return false, fmt.Sprintf("%s in %d readings over %d min, not launching blind%s", why, i+1,
				int((Retries * Wait).Minutes()), forced)
		}
		c.Sleep(Wait)
	}
	var over []string
	if session >= lim.Window {
		over = append(over, "session")
	}
	if weekly >= lim.Weekly {
		over = append(over, "weekly")
	}
	if len(over) == 0 {
		return true, fmt.Sprintf("%s (limits %d%% / %d%%)", desc, lim.Window, lim.Weekly)
	}
	var u usage
	r := map[string]string{}
	if c.X.GetJSON(base+"/api/usage", &u) == nil {
		r = u.resets()
	}
	for i, k := range over {
		over[i] = k + " over its limit"
		if r[k] != "" {
			over[i] += ", resets " + r[k]
		}
	}
	return force, desc + ": " + strings.Join(over, ", ") + forced
}
