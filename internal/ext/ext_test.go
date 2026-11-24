package ext_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
)

func TestRealRun(t *testing.T) {
	tests := []struct {
		args    []string
		out     string
		code    int
		message string
	}{
		{[]string{"sh", "-c", "echo hi"}, "hi\n", 0, ""},
		{[]string{"sh", "-c", "echo out; echo boom >&2; exit 3"}, "out\n", 3, "sh -c echo out; echo boom >&2; exit 3: exit 3: boom"},
		{[]string{"sh", "-c", "echo only-stdout; exit 1"}, "only-stdout\n", 1, "sh -c echo only-stdout; exit 1: exit 1: only-stdout"},
		{[]string{"caduceus-no-such-command"}, "", -1, ""},
	}
	for _, tc := range tests {
		t.Run(tc.args[0], func(t *testing.T) {
			out, err := ext.Real{}.Run(tc.args)
			if out != tc.out {
				t.Errorf("stdout %q, want %q", out, tc.out)
			}
			if tc.code == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var e *ext.Error
			if !errors.As(err, &e) || e.Code != tc.code {
				t.Fatalf("err %v, want code %d", err, tc.code)
			}
			if tc.message != "" && e.Error() != tc.message {
				t.Errorf("message %q, want %q", e.Error(), tc.message)
			}
		})
	}
}

func TestRealGetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/summary" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"session": "43%", "weekly": "n/a"}`)
	}))
	defer srv.Close()
	var got map[string]string
	if err := (ext.Real{}).GetJSON(srv.URL+"/api/summary", &got); err != nil {
		t.Fatal(err)
	}
	if got["session"] != "43%" || got["weekly"] != "n/a" {
		t.Errorf("got %v", got)
	}
	if err := (ext.Real{}).GetJSON(srv.URL+"/other", &got); err == nil {
		t.Error("404 gave no error")
	}
}

type recorder struct{ errs []string }

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func TestFakeAnswersAndFailsOnUnexpectedCalls(t *testing.T) {
	rec := &recorder{}
	f := &exttest.Fake{
		T:        rec,
		Commands: map[string]func([]string) (string, error){"date": func([]string) (string, error) { return "03:31\n", nil }},
		JSON:     map[string]any{"http://horus/api/summary": map[string]string{"session": "12%"}},
	}
	if out, err := f.Run([]string{"date", "+%H:%M"}); out != "03:31\n" || err != nil {
		t.Errorf("date: %q %v", out, err)
	}
	var s struct{ Session string }
	if err := f.GetJSON("http://horus/api/summary", &s); err != nil || s.Session != "12%" {
		t.Errorf("summary: %+v %v", s, err)
	}
	if len(rec.errs) != 0 {
		t.Fatalf("expected calls failed the test: %v", rec.errs)
	}
	if _, err := f.Run([]string{"gh", "issue", "view"}); err == nil {
		t.Error("unexpected gh call gave no error")
	}
	if _, err := f.Run([]string{"git", "status"}); err == nil {
		t.Error("git without Git gave no error")
	}
	if err := f.GetJSON("http://horus/api/usage", &s); err == nil {
		t.Error("unexpected GET gave no error")
	}
	if len(rec.errs) != 3 {
		t.Errorf("want 3 test failures, got %v", rec.errs)
	}
	if len(f.Calls) != 5 {
		t.Errorf("calls %v", f.Calls)
	}
}
