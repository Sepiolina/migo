package workflow

// StepComparison stores input positions (one-based) and changed column indices
// (zero-based). Counts cover all input rows even when the samples are smaller.
type StepComparison struct {
	Input            int        `json:"input"`
	ChangedRows      int        `json:"changedRows"`
	ChangedCells     int        `json:"changedCells"`
	Before           [][]string `json:"before"`
	Changed          [][]int    `json:"changed,omitempty"`
	Removed          [][]string `json:"removed,omitempty"`
	RemovedPositions []int      `json:"removedPositions,omitempty"`
	KeptPositions    []int      `json:"keptPositions,omitempty"`
	limit            int
	removedBudget    int
}

// Reserve space in the runner frame for both the ordinary trace and profiles.
const comparisonSampleBytes = 96 << 10

func newComparison(before *Table, limit int) *StepComparison {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	c := &StepComparison{Input: len(before.Rows), Before: [][]string{}, limit: limit, removedBudget: comparisonSampleBytes}
	budget := comparisonSampleBytes
	for _, r := range before.Rows {
		if len(c.Before) >= limit {
			break
		}
		row, ok := sampleComparisonRow(r, &budget)
		if !ok {
			break
		}
		c.Before = append(c.Before, row)
	}
	return c
}

func sampleComparisonRow(values []any, budget *int) ([]string, bool) {
	if *budget < 2+3*len(values) {
		return nil, false
	}
	row := make([]string, len(values))
	cost := 2
	for i, v := range values {
		row[i] = text(v)
		// A JSON escape can require six bytes per input byte.
		cost += 3 + 6*len(row[i])
		if cost > *budget {
			return nil, false
		}
	}
	*budget -= cost
	return row, true
}

func (c *StepComparison) addRemoved(i int, values []any) {
	if len(c.Removed) >= c.limit || c.removedBudget == 0 {
		return
	}
	row, ok := sampleComparisonRow(values, &c.removedBudget)
	if !ok {
		c.removedBudget = 0
		return
	}
	c.Removed = append(c.Removed, row)
	c.RemovedPositions = append(c.RemovedPositions, i+1)
}
