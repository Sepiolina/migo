package workflow

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var categoryOperation = map[string]string{"filters": "filter", "forward_fills": "forward_fill", "groups": "group", "aggregations": "aggregate", "mappings": "map", "styles": "style", "sorts": "sort", "transforms": "transform", "pipelines": "pipeline"}

type resolver struct {
	presets map[string]map[string]*yaml.Node
}

func (r *resolver) document(data []byte, dir string, active map[string]bool, imported bool) (*yaml.Node, error) {
	root, err := readNode(data)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{"imports": true, "presets": true}
	if !imported {
		for _, k := range []string{"workflow", "input", "process", "output"} {
			allowed[k] = true
		}
	}
	for i := 0; i < len(root.Content); i += 2 {
		if !allowed[root.Content[i].Value] {
			return nil, fmt.Errorf("unknown document field %q", root.Content[i].Value)
		}
	}
	if n := field(root, "imports"); n != nil {
		if n.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("imports must be a list")
		}
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || item.Value == "" {
				return nil, fmt.Errorf("import path must be a nonempty string")
			}
			path := filepath.Join(dir, item.Value)
			if filepath.IsAbs(item.Value) {
				path = item.Value
			}
			path, err = filepath.Abs(path)
			if err != nil {
				return nil, err
			}
			canonical, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil, fmt.Errorf("import %s: %w", item.Value, err)
			}
			if active[canonical] {
				return nil, fmt.Errorf("import cycle at %s", path)
			}
			b, e := os.ReadFile(path)
			if e != nil {
				return nil, fmt.Errorf("import %s: %w", path, e)
			}
			active[canonical] = true
			_, e = r.document(b, filepath.Dir(path), active, true)
			delete(active, canonical)
			if e != nil {
				return nil, fmt.Errorf("import %s: %w", path, e)
			}
		}
	}
	if n := field(root, "presets"); n != nil {
		if n.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("presets must be a mapping")
		}
		for i := 0; i < len(n.Content); i += 2 {
			cat, names := n.Content[i].Value, n.Content[i+1]
			if _, ok := categoryOperation[cat]; !ok {
				return nil, fmt.Errorf("unknown preset category %q", cat)
			}
			if names.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("presets.%s must be a mapping", cat)
			}
			if r.presets[cat] == nil {
				r.presets[cat] = map[string]*yaml.Node{}
			}
			for j := 0; j < len(names.Content); j += 2 {
				name := names.Content[j].Value
				if name == "" {
					return nil, fmt.Errorf("preset name cannot be empty")
				}
				if _, exists := r.presets[cat][name]; exists {
					return nil, fmt.Errorf("duplicate preset %s.%s", cat, name)
				}
				r.presets[cat][name] = clone(names.Content[j+1])
			}
		}
	}
	return root, nil
}
func merge(a, b *yaml.Node) *yaml.Node {
	if a == nil || a.Kind != yaml.MappingNode || b.Kind != yaml.MappingNode {
		return clone(b)
	}
	out := clone(a)
	for i := 0; i < len(b.Content); i += 2 {
		k, v := b.Content[i].Value, b.Content[i+1]
		if old := field(out, k); old != nil {
			setField(out, k, merge(old, v))
		} else {
			setField(out, k, clone(v))
		}
	}
	return out
}
func (r *resolver) resolve(op string, n *yaml.Node, stack map[string]bool) (*yaml.Node, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s must be a mapping", op)
	}
	if stack == nil {
		stack = map[string]bool{}
	}
	own := clone(n)
	var base *yaml.Node
	if p := field(own, "preset"); p != nil {
		if p.Kind != yaml.ScalarNode || p.Tag != "!!str" || p.Value == "" {
			return nil, fmt.Errorf("%s preset must be a nonempty string", op)
		}
		cat := ""
		for c, o := range categoryOperation {
			if o == op {
				cat = c
			}
		}
		key := cat + "." + p.Value
		if stack[key] {
			return nil, fmt.Errorf("preset cycle at %s", key)
		}
		def := r.presets[cat][p.Value]
		if def == nil {
			return nil, fmt.Errorf("unknown %s preset %q", op, p.Value)
		}
		stack[key] = true
		var err error
		base, err = r.resolve(op, def, stack)
		delete(stack, key)
		if err != nil {
			return nil, err
		}
		removeField(own, "preset")
	}
	override := field(own, "override")
	removeField(own, "override")
	if override != nil && override.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s override must be a mapping", op)
	}
	out := merge(base, own)
	if override != nil {
		out = merge(out, override)
	}
	if op == "group" {
		if p := field(out, "aggregation_preset"); p != nil {
			if p.Tag != "!!str" {
				return nil, fmt.Errorf("aggregation_preset must be a string")
			}
			a, err := r.resolve("aggregate", mapping("preset", p), stack)
			if err != nil {
				return nil, err
			}
			var spec AggregateSpec
			if err := strictDecode(a, &spec); err != nil {
				return nil, err
			}
			cols := field(a, "columns")
			if explicit := field(out, "aggregate"); explicit != nil {
				cols = merge(cols, explicit)
			}
			if cols != nil {
				setField(out, "aggregate", cols)
			}
			removeField(out, "aggregation_preset")
		}
	}
	return out, nil
}
func (r *resolver) steps(n *yaml.Node, stack map[string]bool) ([]Step, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("do must be a list")
	}
	var out []Step
	for i, node := range n.Content {
		if node.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("step %d must be a mapping", i+1)
		}
		copy := clone(node)
		ops := 0
		op := ""
		for j := 0; j < len(copy.Content); j += 2 {
			if copy.Content[j].Value != "enabled" {
				ops++
				op = copy.Content[j].Value
			}
		}
		if ops != 1 {
			return nil, fmt.Errorf("step %d requires exactly one operation", i+1)
		}
		known := false
		for _, v := range []string{"map", "filter", "forward_fill", "group", "aggregate", "sort", "deduplicate", "calculate", "transform", "join", "merge", "compare", "style", "pipeline"} {
			if op == v {
				known = true
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown operation %q", op)
		}
		resolved, err := r.resolve(op, field(copy, op), stack)
		if err != nil {
			return nil, fmt.Errorf("step %d %s: %w", i+1, op, err)
		}
		if op == "pipeline" {
			var spec struct {
				Do yaml.Node `yaml:"do"`
			}
			if err := strictDecode(resolved, &spec); err != nil {
				return nil, err
			} // Keep pipeline references active while expanding their nested steps.
			next := map[string]bool{}
			for k, v := range stack {
				next[k] = v
			}
			if p := field(field(copy, op), "preset"); p != nil {
				key := "pipelines." + p.Value
				if next[key] {
					return nil, fmt.Errorf("pipeline cycle at %s", key)
				}
				next[key] = true
			}
			nested, err := r.steps(&spec.Do, next)
			if err != nil {
				return nil, err
			}
			if enabled := field(copy, "enabled"); enabled != nil {
				var b *bool
				if err := enabled.Decode(&b); err != nil {
					return nil, err
				}
				if b != nil && !*b {
					for k := range nested {
						v := false
						nested[k].Enabled = &v
					}
				}
			}
			out = append(out, nested...)
		} else {
			setField(copy, op, resolved)
			var s Step
			if err := strictDecode(copy, &s); err != nil {
				return nil, fmt.Errorf("step %d: %w", i+1, err)
			}
			out = append(out, s)
		}
	}
	return out, nil
}
