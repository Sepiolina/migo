package workflow

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"

	"miservice/internal/jsx"
)

// Expressions are expr-lang (https://expr-lang.org/docs/language-definition).
// In a row expression every column is a variable (`Qty`, or `col("Part
// Number")` for names that are not identifiers). Helpers on top of the
// builtins:
//
//	norm(v)            trimmed upper case text
//	trim(v), str(v)    trimmed text; text (numbers as JavaScript prints them)
//	num(v)             a number; error if v is not one
//	toNumber(v, d)     lenient: JavaScript parseFloat (commas ignored), else d
//	parseFloat(v)      JavaScript parseFloat, nil when not a number
//	blank(v)           nil or whitespace only
//	truthy(v)          JavaScript truthiness (for config flags and JSON values)
//	coalesce(a, b...)  the first truthy value, else nil
//	fields(v)          whitespace-separated words
//	splitAny(v, chars) the non-empty pieces between any of chars
//	replaceRe(v, re, s) regexp replace
//	config(key)        a config.json value
type program struct {
	src  string
	prog *vm.Program
	refs []string // identifiers that must be columns (row expressions)
}

// Variables that are not columns, per kind of expression.
var (
	rowVars    = []string{"index", "size", "group", "prev", "col"}
	tableVars  = []string{"rows", "table", "sheets"}
	globalVars = []string{"config", "norm", "trim", "str", "num", "toNumber", "parseFloat", "blank", "truthy", "coalesce", "fields", "splitAny", "replaceRe"}
)

var reCache sync.Map

var helpers = []expr.Option{
	expr.DisableBuiltin("trim"),
	expr.Function("norm", func(p ...any) (any, error) { return jsx.Norm(text(p[0])), nil }, new(func(any) string)),
	expr.Function("trim", func(p ...any) (any, error) { return jsx.Trim(text(p[0])), nil }, new(func(any) string)),
	expr.Function("str", func(p ...any) (any, error) { return text(p[0]), nil }, new(func(any) string)),
	expr.Function("blank", func(p ...any) (any, error) { return blank(p[0]), nil }, new(func(any) bool)),
	expr.Function("num", func(p ...any) (any, error) {
		v, err := numeric(cell(p[0]), -1, "")
		if err != nil {
			return nil, fmt.Errorf("num: %q is not a number", text(p[0]))
		}
		return v, nil
	}, new(func(any) float64)),
	expr.Function("toNumber", func(p ...any) (any, error) {
		return toNumber(cell(p[0]), toNumber(cell(p[1]), 0)), nil
	}, new(func(any, any) float64)),
	expr.Function("parseFloat", func(p ...any) (any, error) {
		if f, ok := p[0].(float64); ok {
			return f, nil
		}
		if f, ok := jsx.ParseFloat(text(cell(p[0]))); ok {
			return f, nil
		}
		return nil, nil
	}, new(func(any) any)),
	expr.Function("truthy", func(p ...any) (any, error) { return jsx.Truthy(cell(p[0])), nil }, new(func(any) bool)),
	expr.Function("splitAny", func(p ...any) (any, error) {
		seps := p[1].(string)
		var out []any
		for _, f := range strings.FieldsFunc(text(p[0]), func(r rune) bool { return strings.ContainsRune(seps, r) }) {
			out = append(out, f)
		}
		return out, nil
	}, new(func(any, string) []any)),
	expr.Function("coalesce", func(p ...any) (any, error) {
		for _, v := range p {
			if jsx.Truthy(cell(v)) {
				return v, nil
			}
		}
		return nil, nil
	}),
	expr.Function("fields", func(p ...any) (any, error) {
		var out []any
		for _, f := range strings.FieldsFunc(text(p[0]), jsx.IsSpace) {
			out = append(out, f)
		}
		return out, nil
	}, new(func(any) []any)),
	expr.Function("replaceRe", func(p ...any) (any, error) {
		re, err := cachedRe(p[1].(string))
		if err != nil {
			return nil, err
		}
		return re.ReplaceAllString(text(p[0]), p[2].(string)), nil
	}, new(func(any, string, string) string)),
}

func cachedRe(pattern string) (*regexp.Regexp, error) {
	if re, ok := reCache.Load(pattern); ok {
		return re.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	reCache.Store(pattern, re)
	return re, nil
}

// compileExpr compiles src. vars are the context variables it may use; with
// columns, any other identifier is a column (checked when the table is
// known), otherwise it is an error now.
func compileExpr(src string, columns bool, vars ...string) (*program, error) {
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("expression must not be blank")
	}
	opts := append([]expr.Option{expr.Env(map[string]any{}), expr.AllowUndefinedVariables()}, helpers...)
	p, err := expr.Compile(src, opts...)
	if err != nil {
		return nil, fmt.Errorf("expression %q: %w", src, err)
	}
	out := &program{src: src, prog: p}
	known := map[string]bool{"config": true, "$env": true}
	for _, v := range append(vars, globalVars...) {
		known[v] = true
	}
	node := p.Node()
	v := &identVisitor{lets: map[string]bool{}}
	ast.Walk(&node, v)
	for _, id := range v.idents {
		switch {
		case known[id] || v.lets[id]:
		case columns:
			out.refs = append(out.refs, id)
		default:
			return nil, fmt.Errorf("expression %q: unknown name %q", src, id)
		}
	}
	return out, nil
}

type identVisitor struct {
	idents []string
	lets   map[string]bool
}

func (v *identVisitor) Visit(n *ast.Node) {
	switch x := (*n).(type) {
	case *ast.VariableDeclaratorNode:
		v.lets[x.Name] = true
	case *ast.IdentifierNode:
		if !contains(v.idents, x.Value) {
			v.idents = append(v.idents, x.Value)
		}
	}
}

// check reports a referenced column the table does not have.
func (p *program) check(t *Table) error {
	for _, r := range p.refs {
		if t.index(r) < 0 {
			return fmt.Errorf("expression %q: unknown column %q", p.src, r)
		}
	}
	return nil
}

func (p *program) run(env map[string]any) (any, error) {
	v, err := expr.Run(p.prog, env)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p.src, err)
	}
	return v, nil
}

func (p *program) test(env map[string]any) (bool, error) {
	v, err := p.run(env)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s: want true or false, got %q", p.src, text(cell(v)))
	}
	return b, nil
}

// rowEnv is the environment of one row: its columns plus extras.
func rowEnv(t *Table, i int, extra map[string]any) map[string]any {
	env := make(map[string]any, len(t.Columns)+len(extra)+4)
	for c, name := range t.Columns {
		env[name] = t.Rows[i][c]
	}
	env["index"] = i
	env["col"] = func(name string) (any, error) {
		n, err := t.col(name)
		if err != nil {
			return nil, err
		}
		return t.Rows[i][n], nil
	}
	env["prev"] = func(name string) (any, error) {
		n, err := t.col(name)
		if err != nil || i == 0 {
			return nil, err
		}
		return t.Rows[i-1][n], nil
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

// rowMaps is a table as a list of column -> value maps.
func rowMaps(t *Table, rows []int) []any {
	out := make([]any, len(rows))
	for i, r := range rows {
		m := make(map[string]any, len(t.Columns))
		for c, name := range t.Columns {
			m[name] = t.Rows[r][c]
		}
		out[i] = m
	}
	return out
}

func allRows(t *Table) []int {
	ix := make([]int, len(t.Rows))
	for i := range ix {
		ix[i] = i
	}
	return ix
}

// template is text with {expression} parts, e.g. "Loaded {count} rows".
type template struct {
	lits  []string
	progs []*program
}

func compileTemplate(src string, columns bool, vars ...string) (*template, error) {
	t := &template{}
	rest := src
	for {
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			t.lits = append(t.lits, rest)
			return t, nil
		}
		j := closingBrace(rest[i:])
		if j < 0 {
			return nil, fmt.Errorf("template %q: unclosed {", src)
		}
		p, err := compileExpr(rest[i+1:i+j], columns, vars...)
		if err != nil {
			return nil, fmt.Errorf("template %q: %w", src, err)
		}
		t.lits = append(t.lits, rest[:i])
		t.progs = append(t.progs, p)
		rest = rest[i+j+1:]
	}
}

// closingBrace returns the index of the } closing the { at s[0], skipping
// nested braces and quoted strings; -1 if there is none.
func closingBrace(s string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote != '`' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func (t *template) render(env map[string]any) (string, error) {
	var b strings.Builder
	for i, lit := range t.lits {
		b.WriteString(lit)
		if i < len(t.progs) {
			v, err := t.progs[i].run(env)
			if err != nil {
				return "", err
			}
			b.WriteString(text(cell(v)))
		}
	}
	return b.String(), nil
}
