package workflow

import (
	"fmt"
	"sort"
	"strings"
)

// A workflow step describes itself: its place in the step library, its help
// and the fields of its form. Mai Studio builds the add-step menu and the
// inspector from these descriptors (through `migo describe`), and
// GET /api/workflows serves them, so a step is declared once, here in Go.

// StepInfo describes one step type.
type StepInfo struct {
	Op          string `json:"op"`          // the YAML key, e.g. "filter"
	Type        string `json:"type"`        // a stable id; default "mai_" + Op
	Category    string `json:"category"`    // one of Categories
	Icon        string `json:"icon"`        // one glyph for the step library
	Title       string `json:"title"`       // "Filter rows"
	Heading     string `json:"heading"`     // the form heading, "Keep rows where"
	Description string `json:"description"` // one line for the step library
	Help        string `json:"help"`        // guidance shown with the form
	// Form says how the YAML argument maps to Fields:
	//   map      op: {key: value, ...}
	//   entries  the argument itself is the map of the single entries field
	//   inline   the step node holds the keys (if / then / else)
	Form string `json:"form"`
	// Scalar and List name (by key) the field a short form fills:
	// `save: name` and `fill: [A, B]`. new(string) names the field with no key.
	Scalar *string     `json:"scalar,omitempty"`
	List   *string     `json:"list,omitempty"`
	Fields []FieldInfo `json:"fields"`
	// Example is the YAML argument a newly inserted step starts with. It must
	// compile (TestStepExamplesCompile).
	Example string `json:"example"`
	Service bool   `json:"service,omitempty"` // needs a connection; skipped in local runs
}

// FieldInfo describes one form field. Kind selects the editor:
//
//	text      plain text (Placeholder, Multiline)
//	template  text with {expression} parts (Multiline)
//	number    a number (Integer, Min)
//	bool      a checkbox
//	enum      one of Options (label, value); Fallback applies when absent
//	list      names separated by commas, written as a YAML list
//	expr      an expression; Scope is row, table or name
//	entries   a map edited as rows of the Item named (Rest: every other key)
//	items     a list of maps, each edited as the Item named
//	steps     a nested list of steps
//	yaml      any YAML value, written in flow style
type FieldInfo struct {
	Kind        string      `json:"kind"`
	Key         string      `json:"key"`   // the YAML key ("a.b" nests); "" is the argument itself
	Name        string      `json:"name"`  // unique within the form; default: Key in upper case
	Label       string      `json:"label"` // shown before the field
	Optional    bool        `json:"optional,omitempty"`
	Advanced    bool        `json:"advanced,omitempty"` // hidden until used or "Show all"
	Hint        string      `json:"hint,omitempty"`
	Virtual     bool        `json:"virtual,omitempty"` // UI only, no YAML key
	Placeholder string      `json:"placeholder,omitempty"`
	Multiline   bool        `json:"multiline,omitempty"`
	Integer     bool        `json:"integer,omitempty"`
	Min         *int        `json:"min,omitempty"`
	Options     [][2]string `json:"options,omitempty"`
	Fallback    string      `json:"fallback,omitempty"`
	Boolean     bool        `json:"boolean,omitempty"` // enum written as true/false
	Scope       string      `json:"scope,omitempty"`
	Condition   bool        `json:"condition,omitempty"` // expr that answers yes or no
	Item        string      `json:"item,omitempty"`
	Rest        bool        `json:"rest,omitempty"`
}

// ItemInfo describes a nested form: one key/value pair of an entries field
// (Shape "entry") or one map of an items field (Shape "object").
type ItemInfo struct {
	Type       string      `json:"type"`
	Category   string      `json:"category"`
	Shape      string      `json:"shape"`
	Check      string      `json:"check"`
	Title      string      `json:"title"`
	Heading    string      `json:"heading"`
	Help       string      `json:"help"`
	EntryKey   *FieldInfo  `json:"entryKey,omitempty"`
	EntryValue *FieldInfo  `json:"entryValue,omitempty"`
	Fields     []FieldInfo `json:"fields,omitempty"`
}

// FuncInfo describes one expression helper.
type FuncInfo struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Doc       string `json:"doc"`
}

// Catalog is everything an editor needs to offer and edit steps.
type Catalog struct {
	Steps     []StepInfo     `json:"steps"`
	Items     []ItemInfo     `json:"items"`
	Functions []FuncInfo     `json:"functions"`
	Templates []TemplateInfo `json:"templates"`
}

// Categories are the step library sections, in display order.
var Categories = []string{"Data", "Rows", "Columns", "Groups", "Control", "Services", "Output"}

var fieldKinds = map[string]bool{
	"text": true, "template": true, "number": true, "bool": true, "enum": true, "list": true,
	"expr": true, "entries": true, "items": true, "steps": true, "yaml": true,
}

var extraSteps []StepInfo

func short(key string) *string { return &key }
func atLeast(n int) *int       { return &n }

// DescribeStep adds the descriptor of a step registered with
// RegisterTableStep, so editors can offer it:
//
//	workflow.DescribeStep(workflow.StepInfo{
//		Op: "upper", Category: "Columns", Icon: "A", Title: "Upper-case columns",
//		Heading: "Upper-case", Description: "Upper-case text in columns.",
//		Help: "Column titles separated by commas.",
//		Form: "map", List: new(string),
//		Fields:  []workflow.FieldInfo{{Kind: "list", Name: "COLUMNS"}},
//		Example: "[Column]",
//	})
//
// An invalid or repeated descriptor panics.
func DescribeStep(info StepInfo) {
	for _, s := range append(append([]StepInfo{}, builtinSteps...), extraSteps...) {
		if s.Op == info.Op {
			panic(fmt.Sprintf("workflow: step %q is already described", info.Op))
		}
	}
	if err := checkStepInfo(info, itemTypes()); err != nil {
		panic("workflow: " + err.Error())
	}
	extraSteps = append(extraSteps, info)
}

// Describe returns the catalog: the built-in steps in library order, then
// the steps described from outside by name, with names and ids filled in.
func Describe() Catalog {
	c := Catalog{Functions: append([]FuncInfo{}, functions...), Templates: Templates()}
	extra := append([]StepInfo{}, extraSteps...)
	sort.Slice(extra, func(i, j int) bool { return extra[i].Op < extra[j].Op })
	for _, s := range append(append([]StepInfo{}, builtinSteps...), extra...) {
		if s.Type == "" {
			s.Type = "mai_" + s.Op
		}
		s.Fields = named(s.Fields)
		c.Steps = append(c.Steps, s)
	}
	for _, it := range builtinItems {
		it.Fields = named(it.Fields)
		if it.EntryKey != nil && it.EntryValue != nil {
			pair := named([]FieldInfo{*it.EntryKey, *it.EntryValue})
			it.EntryKey, it.EntryValue = &pair[0], &pair[1]
		}
		c.Items = append(c.Items, it)
	}
	return c
}

// named returns a copy of fields with default names filled in.
func named(fields []FieldInfo) []FieldInfo {
	if fields == nil {
		return nil
	}
	out := make([]FieldInfo, len(fields))
	for i, f := range fields {
		if f.Name == "" {
			f.Name = strings.ToUpper(f.Key)
			if f.Name == "" && f.Kind == "entries" {
				f.Name = "ENTRIES"
			}
		}
		out[i] = f
	}
	return out
}

func itemTypes() map[string]bool {
	types := map[string]bool{}
	for _, it := range builtinItems {
		types[it.Type] = true
	}
	return types
}

// checkStepInfo reports the first mistake in a descriptor.
func checkStepInfo(s StepInfo, items map[string]bool) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("step %q descriptor: %s", s.Op, fmt.Sprintf(format, a...))
	}
	if s.Op == "" || s.Title == "" || s.Description == "" {
		return bad("op, title and description are required")
	}
	if !contains(Categories, s.Category) {
		return bad("category %q is not one of %v", s.Category, Categories)
	}
	if s.Form != "map" && s.Form != "entries" && s.Form != "inline" {
		return bad("form %q is not map, entries or inline", s.Form)
	}
	if len(s.Fields) == 0 {
		return bad("at least one field is required")
	}
	if err := checkFields(s.Fields, items); err != nil {
		return bad("%v", err)
	}
	for _, key := range []*string{s.Scalar, s.List} {
		if key != nil && !hasKey(s.Fields, *key) {
			return bad("short form names the missing field %q", *key)
		}
	}
	return nil
}

func checkFields(fields []FieldInfo, items map[string]bool) error {
	seen := map[string]bool{}
	for _, f := range named(fields) {
		switch {
		case !fieldKinds[f.Kind]:
			return fmt.Errorf("field %q has unknown kind %q", f.Key, f.Kind)
		case f.Name == "" || seen[f.Name]:
			return fmt.Errorf("field %q needs a unique name (got %q)", f.Key, f.Name)
		case (f.Kind == "entries" || f.Kind == "items") && !items[f.Item]:
			return fmt.Errorf("field %q uses unknown item %q", f.Key, f.Item)
		case f.Kind == "enum" && len(f.Options) == 0:
			return fmt.Errorf("field %q needs options", f.Key)
		case f.Kind == "expr" && f.Scope != "row" && f.Scope != "table" && f.Scope != "name":
			return fmt.Errorf("field %q needs scope row, table or name", f.Key)
		}
		seen[f.Name] = true
	}
	return nil
}

func hasKey(fields []FieldInfo, key string) bool {
	for _, f := range fields {
		if f.Key == key && !f.Virtual {
			return true
		}
	}
	return false
}

// functions documents the expression helpers (expr.go), on top of expr's
// own built-ins. TestFunctionsDescribed keeps it in step with globalVars.
var functions = []FuncInfo{
	{"config", `config("key")`, "A config.json value."},
	{"norm", "norm(v)", "Text, trimmed and in upper case."},
	{"trim", "trim(v)", "Text without leading and trailing whitespace (JavaScript rules)."},
	{"str", "str(v)", "The value as text."},
	{"num", "num(v)", "The value as a number; an error if it is not one."},
	{"toNumber", "toNumber(v, d)", "JavaScript parseFloat with commas ignored; d when it is not a number."},
	{"parseFloat", "parseFloat(v)", "JavaScript parseFloat; nil when it is not a number."},
	{"blank", "blank(v)", "True for nil and for whitespace-only text."},
	{"truthy", "truthy(v)", "JavaScript truthiness."},
	{"coalesce", "coalesce(a, b, ...)", "The first truthy value."},
	{"fields", "fields(v)", "The whitespace-separated words of the text."},
	{"splitAny", `splitAny(v, ",;")`, "The pieces of the text between any of the characters."},
	{"replaceRe", `replaceRe(v, "^0+", "")`, "Replace every match of a regular expression."},
}
