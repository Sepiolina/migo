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
type MasterDB struct{ pool *sqlserver.Pool }

// NewMasterDB queries through the shared pool.
func NewMasterDB(pool *sqlserver.Pool) *MasterDB { return &MasterDB{pool: pool} }

const materialMasterQuery = `
      SELECT MaterialNo, ISNULL(ZBlock, '') as ZBlock, Maker
      FROM SAP_ForthFacDb.dbo.Material_Master
      WHERE ZBlock <> 'D' AND Project = 'FNS - Finisar'
        AND UPPER(LTRIM(RTRIM(Maker))) IN (%s)
    `

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
		recs, err := queryChunk(ctx, db, chunk)
		if err != nil {
			return nil, err
		}
		BuildResultMap(recs, results)
	}
	return results, nil
}

func queryChunk(ctx context.Context, db *sql.DB, chunk []string) ([]MasterRecord, error) {
	args := make([]any, len(chunk))
	ph := make([]string, len(chunk))
	for i, mpn := range chunk {
		args[i] = sqlserver.VarChar(jsx.Norm(mpn)) // same normalised value that keys the result map
		ph[i] = fmt.Sprintf("@p%d", i+1)
	}
	rs, err := db.QueryContext(ctx, fmt.Sprintf(materialMasterQuery, strings.Join(ph, ", ")), args...)
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

// eligibleForLookup: rows that take part in the Forth P/N assignment.
func eligibleForLookup(r *Row) bool { return r.InScope && !r.IsLevel0 && r.HasIJK }

// applyFallbackForthPN is used when the DB is off or failed: every eligible
// row asks for a new P/N (blocked rows stay empty).
func applyFallbackForthPN(rows []*Row) {
	for _, r := range rows {
		if eligibleForLookup(r) {
			if r.IsForbidden || r.IsZ1 {
				r.ForthPN = ""
			} else {
				r.ForthPN = createPN
			}
		}
	}
}

// lookupForthPNs ports dbLookup (Material Master lookup + PCB SANMINA retry).
func lookupForthPNs(ctx context.Context, j *Job, lookup LookupFunc) {
	if !j.Cfg.DB.Enabled || lookup == nil {
		applyFallbackForthPN(j.Rows)
		return
	}
	lookupRows := rowsToLookUp(j.Rows)
	if err := resolveFromMaster(ctx, lookupRows, lookup); err != nil {
		j.Log.Printf("WARNING: MSSQL connection/lookup failed (%s) -- skipping DB lookup and using fallback Forth P/N.", err.Error())
		applyFallbackForthPN(j.Rows)
	}
}

// rowsToLookUp returns the eligible rows that have an MPN to search for; the
// ones without an MPN are settled right here.
func rowsToLookUp(rows []*Row) []*Row {
	var lookupRows []*Row
	for _, r := range rows {
		if !eligibleForLookup(r) {
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

// resolveFromMaster asks the lookup for every row's MPN (PCB rows that miss
// are retried with the _SANMINA suffix) and writes ForthPN / BlockStatus.
func resolveFromMaster(ctx context.Context, lookupRows []*Row, lookup LookupFunc) error {
	var mpns []string
	for _, r := range lookupRows {
		mpns = append(mpns, jsx.Trim(r.MfrPN))
	}
	results, err := lookup(ctx, uniqueStrings(mpns))
	if err != nil {
		return err
	}
	var sanmina []string
	for _, r := range lookupRows {
		if isPcbRow(r) {
			if _, hit := results[jsx.Norm(r.MfrPN)]; !hit {
				sanmina = append(sanmina, WithSanminaSuffix(r.MfrPN))
			}
		}
	}
	sanResults := map[string]Match{}
	if len(sanmina) > 0 {
		if sanResults, err = lookup(ctx, uniqueStrings(sanmina)); err != nil {
			return err
		}
	}
	for _, r := range lookupRows {
		applyMatch(r, results, sanResults)
	}
	return nil
}

func applyMatch(r *Row, results, sanResults map[string]Match) {
	match, found := results[jsx.Norm(r.MfrPN)]
	// PCB rows: (A) <pn>_REV_<REV> first, then (B) ..._SANMINA.
	if !found && isPcbRow(r) {
		if sm, ok := sanResults[jsx.Norm(WithSanminaSuffix(r.MfrPN))]; ok {
			match, found = sm, true
			r.MfrPN = WithSanminaSuffix(r.MfrPN)
		}
	}
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
