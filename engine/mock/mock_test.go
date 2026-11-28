package mock

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEntriesAnswerInOrderAndTheLastRepeats(t *testing.T) {
	set, err := Parse([]byte(`
config: {site: A}
sql:
  - {query: Parts, columns: [N], rows: [[1]]}
  - {query: Parts, columns: [N], rows: [[2]]}
  - {columns: [N], rows: [[9]]}
rest:
  - {url: /a, method: post, body: {n: 1}}
  - {url: /a, body: second}
  - {url: /down, status: 503}
`))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := set.Runtime(map[string]any{"extra": true})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Cfg.String("site") != "A" || !rt.Cfg.Bool("extra") {
		t.Errorf("config: %v %v", rt.Cfg.Value("site"), rt.Cfg.Value("extra"))
	}
	var got []any
	for _, q := range []string{"SELECT * FROM Parts", "SELECT * FROM Parts", "SELECT * FROM Parts", "SELECT 1"} {
		table, err := rt.SQL(context.Background(), "", q, nil)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, table.Rows[0][0])
	}
	// Parts: first entry, second, then the catch-all (the next unused match); "SELECT 1": the catch-all again.
	if len(got) != 4 || got[0] != 1 || got[1] != 2 || got[2] != 9 || got[3] != 9 {
		t.Errorf("sql answers %v", got)
	}

	call := func(method, url string) (int, string, error) {
		req, _ := http.NewRequest(method, url, nil)
		resp, err := rt.HTTP.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body), nil
	}
	for i, want := range []string{`{"n":1}`, "second", "second"} {
		if status, body, err := call("POST", "http://host/a?page=1"); err != nil || status != 200 || body != want {
			t.Errorf("call %d: %d %q %v, want %q", i+1, status, body, err, want)
		}
	}
	if status, _, _ := call("GET", "http://host/down"); status != 503 {
		t.Errorf("status %d, want 503", status)
	}
	if _, _, err := call("GET", "http://host/unknown"); err == nil || !strings.Contains(err.Error(), "no mock answers GET http://host/unknown") {
		t.Errorf("unmatched call: %v", err)
	}
}

func TestParseRejectsMistakes(t *testing.T) {
	for _, src := range []string{"sqls: []", "rest: [{status: 99}]", "rest: [{uri: /a}]"} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("accepted %q", src)
		}
	}
	if set, err := Parse(nil); err != nil || len(set.SQL)+len(set.REST) != 0 {
		t.Errorf("an empty file should be an empty set: %v", err)
	}
}
