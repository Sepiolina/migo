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

func TestEcosystemIsPortableAndPortFree(t *testing.T) {
	for _, want := range []string{"process.platform === 'win32'", "'maiservice.exe'", "'maiservice'", "'config.json'"} {
		if !strings.Contains(ecosystem, want) {
			t.Errorf("ecosystem.config.js lacks %q:\n%s", want, ecosystem)
		}
	}
	if strings.Contains(ecosystem, "PORT") {
		t.Errorf("ecosystem.config.js sets a port; it belongs in config.json only:\n%s", ecosystem)
	}
}
