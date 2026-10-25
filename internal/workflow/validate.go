package workflow

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseSource separates an input alias and quoted sheet from a source reference.
// Plain dataset references return empty alias and sheet.
func ParseSource(source string) (alias, sheet string, err error) {
	if strings.TrimSpace(source) == "" {
		return "", "", fmt.Errorf("source cannot be blank")
	}
	i := strings.IndexByte(source, '[')
	if i < 0 {
		if strings.ContainsAny(source, "]\"") {
			return "", "", fmt.Errorf("invalid source %q", source)
		}
		return "", "", nil
	}
	if i == 0 || !strings.HasSuffix(source, "]") {
		return "", "", fmt.Errorf("invalid source %q", source)
	}
	alias = source[:i]
	quoted := source[i+1 : len(source)-1]
	if !strings.HasPrefix(quoted, "\"") {
		return "", "", fmt.Errorf("source sheet must be double quoted: %q", source)
	}
	sheet, err = strconv.Unquote(quoted)
	if err != nil || strings.TrimSpace(sheet) == "" {
		return "", "", fmt.Errorf("invalid source sheet in %q", source)
	}
	if !nonblank(alias) || strings.TrimSpace(alias) != alias || strings.ContainsAny(alias, "[]\"") {
		return "", "", fmt.Errorf("invalid input alias in %q", source)
	}
	return alias, sheet, nil
}

// StepName returns the selected operation name (empty for an invalid empty step).
func StepName(s Step) string {
	for _, p := range stepOperations(s) {
		if p.present {
			return p.name
		}
	}
	return ""
}

type stepOperation struct {
	name    string
	present bool
}

func stepOperations(s Step) []stepOperation {
	return []stepOperation{{"map", s.Map != nil}, {"filter", s.Filter != nil}, {"forward_fill", s.ForwardFill != nil}, {"group", s.Group != nil}, {"aggregate", s.Aggregate != nil}, {"sort", s.Sort != nil}, {"deduplicate", s.Deduplicate != nil}, {"calculate", s.Calculate != nil}, {"transform", s.Transform != nil}, {"join", s.Join != nil}, {"merge", s.Merge != nil}, {"compare", s.Compare != nil}, {"style", s.Style != nil}}
}
func nonblank(s string) bool { return strings.TrimSpace(s) != "" }
func columns(c []string) error {
	if len(c) == 0 {
		return fmt.Errorf("columns must not be empty")
	}
	seen := map[string]bool{}
	for _, s := range c {
		if !nonblank(s) {
			return fmt.Errorf("column must not be blank")
		}
		if strings.HasPrefix(s, "@") {
			for _, r := range s[1:] {
				if r < 'A' || r > 'Z' {
					return fmt.Errorf("invalid column index %q", s)
				}
			}
			if len(s) == 1 {
				return fmt.Errorf("invalid column index %q", s)
			}
		}
		if seen[s] {
			return fmt.Errorf("duplicate column %q", s)
		}
		seen[s] = true
	}
	return nil
}
func enum(value string, options ...string) error {
	for _, o := range options {
		if value == o {
			return nil
		}
	}
	return fmt.Errorf("unsupported value %q (expected %s)", value, strings.Join(options, ", "))
}
func aggregation(cols map[string]string) error {
	if len(cols) == 0 {
		return fmt.Errorf("aggregation columns must not be empty")
	}
	for c, f := range cols {
		if err := columns([]string{c}); err != nil {
			return err
		}
		if err := enum(f, "sum", "first", "last", "min", "max", "count", "mean", "join"); err != nil {
			return err
		}
	}
	return nil
}
func validateFilter(f FilterSpec) error {
	predicates := 0
	for _, yes := range []bool{f.Equal != nil, f.NotEqual != nil, f.GreaterThan != nil, f.GreaterOrEqual != nil, f.LessThan != nil, f.LessOrEqual != nil, f.Contains != nil, f.In != nil, f.Blank != nil} {
		if yes {
			predicates++
		}
	}
	modes := 0
	if f.All != nil {
		modes++
	}
	if f.Any != nil {
		modes++
	}
	if f.Column != "" || predicates > 0 {
		modes++
	}
	if modes != 1 {
		return fmt.Errorf("filter requires exactly one of all, any, or a column predicate")
	}
	if f.All != nil || f.Any != nil {
		children := f.All
		if f.Any != nil {
			children = f.Any
		}
		if len(children) == 0 {
			return fmt.Errorf("filter all/any must not be empty")
		}
		for _, c := range children {
			if err := validateFilter(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := columns([]string{f.Column}); err != nil {
		return err
	}
	if predicates == 0 {
		return fmt.Errorf("filter requires a predicate")
	}
	if f.In != nil && len(f.In) == 0 {
		return fmt.Errorf("filter in must not be empty")
	}
	for _, p := range []*float64{f.GreaterThan, f.GreaterOrEqual, f.LessThan, f.LessOrEqual} {
		if p != nil && (math.IsNaN(*p) || math.IsInf(*p, 0)) {
			return fmt.Errorf("filter numeric predicate must be finite")
		}
	}
	return nil
}
func validateStep(s Step) error {
	count := 0
	for _, p := range stepOperations(s) {
		if p.present {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("step requires exactly one operation")
	}
	switch {
	case s.Map != nil:
		if len(s.Map.Columns) == 0 {
			return fmt.Errorf("map columns must not be empty")
		}
		targets := map[string]bool{}
		for _, m := range s.Map.Columns {
			if err := columns([]string{m.From}); err != nil {
				return err
			}
			if !nonblank(m.To) {
				return fmt.Errorf("mapping to must not be blank")
			}
			if targets[m.To] {
				return fmt.Errorf("duplicate mapping target %q", m.To)
			}
			targets[m.To] = true
		}
	case s.Filter != nil:
		return validateFilter(*s.Filter)
	case s.ForwardFill != nil:
		return columns(s.ForwardFill.Columns)
	case s.Deduplicate != nil:
		return columns(s.Deduplicate.Columns)
	case s.Group != nil:
		if err := columns(s.Group.By); err != nil {
			return err
		}
		if len(s.Group.Aggregate) > 0 {
			return aggregation(s.Group.Aggregate)
		}
	case s.Aggregate != nil:
		return aggregation(s.Aggregate.Columns)
	case s.Sort != nil:
		if err := columns(s.Sort.By); err != nil {
			return err
		}
		return enum(s.Sort.Order, "", "ascending", "descending")
	case s.Calculate != nil:
		c := s.Calculate
		if err := columns([]string{c.Column}); err != nil {
			return err
		}
		if err := columns(c.Columns); err != nil {
			return err
		}
		if c.Value != nil && (math.IsNaN(*c.Value) || math.IsInf(*c.Value, 0)) {
			return fmt.Errorf("calculation value must be finite")
		}
		return enum(c.Operation, "add", "subtract", "multiply", "divide")
	case s.Transform != nil:
		if err := columns(s.Transform.Columns); err != nil {
			return err
		}
		return enum(s.Transform.Operation, "trim", "upper", "lower", "replace", "normalize")
	case s.Join != nil:
		j := s.Join
		if !nonblank(j.With) {
			return fmt.Errorf("join with is required")
		}
		if err := columns(j.On); err != nil {
			return err
		}
		if j.RightOn != nil {
			if err := columns(j.RightOn); err != nil {
				return err
			}
			if len(j.On) != len(j.RightOn) {
				return fmt.Errorf("join on and right_on lengths differ")
			}
		}
		return enum(j.How, "", "left", "inner")
	case s.Merge != nil:
		if len(s.Merge.With) == 0 {
			return fmt.Errorf("merge with must not be empty")
		}
		for _, ref := range s.Merge.With {
			if !nonblank(ref) {
				return fmt.Errorf("merge dataset cannot be blank")
			}
		}
	case s.Compare != nil:
		if !nonblank(s.Compare.With) {
			return fmt.Errorf("compare with is required")
		}
		if err := columns(s.Compare.By); err != nil {
			return err
		}
		if s.Compare.Columns != nil {
			return columns(s.Compare.Columns)
		}
	case s.Style != nil:
		for c := range s.Style.NumberFormats {
			if err := columns([]string{c}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Validate checks structural properties and ordered dataset references before I/O.
func Validate(c *Config) error {
	if c == nil {
		return fmt.Errorf("workflow config is nil")
	}
	if len(c.Input) == 0 {
		return fmt.Errorf("input must not be empty")
	}
	for alias, path := range c.Input {
		if !nonblank(alias) || strings.TrimSpace(alias) != alias || strings.ContainsAny(alias, "[]\"") {
			return fmt.Errorf("invalid input alias %q", alias)
		}
		if !nonblank(path) {
			return fmt.Errorf("input %s path is blank", alias)
		}
	}
	datasets := map[string]bool{}
	if len(c.Process) == 0 {
		return fmt.Errorf("process must not be empty")
	}
	for i, p := range c.Process {
		ctx := fmt.Sprintf("process %d (%s)", i+1, p.As)
		alias, _, err := ParseSource(p.From)
		if err != nil {
			return fmt.Errorf("%s: %w", ctx, err)
		}
		if alias != "" {
			if _, ok := c.Input[alias]; !ok {
				return fmt.Errorf("%s: unknown input %q", ctx, alias)
			}
		} else if !datasets[p.From] {
			return fmt.Errorf("%s: unknown or forward dataset %q", ctx, p.From)
		}
		if !nonblank(p.As) || strings.TrimSpace(p.As) != p.As || strings.ContainsAny(p.As, "[]\"") {
			return fmt.Errorf("%s: invalid dataset name", ctx)
		}
		if datasets[p.As] {
			return fmt.Errorf("%s: duplicate dataset name", ctx)
		}
		if _, ok := c.Input[p.As]; ok {
			return fmt.Errorf("%s: dataset collides with input alias", ctx)
		}
		if p.HeaderRow < 0 {
			return fmt.Errorf("%s: header_row must be positive", ctx)
		}
		for j, s := range p.Do {
			if err := validateStep(s); err != nil {
				return fmt.Errorf("%s step %d %s: %w", ctx, j+1, StepName(s), err)
			}
			if s.Enabled != nil && !*s.Enabled {
				continue
			}
			var refs []string
			if s.Join != nil {
				refs = append(refs, s.Join.With)
			}
			if s.Merge != nil {
				refs = append(refs, s.Merge.With...)
			}
			if s.Compare != nil {
				refs = append(refs, s.Compare.With)
			}
			for _, ref := range refs {
				if !datasets[ref] {
					return fmt.Errorf("%s step %d: unknown or forward dataset %q", ctx, j+1, ref)
				}
			}
		}
		datasets[p.As] = true
	}
	if len(c.Output) == 0 {
		return fmt.Errorf("output must not be empty")
	}
	files := map[string]bool{}
	for i, o := range c.Output {
		if !nonblank(o.File) || !strings.EqualFold(filepath.Ext(o.File), ".xlsx") {
			return fmt.Errorf("output %d file must have .xlsx extension", i+1)
		}
		path := o.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(c.BaseDir, path)
		}
		path = filepath.Clean(path)
		if files[path] {
			return fmt.Errorf("duplicate output file %q", o.File)
		}
		files[path] = true
		if len(o.Sheets) == 0 {
			return fmt.Errorf("output %d sheets must not be empty", i+1)
		}
		names := map[string]bool{}
		for name, ref := range o.Sheets {
			if !nonblank(name) || len([]rune(name)) > 31 || strings.ContainsAny(name, "[]:*?/\\") || strings.HasPrefix(name, "'") || strings.HasSuffix(name, "'") {
				return fmt.Errorf("invalid output sheet name %q", name)
			}
			fold := strings.ToLower(name)
			if names[fold] {
				return fmt.Errorf("duplicate output sheet name %q", name)
			}
			names[fold] = true
			if !datasets[ref] {
				return fmt.Errorf("output sheet %q: unknown dataset %q", name, ref)
			}
		}
	}
	return nil
}
