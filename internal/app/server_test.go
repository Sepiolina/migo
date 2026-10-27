package app

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"testing"

	"miservice/internal/config"
	"miservice/internal/logx"
	"miservice/internal/module"
)

type failingStarter struct {
	exampleModule
	ctx context.Context
	err error
}

func (m *failingStarter) Start(ctx context.Context) error { m.ctx = ctx; return m.err }

func TestStartupFailureReleasesResources(t *testing.T) {
	var started, closed bool
	failure := errors.New("startup failed")
	m := &failingStarter{exampleModule: exampleModule{&started, &closed}, err: failure}
	a, err := New(&config.Config{Dir: t.TempDir()}, logx.Nop, []module.Factory{
		func(module.Services) (module.Module, error) { return m, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := a.serve(context.Background(), ln); !errors.Is(err, failure) {
		t.Fatalf("got %v, want startup failure", err)
	}
	if !closed {
		t.Fatal("module was not closed")
	}
	if m.ctx.Err() != context.Canceled {
		t.Fatal("background work was not canceled")
	}
	replacement, err := net.Listen("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("listener was not released: %v", err)
	}
	replacement.Close()
}

func TestInvalidInheritedListener(t *testing.T) {
	if os.Getenv("MAI_TEST_INVALID_FD") == "1" {
		a, err := New(&config.Config{Dir: t.TempDir()}, logx.Nop, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Run(context.Background()); err == nil {
			t.Fatal("invalid descriptor accepted")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInvalidInheritedListener$")
	cmd.Env = append(os.Environ(), "MAI_TEST_INVALID_FD=1", "MAISERVICE_LISTEN_FD=3")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child failed: %v: %s", err, output)
	}
}
