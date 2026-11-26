package workflow

import (
	"fmt"
	"math"

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
