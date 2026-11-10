package workflows_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"

	"miservice/internal/app"
	"miservice/internal/config"
	"miservice/internal/logx"
)

const routed = `name: Parts v2
service:
  route: parts-v2
  description: Clean part numbers.
  owner_required: false
  max_upload_mb: 5
input:
  parts: {label: Parts list, match: "(?i)parts"}
steps:
  - from: parts
  - save: cleaned
output:
  sheets: {Parts: cleaned}
`

func multipartBody(t *testing.T, fields map[string]string, files ...[2]string) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	for _, f := range files {
		fw, _ := mw.CreateFormFile("files", f[0])
		b, err := os.ReadFile(f[1])
		if err != nil {
			t.Fatal(err)
		}
		fw.Write(b)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestCatalogDescribeAndRun(t *testing.T) {
	ts, _ := newServer(t, [2]string{"parts.yaml", routed})

	resp, err := http.Get(ts.URL + "/api/workflows")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Workflows []struct {
			Name, Route, File, Description string
			Inputs                         []struct{ Name, Label, Match string }
			Sheets                         []string
			Owner                          struct {
				Field    string
				Required bool
			}
			MaxUploadMB int
			Endpoints   struct{ Upload, Download, Run string }
		}
		Steps []string
	}
	json.NewDecoder(resp.Body).Decode(&catalog)
	resp.Body.Close()
	if len(catalog.Workflows) != 2 || !strings.Contains(strings.Join(catalog.Steps, ","), "pick") {
		t.Fatalf("catalog %+v", catalog)
	}
	var v2 = catalog.Workflows[1]
	if catalog.Workflows[0].Route == "parts-v2" {
		v2 = catalog.Workflows[0]
	}
	if v2.Route != "parts-v2" || v2.File != "parts.yaml" || v2.Description != "Clean part numbers." ||
		v2.Owner.Required || v2.Owner.Field != "bam" || v2.MaxUploadMB != 5 ||
		v2.Inputs[0].Match != "(?i)parts" || v2.Sheets[0] != "Parts" || v2.Endpoints.Run != "/api/parts-v2/run" {
		t.Errorf("described %+v", v2)
	}

	resp, _ = http.Get(ts.URL + "/api/parts-v2")
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"route":"parts-v2"`) {
		t.Errorf("describe %d %s", resp.StatusCode, b)
	}
	if resp, _ := http.Get(ts.URL + "/api/parts/download"); resp.StatusCode != 404 {
		t.Errorf("the file name must not be a route once service.route is set: %d", resp.StatusCode)
	}

	// run: no owner needed here, the workbook is the response, nothing is kept.
	body, kind := multipartBody(t, nil, [2]string{"Parts.xlsx", "../../../examples/parts.xlsx"})
	resp, err = http.Post(ts.URL+"/api/parts-v2/run", kind, body)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.HasPrefix(b, []byte("PK")) ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), "Parts_anonymous.xlsx") {
		t.Errorf("run %d %v %q", resp.StatusCode, resp.Header, b[:min(len(b), 40)])
	}
	if resp, _ := http.Get(ts.URL + "/api/parts-v2/download"); resp.StatusCode != 404 {
		t.Errorf("run must not replace the latest download: %d", resp.StatusCode)
	}

	// The BOM still requires its owner on run.
	body, kind = multipartBody(t, nil,
		[2]string{"Customer BOM.xlsx", "../../../testdata/bom_basic.xlsx"},
		[2]string{"Priority.xlsx", "../../../testdata/prio_basic.xlsx"})
	if resp, _ := http.Post(ts.URL+"/api/bom/run", kind, body); resp.StatusCode != 400 {
		t.Errorf("bom run without bam: %d", resp.StatusCode)
	}
}

func TestRouteConflictsStopStartup(t *testing.T) {
	for _, tc := range []struct{ file, yaml, want string }{
		{"other.yaml", strings.Replace(routed, "parts-v2", "bom", 1), "both use the route"},
		{"x.yaml", strings.Replace(routed, "parts-v2", "workflows", 1), "reserved"},
		{"y.yaml", strings.Replace(routed, "parts-v2", "Bad Route", 1), "service.route"},
		{"z.yaml", strings.Replace(routed, "max_upload_mb: 5", "max_upload_mb: -1", 1), "max_upload_mb"},
	} {
		dir := t.TempDir()
		os.MkdirAll(dir+"/workflows", 0o755)
		os.WriteFile(dir+"/workflows/"+tc.file, []byte(tc.yaml), 0o600)
		if _, err := app.New(&config.Config{Dir: dir}, logx.Nop, app.DefaultModules); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err=%v, want %q", tc.file, err, tc.want)
		}
	}
}

func TestCSVEndpoint(t *testing.T) {
	ts, dir := newServer(t, [2]string{"orders.yaml", `name: Orders
input: {orders: {label: Orders}}
steps:
  - from: orders
  - drop: [Note]
  - save: out
output:
  format: csv
  sheets: {Orders: out}
`})
	src := dir + "/orders.csv"
	os.WriteFile(src, []byte("ID,Note\n1,a\n"), 0o600)
	body, kind := multipartBody(t, map[string]string{"bam": "7"}, [2]string{"orders.csv", src})
	resp, err := http.Post(ts.URL+"/api/orders/run", kind, body)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), "orders_7.csv") || string(b) != "\xEF\xBB\xBFID\n1\n" {
		t.Errorf("csv run %d %v %q", resp.StatusCode, resp.Header, b)
	}
}
