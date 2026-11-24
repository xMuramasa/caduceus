// Package github reaches GitHub through the gh CLI only: issue reads, blocked-by, comments and the board's Status.
package github

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/ext"
)

const issueFields = "number,title,body,state,labels,comments,url"

// Issue is what `gh issue view --json` gives for issueFields.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	URL    string `json:"url"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Comments []struct {
		Body string `json:"body"`
	} `json:"comments"`
}

// LabelNames lists the issue's label names.
func (i *Issue) LabelNames() []string {
	out := make([]string, len(i.Labels))
	for k, l := range i.Labels {
		out[k] = l.Name
	}
	return out
}

// Blocker is an issue blocking another; it can live in any repo.
type Blocker struct {
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
}

func gh(x ext.Ext, args ...string) (string, error) { return x.Run(append([]string{"gh"}, args...)) }

func decode(out string, v any, what string) error {
	if err := json.Unmarshal([]byte(out), v); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// View reads repo#n.
func View(x ext.Ext, repo string, n int) (*Issue, error) {
	out, err := gh(x, "issue", "view", fmt.Sprint(n), "-R", repo, "--json", issueFields)
	if err != nil {
		return nil, err
	}
	var i Issue
	return &i, decode(out, &i, fmt.Sprintf("%s#%d", repo, n))
}

// BlockedBy lists the issues blocking repo#n.
func BlockedBy(x ext.Ext, repo string, n int) ([]Blocker, error) {
	out, err := gh(x, "api", fmt.Sprintf("repos/%s/issues/%d/dependencies/blocked_by?per_page=100", repo, n))
	if err != nil {
		return nil, err
	}
	var b []Blocker
	return b, decode(out, &b, fmt.Sprintf("%s#%d blocked-by", repo, n))
}

// Comment posts body on repo#n through a file, so the text never reaches a command line.
func Comment(x ext.Ext, repo string, n int, body string) error {
	f, err := os.CreateTemp("", "caduceus-comment-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	_, err = gh(x, "issue", "comment", fmt.Sprint(n), "-R", repo, "--body-file", f.Name())
	return err
}

// graphql runs a query; string vars go as -f, int vars as -F.
func graphql(x ext.Ext, query string, vars map[string]any, v any) error {
	args := []string{"api", "graphql", "-f", "query=" + query}
	for _, k := range slices.Sorted(maps.Keys(vars)) {
		flag := "-f"
		if _, ok := vars[k].(int); ok {
			flag = "-F"
		}
		args = append(args, flag, fmt.Sprintf("%s=%v", k, vars[k]))
	}
	out, err := gh(x, args...)
	if err != nil {
		return err
	}
	var d struct {
		Data   json.RawMessage `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	if err := decode(out, &d, "graphql"); err != nil {
		return err
	}
	if len(d.Errors) > 0 && string(d.Errors) != "null" { // GraphQL errors can come back with exit code 0
		e := string(d.Errors)
		return fmt.Errorf("graphql: %s", e[:min(len(e), 300)])
	}
	return decode(string(d.Data), v, "graphql")
}

// ItemOf is repo#n's item id on the board, from the issue side (a board scan misses items past 100).
func ItemOf(x ext.Ext, board config.Board, repo string, n int) (string, error) {
	owner, name, _ := strings.Cut(repo, "/")
	q := "query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){issue(number:$n)" +
		"{projectItems(first:10){nodes{id project{id}}}}}}"
	var d struct {
		Repository struct {
			Issue struct {
				ProjectItems struct {
					Nodes []struct {
						ID      string `json:"id"`
						Project struct {
							ID string `json:"id"`
						} `json:"project"`
					} `json:"nodes"`
				} `json:"projectItems"`
			} `json:"issue"`
		} `json:"repository"`
	}
	if err := graphql(x, q, map[string]any{"o": owner, "r": name, "n": n}, &d); err != nil {
		return "", err
	}
	for _, node := range d.Repository.Issue.ProjectItems.Nodes {
		if node.Project.ID == board.ID {
			return node.ID, nil
		}
	}
	return "", fmt.Errorf("%s#%d is not on the board", repo, n)
}

// SetStatus sets repo#n's Status on the board to the named option.
func SetStatus(x ext.Ext, board config.Board, repo string, n int, status string) error {
	opt, ok := board.Status[status]
	if !ok {
		return fmt.Errorf("the config's board.status has no %q", status)
	}
	item, err := ItemOf(x, board, repo, n)
	if err != nil {
		return err
	}
	q := "mutation($p:ID!,$i:ID!,$f:ID!,$o:String!){updateProjectV2ItemFieldValue(input:{projectId:$p,itemId:$i," +
		"fieldId:$f,value:{singleSelectOptionId:$o}}){projectV2Item{id}}}"
	var d any
	return graphql(x, q, map[string]any{"p": board.ID, "i": item, "f": board.StatusField, "o": opt}, &d)
}
