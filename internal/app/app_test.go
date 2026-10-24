package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"miservice/internal/config"
	"miservice/internal/httpx"
	"miservice/internal/logx"
	"miservice/internal/module"
)

// exampleModule is the pattern from docs/EXTENDING.md: a constructor, Routes,
// and the optional Starter / Closer hooks.
type exampleModule struct {
	started, closed *bool
}

func (exampleModule) Name() string { return "example" }
func (m exampleModule) Routes(r *httpx.Router) {
	r.GET("/api/example/ping", func(w http.ResponseWriter, _ *http.Request) error {
		return httpx.WriteJSON(w, http.StatusOK, map[string]string{"pong": "yes"})
	})
}
func (m exampleModule) Start(context.Context) error { *m.started = true; return nil }
func (m exampleModule) Close() error                { *m.closed = true; return nil }

func TestModulesAreWiredStartedAndClosed(t *testing.T) {
	var started, closed bool
	factory := func(module.Services) (module.Module, error) { return exampleModule{&started, &closed}, nil }
	a, err := New(&config.Config{Dir: t.TempDir()}, logx.Nop, []module.Factory{factory})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/example/ping", nil))
	if w.Code != 200 || w.Body.String() != `{"pong":"yes"}` || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.start(ctx); err != nil || !started {
		t.Errorf("Starter not called: %v", err)
	}
	a.closeModules()
	if !closed {
		t.Error("Closer not called")
	}
}

func TestFailingFactoryStopsStartup(t *testing.T) {
	var started, closed bool
	ok := func(module.Services) (module.Module, error) { return exampleModule{&started, &closed}, nil }
	bad := func(module.Services) (module.Module, error) { return nil, errors.New("no database") }
	if _, err := New(&config.Config{Dir: t.TempDir()}, logx.Nop, []module.Factory{ok, bad}); err == nil {
		t.Fatal("expected error")
	}
	if !closed {
		t.Error("modules created before the failure must be closed")
	}
}

func TestDefaultModulesServeHealth(t *testing.T) {
	a, err := New(&config.Config{Dir: t.TempDir()}, logx.Nop, DefaultModules)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/health", nil))
	if w.Code != 200 {
		t.Error(w.Code)
	}
}
