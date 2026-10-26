package bom

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"miservice/internal/clients/sqlserver"
	"miservice/internal/jsx"
)

// Match is one Material_Master hit.
type Match struct{ MaterialNo, ZBlock string }

// LookupFunc resolves manufacturer part numbers against Material_Master. The
// result map is keyed by jsx.Norm(mpn). Anything with this signature can
// stand in for the database (tests, migo -stubdb).
type LookupFunc func(ctx context.Context, mpns []string) (map[string]Match, error)

// MasterRecord is one Material_Master row as returned by the query.
type MasterRecord struct{ MaterialNo, ZBlock, Maker string }

// BuildResultMap adds records to existing keyed by norm(Maker); the first
// match per MPN wins and blank makers are skipped.
func BuildResultMap(recs []MasterRecord, existing map[string]Match) map[string]Match {
	if existing == nil {
		existing = map[string]Match{}
	}
	for _, r := range recs {
		key := jsx.Norm(r.Maker)
		if key == "" {
			continue
		}
		if _, ok := existing[key]; !ok {
			existing[key] = Match{MaterialNo: r.MaterialNo, ZBlock: r.ZBlock}
		}
	}
	return existing
}

// MasterDB is the real LookupFunc: Material_Master on SQL Server.
type MasterDB struct {
	pool  *sqlserver.Pool
	query string // from the workflow file; @mpns becomes the parameter list
}

// mpnsParam is the placeholder for the normalised MPNs in a lookup query.
const mpnsParam = "@mpns"

// Lookup is a LookupFunc: one batched, parameterised IN query per chunk.
func (m *MasterDB) Lookup(ctx context.Context, mpns []string) (map[string]Match, error) {
	results := map[string]Match{}
	if len(mpns) == 0 {
		return results, nil
	}
	db, err := m.pool.DB()
	if err != nil {
		return nil, err
	}
	for _, chunk := range sqlserver.Chunk(mpns, sqlserver.MaxParams) {
		recs, err := queryChunk(ctx, db, m.query, chunk)
		if err != nil {
			return nil, err
		}
		BuildResultMap(recs, results)
	}
	return results, nil
}

func queryChunk(ctx context.Context, db *sql.DB, query string, chunk []string) ([]MasterRecord, error) {
	args := make([]any, len(chunk))
	ph := make([]string, len(chunk))
	for i, mpn := range chunk {
		args[i] = sqlserver.VarChar(jsx.Norm(mpn)) // same normalised value that keys the result map
		ph[i] = fmt.Sprintf("@p%d", i+1)
	}
	rs, err := db.QueryContext(ctx, strings.Replace(query, mpnsParam, strings.Join(ph, ", "), 1), args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var recs []MasterRecord
	for rs.Next() {
		var mat, z, maker sql.NullString
		if err := rs.Scan(&mat, &z, &maker); err != nil {
			return nil, err
		}
		recs = append(recs, MasterRecord{mat.String, z.String, maker.String})
	}
	return recs, rs.Err()
}

// ---- the material_master step ---------------------------------------------

const createPN = "CREATE P/N" // marker for "no Forth P/N exists yet"

// MasterSpec is the material_master step argument:
//
//	rows:  'in_scope && !level0 && has_ijk'   # rows that get a Forth P/N
//	query: SELECT MaterialNo, ZBlock, Maker ... IN (@mpns)
//	retry: {rows: pcb, suffix: _SANMINA}       # missed rows looked up again with a suffix
//
// The query returns MaterialNo, ZBlock and Maker (the MPN) in that order;
// @mpns becomes the normalised MPNs as parameters. When db_enabled is off or
// the lookup fails, every covered row asks for a new P/N ("CREATE P/N").
type MasterSpec struct {
	Rows  string `yaml:"rows"`
	Query string `yaml:"query"`
	Retry *struct {
		Rows   string `yaml:"rows"`
		Suffix string `yaml:"suffix"`
	} `yaml:"retry"`
}

type master struct {
	rows        rule
	query       string
	retry       *rule
	retrySuffix string
}

func (s MasterSpec) compile() (*master, error) {
	m := &master{query: s.Query}
	var err error
	if m.rows, err = compileRule(s.Rows); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	if strings.Count(s.Query, mpnsParam) != 1 {
		return nil, fmt.Errorf("query must contain %s exactly once", mpnsParam)
	}
	if s.Retry != nil {
		r, err := compileRule(s.Retry.Rows)
		if err != nil {
			return nil, fmt.Errorf("retry rows: %w", err)
		}
		if strings.TrimSpace(s.Retry.Suffix) == "" {
			return nil, fmt.Errorf("retry needs a suffix")
		}
		m.retry, m.retrySuffix = &r, s.Retry.Suffix
	}
	return m, nil
}

// applyFallbackForthPN is used when the DB is off or failed: every covered
// row asks for a new P/N (blocked rows stay empty).
func (m *master) applyFallbackForthPN(rows []*Row) {
	for _, r := range rows {
		if m.rows.test(envOf(r, nil)) {
			if r.IsForbidden || r.IsZ1 {
				r.ForthPN = ""
			} else {
				r.ForthPN = createPN
			}
		}
	}
}

// lookupForthPNs ports dbLookup (Material Master lookup + PCB SANMINA retry).
// lookup overrides the database (tests, migo -stubdb).
func (m *master) lookupForthPNs(ctx context.Context, j *Job, d Deps) {
	lookup := d.Lookup
	if lookup == nil && d.DB != nil {
		lookup = (&MasterDB{pool: d.DB, query: m.query}).Lookup
	}
	if !j.Cfg.DB.Enabled || lookup == nil {
		m.applyFallbackForthPN(j.Rows)
		return
	}
	if err := m.resolve(ctx, m.rowsToLookUp(j.Rows), lookup); err != nil {
		j.Log.Printf("WARNING: MSSQL connection/lookup failed (%s) -- skipping DB lookup and using fallback Forth P/N.", err.Error())
		m.applyFallbackForthPN(j.Rows)
	}
}

// rowsToLookUp returns the covered rows that have an MPN to search for; the
// ones without an MPN are settled right here.
func (m *master) rowsToLookUp(rows []*Row) []*Row {
	var lookupRows []*Row
	for _, r := range rows {
		if !m.rows.test(envOf(r, nil)) {
			continue
		}
		if jsx.Trim(r.MfrPN) == "" {
			if r.IsForbidden {
				r.ForthPN = ""
			} else {
				r.ForthPN = createPN
			}
		} else {
			lookupRows = append(lookupRows, r)
		}
	}
	return lookupRows
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// resolve asks the lookup for every row's MPN (retry rows that miss are
// asked again with the suffix) and writes ForthPN / BlockStatus.
func (m *master) resolve(ctx context.Context, lookupRows []*Row, lookup LookupFunc) error {
	var mpns []string
	retry := make([]bool, len(lookupRows))
	for i, r := range lookupRows {
		mpns = append(mpns, jsx.Trim(r.MfrPN))
		retry[i] = m.retry != nil && m.retry.test(envOf(r, nil))
	}
	results, err := lookup(ctx, uniqueStrings(mpns))
	if err != nil {
		return err
	}
	var again []string
	for i, r := range lookupRows {
		if _, hit := results[jsx.Norm(r.MfrPN)]; retry[i] && !hit {
			again = append(again, withSuffix(r.MfrPN, m.retrySuffix))
		}
	}
	againResults := map[string]Match{}
	if len(again) > 0 {
		if againResults, err = lookup(ctx, uniqueStrings(again)); err != nil {
			return err
		}
	}
	for i, r := range lookupRows {
		match, found := results[jsx.Norm(r.MfrPN)]
		// PCB rows: (A) <pn>_REV_<REV> first, then (B) ..._SANMINA.
		if !found && retry[i] {
			if sm, ok := againResults[jsx.Norm(withSuffix(r.MfrPN, m.retrySuffix))]; ok {
				match, found = sm, true
				r.MfrPN = withSuffix(r.MfrPN, m.retrySuffix)
			}
		}
		applyMatch(r, match, found)
	}
	return nil
}

func applyMatch(r *Row, match Match, found bool) {
	pn, z := createPN, ""
	if found {
		pn, z = match.MaterialNo, match.ZBlock
	}
	if r.IsForbidden && pn == createPN {
		r.ForthPN = ""
	} else {
		r.ForthPN = pn
	}
	r.BlockStatus = z
	if jsx.Norm(r.BlockStatus) == "Z1" {
		r.IsZ1 = true
	}
}
