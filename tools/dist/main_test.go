package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestAsk(t *testing.T) {
	for in, want := range map[string]string{"1\n": "linux", "2\n": "windows", "3\n": "all", "Windows\n": "windows", "both": "all"} {
		got, err := ask(strings.NewReader(in), io.Discard)
		if err != nil || got != want {
			t.Errorf("ask(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "4\n", "mac\n"} {
		if got, err := ask(strings.NewReader(in), io.Discard); err == nil {
			t.Errorf("ask(%q) = %q, want an error", in, got)
		}
	}
}

func TestRunRejectsBadFlags(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil { // the repository folder
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	for _, args := range [][]string{{"-os", "mac"}, {"-os", "linux", "-arch", "386"}, {"-os", "linux", "-port", "0"}, {"linux"}} {
		if err := run(args, strings.NewReader(""), io.Discard); err == nil {
			t.Errorf("run(%q) accepted", args)
		}
	}
}

func TestGeneratedFiles(t *testing.T) {
	win := target{"windows", "amd64"}
	if eco := ecosystem(win, 4000); !strings.Contains(eco, `"maiservice.exe"`) || !strings.Contains(eco, "PORT: '4000'") {
		t.Errorf("windows ecosystem:\n%s", eco)
	}
	if eco := ecosystem(target{"linux", "arm64"}, 3081); strings.Contains(eco, ".exe") {
		t.Errorf("linux ecosystem names an .exe:\n%s", eco)
	}
	if r := readme(win, 3081); !strings.Contains(r, `.\migo.exe`) || !strings.Contains(r, "smoke-test.ps1") {
		t.Errorf("windows readme:\n%s", r)
	}
}
