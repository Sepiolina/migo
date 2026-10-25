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
// three helpers on top of its builtins (trim, upper, lower, replace, len) and
// operators (startsWith, contains, matches, ...):
//
//	col("Part Number")   the cell as text
//	num("Quantity")      the cell as a number (error if it is not one)
//	norm(s)              trimmed upper case, as BOM matching does
type rowExpr struct{ prog *vm.Program }

// rowEnv is built per row, so one compiled workflow can serve concurrent
// requests.
type rowEnv struct {
	Col  func(string) (string, error)  `expr:"col"`
	Num  func(string) (float64, error) `expr:"num"`
	Norm func(string) string           `expr:"norm"`
}

func compileRowExpr(src string, boolean bool) (*rowExpr, error) {
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("expression must not be blank")
	}
	opts := []expr.Option{expr.Env(rowEnv{})}
	if boolean {
		opts = append(opts, expr.AsBool())
	}
	prog, err := expr.Compile(src, opts...)
	if err != nil {
		return nil, fmt.Errorf("expression %q: %w", src, err)
	}
	return &rowExpr{prog}, nil
}

func (e *rowExpr) eval(t *Table, row []string, i int) (any, error) {
	col := func(name string) (string, error) {
		n, err := t.col(name)
		if err != nil {
			return "", fmt.Errorf("row %d: %w", i+1, err)
		}
		return row[n], nil
	}
	return expr.Run(e.prog, rowEnv{
		Col: col,
		Num: func(name string) (float64, error) {
			v, err := col(name)
			if err != nil {
				return 0, err
			}
			return numeric(v, i, name)
		},
		Norm: jsx.Norm,
	})
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
