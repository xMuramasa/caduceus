package github

import (
	"strings"
	"testing"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
)

func TestSetStatusErrors(t *testing.T) {
	board := config.Board{ID: "PVT_FAKE", StatusField: "PVTSSF_FAKE", Status: map[string]string{"In progress": "47fc9ee4"}}
	for _, tc := range []struct{ answer, status, want string }{
		{`{"data":null,"errors":[{"message":"Could not resolve to a Repository"}]}`, "In progress",
			`graphql: [{"message":"Could not resolve to a Repository"}]`},
		{`{"data":{"repository":{"issue":{"projectItems":{"nodes":[{"id":"PVTI_X","project":{"id":"PVT_OTHER"}}]}}}}}`,
			"In progress", "xMuramasa/hermes-relay#27 is not on the board"},
		{"", "Doing", `the config's board.status has no "Doing"`},
	} {
		f := &exttest.Fake{T: t, Commands: map[string]func([]string) (string, error){
			"gh": func([]string) (string, error) { return tc.answer, nil },
		}}
		err := SetStatus(f, board, "xMuramasa/hermes-relay", 27, tc.status)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v, want %q", err, tc.want)
		}
	}
}
