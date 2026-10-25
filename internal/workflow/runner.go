package workflow

import (
	"context"
	"fmt"
	"path/filepath"

	"miservice/internal/sheet"
)

// Execute evaluates ordered processes into independent named snapshots.
// header_row counts from the input reader's declared-range origin.
func Execute(ctx context.Context, cfg *Config) (map[string]*Table, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	tables := make(map[string]*Table)
	for i, p := range cfg.Process {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sourceContext := fmt.Sprintf("process %d (%s), from %s", i+1, p.As, p.From)
		if alias, name, e := ParseSource(p.From); e == nil && alias != "" {
			sourceContext += fmt.Sprintf(", file %q sheet %q", resolvePath(cfg.BaseDir, cfg.Input[alias]), name)
		}
		t, err := processSource(cfg, p, tables)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sourceContext, err)
		}
		for j, step := range p.Do {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if step.Enabled != nil && !*step.Enabled {
				continue
			}
			t, err = ApplyStep(t, step, tables)
			if err != nil {
				return nil, fmt.Errorf("%s, step %d (%s): %w", sourceContext, j+1, StepName(step), err)
			}
		}
		if err := ValidateTable(t); err != nil {
			return nil, fmt.Errorf("process %d (%s): %w", i+1, p.As, err)
		}
		tables[p.As] = CloneTable(t)
	}
	return tables, nil
}

func processSource(cfg *Config, p Process, tables map[string]*Table) (*Table, error) {
	alias, name, err := ParseSource(p.From)
	if err != nil {
		return nil, err
	}
	if alias == "" {
		t, ok := tables[p.From]
		if !ok {
			return nil, fmt.Errorf("unknown dataset %q", p.From)
		}
		return CloneTable(t), nil
	}
	path := resolvePath(cfg.BaseDir, cfg.Input[alias])
	wb, err := sheet.Open(path)
	if err != nil {
		return nil, fmt.Errorf("file %q sheet %q: %w", path, name, err)
	}
	grid, readErr := wb.Rows(name)
	closeErr := wb.Close()
	if readErr != nil {
		return nil, fmt.Errorf("file %q sheet %q: %w", path, name, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("file %q: close: %w", path, closeErr)
	}
	t, err := tableFromGrid(grid, p.HeaderRow)
	if err != nil {
		return nil, fmt.Errorf("file %q sheet %q: %w", path, name, err)
	}
	return t, nil
}

func tableFromGrid(grid [][]string, headerRow int) (*Table, error) {
	if headerRow == 0 {
		headerRow = 1
	}
	if headerRow < 1 || headerRow > len(grid) {
		return nil, fmt.Errorf("header_row %d is outside sheet with %d rows", headerRow, len(grid))
	}
	headers := append([]string(nil), grid[headerRow-1]...)
	// Readers return the declared grid width. Ignore unused trailing columns.
	for len(headers) > 0 && headers[len(headers)-1] == "" {
		headers = headers[:len(headers)-1]
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("header row is empty")
	}
	t := &Table{Columns: headers}
	for i, row := range grid[headerRow:] {
		for _, v := range row[min(len(row), len(headers)):] {
			if v != "" {
				return nil, fmt.Errorf("row %d has data beyond the %d headers", headerRow+i+1, len(headers))
			}
		}
		dst := make([]string, len(headers))
		copy(dst, row)
		t.Rows = append(t.Rows, dst)
	}
	if err := ValidateTable(t); err != nil {
		return nil, err
	}
	return t, nil
}

func resolvePath(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

// Run computes every dataset and stages every workbook before publishing any
// output. Each rename is atomic; publishing several files is not a transaction.
func Run(ctx context.Context, cfg *Config) error {
	return run(ctx, cfg, true)
}

// Check computes and renders all outputs, removing staged files without publishing.
func Check(ctx context.Context, cfg *Config) error { return run(ctx, cfg, false) }

func run(ctx context.Context, cfg *Config, publish bool) error {
	tables, err := Execute(ctx, cfg)
	if err != nil {
		return err
	}
	return exportWorkbooks(ctx, cfg, tables, publish)
}
