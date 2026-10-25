// Package config loads config.json with the same loose typing the Node
// service tolerated (numbers given as strings, JS truthiness for flags).
//
// Settings every module shares (port, directories, the SQL Server and SAP
// connections) are typed fields. A module that needs its OWN keys does not
// have to touch this file: read them with cfg.String / cfg.Int / cfg.Bool.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"miservice/internal/jsx"
)

// Config is the parsed config.json.
type Config struct {
	Port int
	// Dir is the directory holding the config file; uploads/ and outputs/
	// live beside it (as they sit in the project root for the Node service).
	Dir string

	DB  DB
	SAP SAP

	raw map[string]any // every key as decoded, for module-specific settings
}

// DB is the SQL Server connection ("db_*" keys).
type DB struct {
	Enabled  bool
	Server   string
	Port     int
	User     string
	Password string
	Name     string
	TLSMin   string // "db_tls_min": lowest TLS version, default "1.0"
}

// SAP is the SAP RFC gateway connection ("sap_*" and "api_*" keys).
type SAP struct {
	Enabled   bool
	Host      string
	Port      string
	Tcode     any // passed through to SAP unchanged (string or number)
	Werks     any
	BatchSize int // api_batch_size (0 = module default)
	Retries   int // api_retries    (0 = module default)
}

// Bool reads any config.json key with JS truthiness.
func (c *Config) Bool(key string) bool { return jsx.Truthy(c.raw[key]) }

// String reads any config.json key as a string ("" when missing).
func (c *Config) String(key string) string { return str(c.raw[key]) }

// Int reads any config.json key like parseInt(v, 10) (0 when missing or NaN).
func (c *Config) Int(key string) int { return parseInt(c.raw[key]) }

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return jsx.NumToString(x)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// parseInt is parseInt(v, 10) with 0 standing in for NaN.
func parseInt(v any) int {
	f, ok := jsx.ParseFloat(str(v))
	if !ok || math.IsInf(f, 0) {
		return 0
	}
	return int(f)
}

// Load reads path (or, when it does not exist, config.example.json in the
// same directory), then applies the PORT environment override, which wins
// over the file's "port" like it does in the Node service.
func Load(path string) (*Config, error) {
	target := path
	if _, err := os.Stat(target); err != nil {
		alt := filepath.Join(filepath.Dir(path), "config.example.json")
		if _, err2 := os.Stat(alt); err2 != nil {
			return nil, fmt.Errorf("no config.json found at %s. Copy config.example.json to config.json and fill in real values", path)
		}
		target = alt
	}
	b, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	b = []byte(strings.TrimPrefix(string(b), "\uFEFF"))
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", target, err)
	}
	if m == nil {
		return nil, errors.New("config must be a JSON object")
	}
	abs, _ := filepath.Abs(target)
	c := &Config{raw: m, Dir: filepath.Dir(abs)}
	c.DB = DB{
		Enabled:  c.Bool("db_enabled"),
		Server:   c.String("db_server"),
		Port:     c.Int("db_port"),
		User:     c.String("db_user"),
		Password: c.String("db_password"),
		Name:     c.String("db_name"),
		TLSMin:   c.String("db_tls_min"),
	}
	c.SAP = SAP{
		Enabled:   c.Bool("sap_enabled"),
		Host:      c.String("sap_host"),
		Port:      c.String("sap_port"),
		Tcode:     m["sap_tcode"],
		Werks:     m["sap_werks"],
		BatchSize: c.Int("api_batch_size"),
		Retries:   c.Int("api_retries"),
	}
	c.Port = parseInt(os.Getenv("PORT"))
	if c.Port == 0 {
		c.Port = c.Int("port")
	}
	if c.Port == 0 {
		c.Port = 5000
	}
	return c, nil
}
