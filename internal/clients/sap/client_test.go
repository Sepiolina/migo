package sap

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func endpointOf(t *testing.T, ts *httptest.Server) Endpoint {
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	return Endpoint{Host: host, Port: port, Tcode: "T1", EmployeeID: "42"}
}

func TestCallRFCShapesAndKeyNormalising(t *testing.T) {
	var got map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.Write([]byte(`{"tables":[{"rows":[{" MATNR ":"A","Labst":5}]},{"rows":["junk"]}]}`))
	}))
	defer ts.Close()
	rows, err := (&Client{}).CallRFC(context.Background(), endpointOf(t, ts), []map[string]any{{"WERKS": "1000"}}, 0)
	if err != nil || len(rows) != 1 || rows[0]["matnr"] != "A" || rows[0]["labst"] != 5.0 {
		t.Fatalf("%v %v", rows, err)
	}
	if got["tcode"] != "T1" || got["employee_id"] != "42" || len(got["DATA"].([]any)) != 1 {
		t.Errorf("payload %v", got)
	}
}

func TestRetryBackoffAndGiveUp(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "no", 500)
	}))
	defer ts.Close()
	var pauses []time.Duration
	c := &Client{Sleep: func(d time.Duration) { pauses = append(pauses, d) }}
	_, err := c.CallRFC(context.Background(), endpointOf(t, ts), nil, 3)
	if err == nil || !strings.Contains(err.Error(), "status code 500") {
		t.Errorf("err %v", err)
	}
	if calls != 4 || len(pauses) != 3 || pauses[0] != 2*time.Second || pauses[1] != 4*time.Second || pauses[2] != 8*time.Second {
		t.Errorf("calls=%d pauses=%v", calls, pauses)
	}
}

func TestExtractRowsShapes(t *testing.T) {
	for _, js := range []string{`[{"a":1}]`, `{"DATA":[{"a":1}]}`, `{"data":[{"a":1}]}`, `{"rows":[{"a":1}]}`} {
		var v any
		json.Unmarshal([]byte(js), &v)
		if len(extractRows(v)) != 1 {
			t.Errorf("%s", js)
		}
	}
	if extractRows("text") != nil {
		t.Error("non-JSON body has no rows")
	}
}
