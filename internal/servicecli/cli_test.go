package servicecli

import "testing"

func TestConfigEnvironmentPrecedence(t *testing.T) {
	t.Setenv("MISERVICE_CONFIG", "legacy.json")
	t.Setenv("MAISERVICE_CONFIG", "current.json")
	if got := defaultConfigPath(); got != "current.json" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("MAISERVICE_CONFIG", "")
	if got := defaultConfigPath(); got != "legacy.json" {
		t.Fatalf("legacy fallback got %q", got)
	}
}
