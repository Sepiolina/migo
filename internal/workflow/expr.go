package workflow

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"miservice/internal/jsx"
)

// Row expressions (filter, set) are expr-lang (https://expr-lang.org) with
// three helpers on top of its builtins (trim, upper, lower, replace,
// startsWith, contains, matches, ...):
//
//	col("Part Number")   the cell as text
//	num("Quantity")      the cell as a number (error if it is not one)
//	norm(s)              trimmed upper case, as BOM matching does
type rowExpr struct {
	prog *vm.Program
	cur  *rowCursor
}

type rowCursor struct {
	t   *Table
	row []string
	i   int
}

func compileRowExpr(src string, boolean bool) (*rowExpr, error) {
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("expression must not be blank")
	}
	cur := &rowCursor{}
	cell := func(name string) (string, error) {
		n, err := cur.t.col(name)
		if err != nil {
			return "", fmt.Errorf("row %d: %w", cur.i+1, err)
		}
		return cur.row[n], nil
	}
	opts := []expr.Option{
		expr.Env(map[string]any{}),
		expr.Function("col", func(p ...any) (any, error) { return cell(p[0].(string)) }, new(func(string) string)),
		expr.Function("num", func(p ...any) (any, error) {
			v, err := cell(p[0].(string))
			if err != nil {
				return nil, err
			}
			return numeric(v, cur.i, p[0].(string))
		}, new(func(string) float64)),
		expr.Function("norm", func(p ...any) (any, error) { return jsx.Norm(p[0].(string)), nil }, new(func(string) string)),
	}
	if boolean {
		opts = append(opts, expr.AsBool())
	}
	prog, err := expr.Compile(src, opts...)
	if err != nil {
		return nil, fmt.Errorf("expression %q: %w", src, err)
	}
	return &rowExpr{prog, cur}, nil
}

func (e *rowExpr) eval(t *Table, row []string, i int) (any, error) {
	e.cur.t, e.cur.row, e.cur.i = t, row, i
	return expr.Run(e.prog, map[string]any{})
}

// toText renders an expression result as a cell value.
func toText(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case float64:
		return number(x)
	}
	return fmt.Sprint(v), nil
}
