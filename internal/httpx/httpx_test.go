package httpx

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"miservice/internal/logx"
)

func serve(rt *Router, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	rt.Handler().ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestRoutingRules(t *testing.T) {
	rt := NewRouter(logx.Nop)
	ok := func(w http.ResponseWriter, r *http.Request) error {
		return WriteJSON(w, 200, map[string]string{"ok": "1"})
	}
	rt.GET("/api/Thing", ok)
	rt.POST("/api/post", ok)
	rt.Any("/api/any", ok)
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/api/thing", 200}, {"HEAD", "/api/thing/", 200}, {"POST", "/api/thing", 404},
		{"POST", "/api/post", 200}, {"GET", "/api/post", 404}, {"DELETE", "/api/any", 200}, {"GET", "/nope", 404},
	} {
		if got := serve(rt, c.method, c.path).Code; got != c.want {
			t.Errorf("%s %s = %d, want %d", c.method, c.path, got, c.want)
		}
	}
	if b := serve(rt, "GET", "/x?y=1").Body.String(); b != "Cannot GET /x" {
		t.Errorf("404 body %q", b)
	}
	if len(rt.Routes()) != 3 {
		t.Errorf("%v", rt.Routes())
	}
}

func TestDuplicateRoutePanics(t *testing.T) {
	rt := NewRouter(logx.Nop)
	h := func(http.ResponseWriter, *http.Request) error { return nil }
	rt.GET("/a", h)
	defer func() {
		if recover() == nil {
			t.Error("registering /a twice must panic at startup")
		}
	}()
	rt.GET("/A/", h)
}

func TestErrorShapes(t *testing.T) {
	var logged []string
	rt := NewRouter(func(s string) { logged = append(logged, s) })
	rt.GET("/fail", func(http.ResponseWriter, *http.Request) error { return Fail(400, "nope") })
	rt.GET("/status", func(http.ResponseWriter, *http.Request) error { return Status(400, errors.New("too big")) })
	rt.GET("/boom", func(http.ResponseWriter, *http.Request) error { return errors.New("boom") })
	if w := serve(rt, "GET", "/fail"); w.Code != 400 || w.Body.String() != `{"error":"nope"}` || len(logged) != 0 {
		t.Errorf("Fail: %d %s %v", w.Code, w.Body, logged)
	}
	if w := serve(rt, "GET", "/status"); w.Code != 400 || w.Body.String() != `{"status":"error","message":"too big"}` {
		t.Errorf("Status: %d %s", w.Code, w.Body)
	}
	if w := serve(rt, "GET", "/boom"); w.Code != 500 || w.Body.String() != `{"status":"error","message":"boom"}` || len(logged) != 2 || logged[1] != "ERROR: boom" {
		t.Errorf("plain: %d %s %v", w.Code, w.Body, logged)
	}
}

func TestMiddleware(t *testing.T) {
	rt := NewRouter(logx.Nop)
	rt.Use(Recover(logx.Nop), CORS())
	rt.GET("/panic", func(http.ResponseWriter, *http.Request) error { panic("bug") })
	rt.GET("/ok", func(w http.ResponseWriter, _ *http.Request) error { return WriteJSON(w, 200, 1) })
	if w := serve(rt, "GET", "/panic"); w.Code != 500 || !strings.Contains(w.Body.String(), "Internal Server Error") {
		t.Errorf("recover: %d %s", w.Code, w.Body)
	}
	w := serve(rt, "GET", "/ok")
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS header")
	}
	w = serve(rt, "OPTIONS", "/anything")
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Errorf("preflight %d", w.Code)
	}
}

func multipartReq(t *testing.T, field string, nfiles int, big bool) *http.Request {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("bam", "7")
	mw.WriteField("bam", "8")
	for i := 0; i < nfiles; i++ {
		fw, _ := mw.CreateFormFile(field, `C:\x\f.xlsx`)
		if big {
			fw.Write(make([]byte, 2<<20))
		} else {
			fw.Write([]byte("data"))
		}
	}
	mw.Close()
	r := httptest.NewRequest("POST", "/", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func TestParseUpload(t *testing.T) {
	opt := UploadOptions{Dir: t.TempDir(), FieldName: "files", MaxFiles: 2, MaxFileMB: 1}
	up, err := ParseUpload(multipartReq(t, "files", 2, false), opt)
	if err != nil || len(up.Files) != 2 || up.Files[0].Name != "f.xlsx" || up.Field("bam") != "7,8" {
		t.Fatalf("%+v %v", up, err)
	}
	up.Cleanup()
	if left, _ := os.ReadDir(opt.Dir); len(left) != 0 {
		t.Error("Cleanup must remove stored files")
	}
	if _, err := ParseUpload(multipartReq(t, "files", 3, false), opt); err == nil || err.Error() != "Unexpected field" {
		t.Errorf("third file: %v", err)
	}
	if _, err := ParseUpload(multipartReq(t, "other", 1, false), opt); err == nil || err.Error() != "Unexpected field" {
		t.Errorf("wrong field: %v", err)
	}
	_, err = ParseUpload(multipartReq(t, "files", 1, true), opt)
	var he *Error
	if !errors.As(err, &he) || he.Status != 400 || !strings.Contains(err.Error(), "1 MB or smaller") {
		t.Errorf("too large: %v", err)
	}
	if left, _ := os.ReadDir(opt.Dir); len(left) != 0 {
		t.Error("failed parses must not leave files behind")
	}
	plain := httptest.NewRequest("POST", "/", io.NopCloser(strings.NewReader("x")))
	if up, err := ParseUpload(plain, opt); err != nil || len(up.Files) != 0 {
		t.Error("non-multipart request yields an empty Upload")
	}
}
