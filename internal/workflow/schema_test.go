package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// conforms checks v against the subset of JSON Schema that Schema emits.
func conforms(root, s map[string]any, v any, path string) error {
	if ref, ok := s["$ref"].(string); ok {
		return conforms(root, root["definitions"].(map[string]any)[strings.TrimPrefix(ref, "#/definitions/")].(map[string]any), v, path)
	}
	if alts, ok := s["anyOf"].([]any); ok {
		var errs []string
		for _, a := range alts {
			err := conforms(root, a.(map[string]any), v, path)
			if err == nil {
				errs = nil
				break
			}
			errs = append(errs, err.Error())
		}
		if errs != nil {
			return fmt.Errorf("%s: no alternative fits (%s)", path, strings.Join(errs, "; "))
		}
	}
	kind := map[string]string{"string": "string", "bool": "boolean", "int": "integer", "float64": "number", "<nil>": "null",
		"map[string]interface {}": "object", "[]interface {}": "array"}[fmt.Sprintf("%T", v)]
	var types []string
	switch t := s["type"].(type) {
	case string:
		types = []string{t}
	case []string:
		types = t
	}
	fits := len(types) == 0
	for _, t := range types {
		fits = fits || t == kind || (t == "number" && kind == "integer")
	}
	if !fits {
		return fmt.Errorf("%s: is %s, want %v", path, kind, types)
	}
	if enum, ok := s["enum"].([]string); ok && !contains(enum, fmt.Sprint(v)) {
		return fmt.Errorf("%s: %v is not one of %v", path, v, enum)
	}
	switch v := v.(type) {
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		required, _ := s["required"].([]string)
		for _, k := range required {
			if _, ok := v[k]; !ok {
				return fmt.Errorf("%s: missing %q", path, k)
			}
		}
		for k, child := range v {
			sub, known := props[k].(map[string]any)
			if !known {
				switch extra := s["additionalProperties"].(type) {
				case bool:
					if !extra {
						return fmt.Errorf("%s: unknown key %q", path, k)
					}
					continue
				case map[string]any:
					sub = extra
				default:
					continue
				}
			}
			if err := conforms(root, sub, child, path+"."+k); err != nil {
				return err
			}
		}
	case []any:
		if items, ok := s["items"].(map[string]any); ok {
			for i, child := range v {
				if err := conforms(root, items, child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// The schema accepts every workflow the parser accepts in this repository:
// a false error in an editor is worse than a missed one.
func TestSchemaAcceptsRealWorkflows(t *testing.T) {
	schema := Schema()
	files, _ := filepath.Glob("../../examples/*.yaml")
	more, _ := filepath.Glob("../../workflows/*.yaml")
	if files = append(files, more...); len(files) < 3 {
		t.Fatalf("found only %v", files)
	}
	docs := map[string][]byte{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		docs[f] = data
	}
	for _, s := range Describe().Steps {
		if s.Example != "" {
			docs[s.Op+" example"] = []byte(fmt.Sprintf("name: T\ninput: {input: {label: In}}\nsteps:\n  - %s: %s\noutput:\n  sheets: {Out: out}\n", s.Op, s.Example))
		}
	}
	for name, data := range docs {
		var doc any
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := conforms(schema, schema, doc, "$"); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSchemaFlagsMistakes(t *testing.T) {
	schema := Schema()
	for name, steps := range map[string]string{
		"unknown step":     "- sorted: {by: [A]}",
		"unknown key":      "- sort: {by: [A], descending: true}",
		"missing required": "- join: {on: [A]}",
		"bad enum":         "- dedupe: {by: [A], keep: newest}",
		"nested step":      "- if: 'true'\n    then:\n      - nope: 1",
	} {
		var doc any
		src := "name: T\ninput: {input: {label: In}}\nsteps:\n  " + steps + "\noutput:\n  sheets: {Out: out}\n"
		if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
			t.Fatal(err)
		}
		if conforms(schema, schema, doc, "$") == nil {
			t.Errorf("%s: the schema accepted %q", name, steps)
		}
	}
}

func TestReferenceCoversEveryStep(t *testing.T) {
	ref := Reference()
	for _, s := range Describe().Steps {
		if !strings.Contains(ref, "### "+s.Op+"\n") || !strings.Contains(ref, s.Help) {
			t.Errorf("the reference lacks step %q", s.Op)
		}
	}
	for _, f := range Describe().Functions {
		if !strings.Contains(ref, "`"+f.Signature+"`") {
			t.Errorf("the reference lacks helper %q", f.Name)
		}
	}
}
