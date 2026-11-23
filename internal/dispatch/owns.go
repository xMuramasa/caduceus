package dispatch

import (
	"regexp"
	"strings"
)

var backticked = regexp.MustCompile("`([^`]+)`")

// Owns gives the backticked paths or globs on the issue body's line starting "Owns:" (a list or bold mark before
// it allowed), or nil when there is no such line.
func Owns(body string) []string {
	for _, line := range strings.Split(body, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimLeft(line, "-*_> \t"), "Owns:")
		if !ok {
			continue
		}
		var out []string
		for _, m := range backticked.FindAllStringSubmatch(rest, -1) {
			out = append(out, strings.TrimPrefix(strings.TrimSpace(m[1]), "./"))
		}
		return out
	}
	return nil
}

// Overlap reports whether two Owns entries could match the same path: the literal part of one (before its first
// glob character) is a prefix of the other's, ending at a path boundary unless a glob follows it.
func Overlap(a, b string) bool {
	pa, ga := literal(a)
	pb, gb := literal(b)
	if len(pa) > len(pb) {
		pa, pb, ga = pb, pa, gb
	}
	if !strings.HasPrefix(pb, pa) {
		return false
	}
	return ga || pa == "" || len(pa) == len(pb) || strings.HasSuffix(pa, "/") || pb[len(pa)] == '/'
}

func literal(glob string) (string, bool) {
	if i := strings.IndexAny(glob, "*?["); i >= 0 {
		return glob[:i], true
	}
	return glob, false
}
