package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadTypedAndModuleKeys(t *testing.T) {
	t.Setenv("PORT", "")
	c, err := Load(write(t, "\uFEFF"+`{"port":"3081","db_enabled":1,"db_port":"1433","sap_port":5000,"api_batch_size":"7","my_module_url":"http://x","my_module_on":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 3081 || !c.DB.Enabled || c.DB.Port != 1433 || c.String("sap_port") != "5000" || c.Int("api_batch_size") != 7 || c.Bool("sap_enabled") {
		t.Errorf("%+v", c)
	}
	// keys no one declared are still reachable for a new module
	if c.String("my_module_url") != "http://x" || !c.Bool("my_module_on") || c.Int("missing") != 0 {
		t.Error("module-specific keys")
	}
}

func TestPortEnvWinsAndDefault(t *testing.T) {
	t.Setenv("PORT", "9000")
	c, _ := Load(write(t, `{"port":3081}`))
	if c.Port != 9000 {
		t.Errorf("PORT env: %d", c.Port)
	}
	t.Setenv("PORT", "")
	if c, _ = Load(write(t, `{}`)); c.Port != 5000 {
		t.Errorf("default port: %d", c.Port)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("missing file")
	}
	if _, err := Load(write(t, `{bad`)); err == nil {
		t.Error("bad json")
	}
	if _, err := Load(write(t, `null`)); err == nil {
		t.Error("null config")
	}
}
