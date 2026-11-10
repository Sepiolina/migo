package workflow

import (
	"context"
	"fmt"
	"math"
	"time"

	"miservice/internal/config"
)

// StepTrace is the current table after one top-level step, as an editor
// shows it: a column profile for every column, rows only for the step asked.
type StepTrace struct {
	Step     int             `json:"step"` // 0-based top-level step index
	Name     string          `json:"name"` // the step's name, e.g. "filter"
	Line     int             `json:"line"`
	Skipped  bool            `json:"skipped,omitempty"`
	Columns  []ColumnProfile `json:"columns"`
	Total    int             `json:"total"`          // rows in the current table
	Rows     [][]string      `json:"rows,omitempty"` // the first rows, for the focused step
	Log      []string        `json:"log,omitempty"`  // lines this step logged
	Error    string          `json:"error,omitempty"`
	Duration float64         `json:"ms"`
}

// ColumnProfile summarizes one column of the current table.
type ColumnProfile struct {
	Name     string  `json:"name"`
	Kind     string  `json:"kind"` // number, text, bool, empty or mixed
	Filled   int     `json:"filled"`
	Distinct int     `json:"distinct"` // capped at maxDistinct
	Min      float64 `json:"min,omitempty"`
	Max      float64 `json:"max,omitempty"`
	Bins     []int   `json:"bins,omitempty"` // a histogram of numbers
}

// TraceOptions choose what Trace returns.
type TraceOptions struct {
	Focus int   // the step whose rows are returned (-1: none)
	Limit int   // rows returned for the focused step
	Skip  []int // top-level steps to leave out, to preview the workflow without them
}

const maxDistinct = 1000

// Trace runs the top-level steps one at a time and records the current table
// after each. It stops at the first failing step, which carries the error;
// later steps are not returned. Nothing is written.
func (wf *Workflow) Trace(ctx context.Context, rt Runtime, inputs map[string]string, opt TraceOptions) ([]StepTrace, error) {
	if rt.Cfg == nil {
		rt.Cfg = &config.Config{}
	}
	var lines []string
	outer := rt.Log
	rt.Log = func(line string) {
		lines = append(lines, line)
		outer.Print(line)
	}
	for _, in := range wf.Inputs {
		if inputs[in.Name] == "" {
			return nil, fmt.Errorf("missing input %q", in.Name)
		}
	}
	skip := map[int]bool{}
	for _, i := range opt.Skip {
		skip[i] = true
	}
	s := &State{rt: rt, Inputs: inputs, Tables: map[string]*Table{}}
	var out []StepTrace
	for i, st := range wf.steps {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		tr := StepTrace{Step: i, Name: st.name, Line: st.line, Columns: []ColumnProfile{}}
		lines = lines[:0]
		start := time.Now()
		if skip[i] {
			tr.Skipped = true
		} else if err := st.run(ctx, s); err != nil {
			tr.Error = err.Error()
		}
		tr.Duration = float64(time.Since(start).Microseconds()) / 1000
		tr.Log = append([]string(nil), lines...)
		if s.Cur != nil {
			tr.Total = len(s.Cur.Rows)
			tr.Columns = profile(s.Cur)
			if i == opt.Focus {
				tr.Rows = head(s.Cur, opt.Limit)
			}
		}
		out = append(out, tr)
		if tr.Error != "" {
			break
		}
	}
	return out, nil
}

func head(t *Table, limit int) [][]string {
	if limit <= 0 {
		limit = 100
	}
	n := len(t.Rows)
	if n > limit {
		n = limit
	}
	rows := make([][]string, n)
	for i := range rows {
		rows[i] = make([]string, len(t.Columns))
		for j := range t.Columns {
			if j < len(t.Rows[i]) {
				rows[i][j] = text(t.Rows[i][j])
			}
		}
	}
	return rows
}

func profile(t *Table) []ColumnProfile {
	out := make([]ColumnProfile, len(t.Columns))
	for j, name := range t.Columns {
		p := ColumnProfile{Name: name}
		seen := map[string]bool{}
		var nums []float64
		kinds := map[string]bool{}
		for _, r := range t.Rows {
			var v any
			if j < len(r) {
				v = cell(r[j])
			}
			if blank(v) {
				continue
			}
			p.Filled++
			if len(seen) < maxDistinct {
				seen[text(v)] = true
			}
			switch x := v.(type) {
			case float64:
				kinds["number"] = true
				if !math.IsNaN(x) && !math.IsInf(x, 0) {
					nums = append(nums, x)
				}
			case bool:
				kinds["bool"] = true
			default:
				kinds["text"] = true
			}
		}
		p.Distinct = len(seen)
		switch len(kinds) {
		case 0:
			p.Kind = "empty"
		case 1:
			for k := range kinds {
				p.Kind = k
			}
		default:
			p.Kind = "mixed"
		}
		if len(nums) > 1 {
			p.Min, p.Max = nums[0], nums[0]
			for _, x := range nums {
				p.Min, p.Max = math.Min(p.Min, x), math.Max(p.Max, x)
			}
			p.Bins = make([]int, 8)
			for _, x := range nums {
				b := 0
				if p.Max > p.Min {
					b = int((x - p.Min) / (p.Max - p.Min) * 8)
				}
				if b > 7 {
					b = 7
				}
				p.Bins[b]++
			}
		}
		out[j] = p
	}
	return out
}

// Steps lists the top-level step names in file order.
func (wf *Workflow) Steps() []string {
	names := make([]string, len(wf.steps))
	for i, st := range wf.steps {
		names[i] = st.name
	}
	return names
}
