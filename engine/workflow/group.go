package workflow

import (
	"fmt"
	"math"
	"strings"

	"gopkg.in/yaml.v3"
)

// groups returns the rows of t matching where (all when nil), bucketed by
// the by columns in first-seen order. Without by there is one group.
func groups(s *State, t *Table, by []string, where *program) ([][]int, error) {
	ix, err := t.cols(by)
	if err != nil {
		return nil, err
	}
	if where != nil {
		if err := where.check(t); err != nil {
			return nil, err
		}
	}
	var out [][]int
	pos := map[string]int{}
	for i := range t.Rows {
		if where != nil {
			ok, err := where.test(rowEnv(t, i, s.vars()))
			if err != nil {
				return nil, fmt.Errorf("row %d: %w", i+1, err)
			}
			if !ok {
				continue
			}
		}
		k := key(t.Rows[i], ix, false)
		g, seen := pos[k]
		if !seen {
			g = len(out)
			pos[k] = g
			out = append(out, nil)
		}
		out[g] = append(out[g], i)
	}
	return out, nil
}

var aggregates = []string{"sum", "count", "mean", "min", "max", "first", "last", "join"}

// - group: {by: [Key], sum: [Qty], first: [Desc], ...}: one row per key.
// Without by, the whole table becomes one row.
func compileGroup(arg *yaml.Node, _ *scope) (tableFn, error) {
	var spec map[string][]string
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	ops := map[string]string{}
	for op, columns := range spec {
		if op == "by" {
			continue
		}
		if !contains(aggregates, op) {
			return nil, fmt.Errorf("unknown aggregate %q (use by, %s)", op, strings.Join(aggregates, ", "))
		}
		for _, c := range columns {
			if _, dup := ops[c]; dup {
				return nil, fmt.Errorf("column %q aggregated twice", c)
			}
			ops[c] = op
		}
	}
	for _, c := range spec["by"] {
		if _, dup := ops[c]; dup {
			return nil, fmt.Errorf("column %q is both a key and aggregated", c)
		}
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("names at least one aggregated column")
	}
	return func(t *Table, _ *State) (*Table, error) {
		keys, err := t.cols(spec["by"])
		if err != nil {
			return nil, err
		}
		for c := range ops {
			if _, err := t.col(c); err != nil {
				return nil, err
			}
		}
		out := &Table{}
		for _, n := range keys {
			out.Columns = append(out.Columns, t.Columns[n])
		}
		var agg []int // aggregated columns, in table order
		for i, c := range t.Columns {
			if _, ok := ops[c]; ok {
				agg = append(agg, i)
				out.Columns = append(out.Columns, c)
			}
		}
		gs, err := groups(nil, t, spec["by"], nil)
		if err != nil {
			return nil, err
		}
		if len(keys) == 0 && len(gs) == 0 {
			gs = [][]int{nil}
		}
		for _, g := range gs {
			row := make([]any, 0, len(out.Columns))
			for _, n := range keys {
				row = append(row, t.Rows[g[0]][n])
			}
			for _, n := range agg {
				v, err := aggregate(t, g, n, ops[t.Columns[n]])
				if err != nil {
					return nil, err
				}
				row = append(row, v)
			}
			out.Rows = append(out.Rows, row)
		}
		return out, nil
	}, nil
}

func aggregate(t *Table, rows []int, n int, op string) (any, error) {
	switch {
	case op == "count":
		return float64(len(rows)), nil
	case len(rows) == 0 && op == "sum":
		return 0.0, nil
	case len(rows) == 0:
		return nil, nil
	case op == "first":
		return t.Rows[rows[0]][n], nil
	case op == "last":
		return t.Rows[rows[len(rows)-1]][n], nil
	case op == "join":
		seen := map[string]bool{}
		var values []string
		for _, r := range rows {
			if v := text(t.Rows[r][n]); strings.TrimSpace(v) != "" && !seen[v] {
				seen[v] = true
				values = append(values, v)
			}
		}
		return strings.Join(values, ", "), nil
	}
	v := 0.0
	for i, r := range rows {
		x, err := numeric(t.Rows[r][n], r, t.Columns[n])
		if err != nil {
			return nil, err
		}
		switch {
		case op == "sum" || op == "mean":
			v += x
		case op == "min" && (i == 0 || x < v), op == "max" && (i == 0 || x > v):
			v = x
		}
	}
	if op == "mean" {
		v /= float64(len(rows))
	}
	return v, nil
}

// - window: {by: [Key], where: '<row condition>', set: {Col: '<expression>'}}
// computes columns per group, keeping every row. Besides the row's own
// columns an expression sees group (the group's rows, as they were before
// this column), index (position in the group, from 0) and size. Rows outside
// where keep their values.
func compileWindow(arg *yaml.Node, _ *scope) (tableFn, error) {
	var spec struct {
		By    []string  `yaml:"by"`
		Where string    `yaml:"where"`
		Set   yaml.Node `yaml:"set"`
	}
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	pairs, err := orderedPairs(&spec.Set)
	if err != nil {
		return nil, fmt.Errorf("set: %w", err)
	}
	var where *program
	if spec.Where != "" {
		if where, err = compileExpr(spec.Where, true, rowVars...); err != nil {
			return nil, err
		}
	}
	progs := make([]*program, len(pairs))
	for i, p := range pairs {
		if progs[i], err = compileExpr(p[1], true, rowVars...); err != nil {
			return nil, fmt.Errorf("%s: %w", p[0], err)
		}
	}
	return func(t *Table, s *State) (*Table, error) {
		gs, err := groups(s, t, spec.By, where)
		if err != nil {
			return nil, err
		}
		for k, p := range pairs {
			dest := t.addColumn(p[0])
			if err := progs[k].check(t); err != nil {
				return nil, err
			}
			vals := map[int]any{}
			for _, g := range gs {
				extra := map[string]any{"group": rowMaps(t, g), "size": len(g), "config": s.config}
				for pos, i := range g {
					extra["index"] = pos
					v, err := progs[k].run(rowEnv(t, i, extra))
					if err != nil {
						return nil, fmt.Errorf("row %d column %q: %w", i+1, p[0], err)
					}
					vals[i] = cell(v)
				}
			}
			for i, v := range vals {
				t.Rows[i][dest] = v
			}
		}
		return t, nil
	}, nil
}

// - pick: chooses one row per group by ordered rules.
//
//	pick:
//	  by: [Group]
//	  where: Eligible                 # rows that take part
//	  rules:                          # the first rule that applies decides
//	    - rows: 'Priority == 1'       # the rule's candidates
//	      when: 'size == 1'           # default size > 0; sees size (rows matched), best, tied
//	      best: Stock                 # optional: highest value wins (ties: first row)
//	      reason: "Single P1 ({size})"    # text with {expressions}; sees chosen too
//	  mark: IsMain                    # true on the chosen row, false on the others
//	  reason: Reason                  # column receiving the chosen rule's reason
func compilePick(arg *yaml.Node, _ *scope) (tableFn, error) {
	var spec struct {
		By     []string `yaml:"by"`
		Where  string   `yaml:"where"`
		Mark   string   `yaml:"mark"`
		Reason string   `yaml:"reason"`
		Rules  []struct {
			Rows   string `yaml:"rows"`
			When   string `yaml:"when"`
			Best   string `yaml:"best"`
			Reason string `yaml:"reason"`
		} `yaml:"rules"`
	}
	if err := strictDecode(arg, &spec); err != nil {
		return nil, err
	}
	if spec.Mark == "" || len(spec.Rules) == 0 {
		return nil, fmt.Errorf("needs mark and rules")
	}
	type rule struct {
		rows, when, best *program
		reason           *template
	}
	var err error
	var where *program
	if spec.Where != "" {
		if where, err = compileExpr(spec.Where, true, rowVars...); err != nil {
			return nil, err
		}
	}
	rules := make([]rule, len(spec.Rules))
	for i, r := range spec.Rules {
		ru := &rules[i]
		if ru.rows, err = compileExpr(r.Rows, true, rowVars...); err != nil {
			return nil, fmt.Errorf("rule %d rows: %w", i+1, err)
		}
		when := r.When
		if when == "" {
			when = "size > 0"
		}
		if ru.when, err = compileExpr(when, false, "size", "best", "tied"); err != nil {
			return nil, fmt.Errorf("rule %d when: %w", i+1, err)
		}
		if r.Best != "" {
			if ru.best, err = compileExpr(r.Best, true, rowVars...); err != nil {
				return nil, fmt.Errorf("rule %d best: %w", i+1, err)
			}
		}
		if spec.Reason != "" {
			if ru.reason, err = compileTemplate(r.Reason, false, "size", "best", "tied", "chosen"); err != nil {
				return nil, fmt.Errorf("rule %d reason: %w", i+1, err)
			}
		}
	}
	return func(t *Table, s *State) (*Table, error) {
		gs, err := groups(s, t, spec.By, where)
		if err != nil {
			return nil, err
		}
		mark := t.addColumn(spec.Mark)
		for _, r := range t.Rows {
			r[mark] = false
		}
		reasonCol := -1
		if spec.Reason != "" {
			reasonCol = t.addColumn(spec.Reason)
		}
		for _, ru := range rules {
			for _, p := range []*program{ru.rows, ru.best} {
				if p != nil {
					if err := p.check(t); err != nil {
						return nil, err
					}
				}
			}
		}
		for _, g := range gs {
			for _, ru := range rules {
				var cand []int
				for _, i := range g {
					ok, err := ru.rows.test(rowEnv(t, i, s.vars()))
					if err != nil {
						return nil, fmt.Errorf("row %d: %w", i+1, err)
					}
					if ok {
						cand = append(cand, i)
					}
				}
				env := map[string]any{"size": len(cand), "best": nil, "tied": 0, "config": s.config}
				chosen := -1
				if len(cand) > 0 {
					chosen = cand[0]
				}
				if ru.best != nil && len(cand) > 0 {
					best, tied := math.Inf(-1), 0
					for _, i := range cand {
						v, err := ru.best.run(rowEnv(t, i, s.vars()))
						if err != nil {
							return nil, fmt.Errorf("row %d: %w", i+1, err)
						}
						x := toNumber(cell(v), 0)
						switch {
						case x > best:
							best, tied, chosen = x, 1, i
						case x == best:
							tied++
						}
					}
					env["best"], env["tied"] = best, tied
				}
				ok, err := ru.when.test(env)
				if err != nil {
					return nil, err
				}
				if !ok || chosen < 0 {
					continue
				}
				t.Rows[chosen][mark] = true
				if ru.reason != nil {
					env["chosen"] = rowMaps(t, []int{chosen})[0]
					reason, err := ru.reason.render(env)
					if err != nil {
						return nil, err
					}
					t.Rows[chosen][reasonCol] = reason
				}
				break
			}
		}
		return t, nil
	}, nil
}
