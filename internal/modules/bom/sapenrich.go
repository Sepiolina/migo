package bom

import (
	"context"
	"regexp"
	"sort"

	"miservice/internal/clients/sap"
	"miservice/internal/config"
	"miservice/internal/jsx"
	"miservice/internal/logx"
)

const (
	// sapEmployeeID is hard-coded in the Node service too ("no longer
	// sourced from the emp/bam field").
	sapEmployeeID = "106049"

	defaultSAPBatchSize = 50
	defaultSAPRetries   = 3
)

var leadingZeros = regexp.MustCompile(`^0+`)

func firstTruthy(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v := m[k]; jsx.Truthy(v) {
			return v
		}
	}
	return nil
}

func toFloatAny(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		return jsx.ToFloat(x, 0)
	}
	return 0
}

func orDefault(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

// enrichFromSAP ports sapEnrich: block status, vendor and stock per Forth P/N.
func enrichFromSAP(ctx context.Context, j *Job, client *sap.Client) {
	cfg := j.Cfg.SAP
	if !cfg.Enabled {
		return
	}
	eligible := sapEligibleRows(j.Rows, j.Policy)
	matnrs := uniqueSortedForthPNs(eligible)
	if len(matnrs) == 0 {
		j.Log.Print("SAP enrichment: no eligible Forth P/N rows to query.")
		return
	}
	info := fetchSAPInfo(ctx, client, cfg, matnrs, j.Log)

	matched := 0
	for _, r := range eligible {
		pn := jsx.Norm(r.ForthPN)
		rec, ok := info[pn]
		if !ok {
			rec, ok = info[jsx.Norm(leadingZeros.ReplaceAllString(pn, ""))]
		}
		if !ok {
			continue
		}
		matched++
		applySAPRecord(r, rec)
	}
	j.Log.Printf("SAP enrichment: matched %d / %d rows.", matched, len(eligible))
}

// sapEligibleRows: rows that have a real Forth P/N and are not blocked by
// the vendor policy.
func sapEligibleRows(rows []*Row, policy *VendorPolicy) []*Row {
	var eligible []*Row
	for _, r := range rows {
		if r.InScope && !r.IsLevel0 && r.HasIJK &&
			(!r.IsForbidden || IsExemptUnderPolicy(r, policy)) &&
			r.ForthPN != "" && r.ForthPN != createPN {
			eligible = append(eligible, r)
		}
	}
	return eligible
}

func uniqueSortedForthPNs(rows []*Row) []string {
	set := map[string]bool{}
	var matnrs []string
	for _, r := range rows {
		if !set[r.ForthPN] {
			set[r.ForthPN] = true
			matnrs = append(matnrs, r.ForthPN)
		}
	}
	sort.Strings(matnrs) // JS default sort is by UTF-16 code unit; identical for ASCII material numbers
	return matnrs
}

// fetchSAPInfo queries the gateway in batches and indexes the answers by
// norm(MATNR), with and without leading zeros. A batch that keeps failing is
// logged and skipped; the rows it covered simply stay un-enriched.
func fetchSAPInfo(ctx context.Context, client *sap.Client, cfg config.SAP, matnrs []string, log logx.Logger) map[string]map[string]any {
	batchSize := orDefault(cfg.BatchSize, defaultSAPBatchSize)
	retries := orDefault(cfg.Retries, defaultSAPRetries)
	ep := sap.Endpoint{Host: cfg.Host, Port: cfg.Port, Tcode: cfg.Tcode, EmployeeID: sapEmployeeID}
	info := map[string]map[string]any{}

	for i := 0; i < len(matnrs); i += batchSize {
		end := i + batchSize
		if end > len(matnrs) {
			end = len(matnrs)
		}
		data := []map[string]any{{"WERKS": cfg.Werks}}
		for _, m := range matnrs[i:end] {
			data = append(data, map[string]any{"MATNR": m})
		}
		rows, err := client.CallRFC(ctx, ep, data, retries)
		if err != nil {
			log.Printf("WARN: SAP batch %d failed after %d retries: %s", i/batchSize+1, retries, err.Error())
			continue
		}
		for _, rec := range rows {
			if m := rec["matnr"]; jsx.Truthy(m) {
				ms := jsx.Trim(jsx.String(m))
				info[jsx.Norm(ms)] = rec
				info[jsx.Norm(leadingZeros.ReplaceAllString(ms, ""))] = rec
			}
		}
	}
	return info
}

// applySAPRecord copies status, vendor and stock from one SAP record.
func applySAPRecord(r *Row, rec map[string]any) {
	status := ""
	if v := firstTruthy(rec, "mmsta", "mstae", "zblock", "status"); v != nil {
		status = jsx.Trim(jsx.String(v))
	}
	if status != "" {
		r.BlockStatus = status
	}
	if jsx.Norm(r.BlockStatus) == "Z1" || jsx.Norm(status) == "Z1" {
		r.BlockStatus = "Z1"
		r.IsZ1 = true
		if r.ForthPN == createPN {
			r.ForthPN = ""
		}
	}

	vname, lifnr := "", ""
	if v := rec["vname"]; jsx.Truthy(v) {
		vname = jsx.Trim(jsx.String(v))
	}
	if v := rec["lifnr"]; jsx.Truthy(v) {
		lifnr = jsx.Trim(jsx.String(v))
	}
	r.VName, r.VendorCode = vname, lifnr
	r.Vendor = vname
	if r.Vendor == "" {
		r.Vendor = lifnr
	}

	stockVal, has := rec["labst"]
	if !has || stockVal == nil {
		stockVal = rec["stock"]
	}
	r.Stock = toFloatAny(stockVal)
}
