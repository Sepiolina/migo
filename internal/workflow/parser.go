package workflow

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads a workflow and resolves imports relative to their declaring files.
func Load(path string) (*Config, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Canonicalize existing files so symlinked imports cannot hide cycles.
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(data, filepath.Dir(path), map[string]bool{canonical: true})
}

// Parse resolves a workflow, using baseDir for imports and spreadsheet paths.
func Parse(data []byte, baseDir string) (*Config, error) {
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	return parse(data, baseDir, map[string]bool{})
}

func parse(data []byte, baseDir string, active map[string]bool) (*Config, error) {
	r := &resolver{presets: map[string]map[string]*yaml.Node{}}
	root, err := r.document(data, baseDir, active, false)
	if err != nil {
		return nil, err
	}
	// Validate even unused presets so misspellings never silently survive.
	for cat, names := range r.presets {
		for name := range names {
			steps, err := r.steps(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{mapping(categoryOperation[cat], mapping("preset", scalar(name)))}}, nil)
			if err != nil {
				return nil, fmt.Errorf("preset %s.%s: %w", cat, name, err)
			}
			for _, step := range steps {
				if err := validateStep(step); err != nil {
					return nil, fmt.Errorf("preset %s.%s: %w", cat, name, err)
				}
			}
		}
	}
	if p := field(root, "process"); p != nil && p.Kind == yaml.SequenceNode {
		for i, proc := range p.Content {
			if steps := field(proc, "do"); steps != nil {
				resolved, err := r.steps(steps, nil)
				if err != nil {
					return nil, fmt.Errorf("process %d: %w", i+1, err)
				}
				var n yaml.Node
				if err := n.Encode(resolved); err != nil {
					return nil, err
				}
				setField(proc, "do", &n)
			}
		}
	}
	removeField(root, "imports")
	removeField(root, "presets")
	var cfg Config
	if err := strictDecode(root, &cfg); err != nil {
		return nil, err
	}
	cfg.BaseDir = baseDir
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func readNode(data []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("multiple YAML documents are not supported")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("document must be a mapping")
	}
	if err := checkNode(doc.Content[0]); err != nil {
		return nil, err
	}
	return doc.Content[0], nil
}
func checkNode(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("line %d: YAML aliases are not supported", n.Line)
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Value == "<<" {
				return fmt.Errorf("line %d: mapping keys must be strings; merge keys are not supported", k.Line)
			}
			if seen[k.Value] {
				return fmt.Errorf("line %d: duplicate key %q", k.Line, k.Value)
			}
			seen[k.Value] = true
		}
	}
	for _, child := range n.Content {
		if err := checkNode(child); err != nil {
			return err
		}
	}
	return nil
}
func strictDecode(n *yaml.Node, v any) error {
	b, err := yaml.Marshal(n)
	if err != nil {
		return err
	}
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	return d.Decode(v)
}
func scalar(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
func mapping(k string, v *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{scalar(k), v}}
}
func field(n *yaml.Node, k string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == k {
			return n.Content[i+1]
		}
	}
	return nil
}
func setField(n *yaml.Node, k string, v *yaml.Node) {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == k {
			n.Content[i+1] = v
			return
		}
	}
	n.Content = append(n.Content, scalar(k), v)
}
func removeField(n *yaml.Node, k string) {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == k {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			return
		}
	}
}
func clone(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	v := *n
	v.Content = nil
	for _, c := range n.Content {
		v.Content = append(v.Content, clone(c))
	}
	return &v
}
