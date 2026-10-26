package bom

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"miservice/internal/clients/sap"
	"miservice/internal/config"
	"miservice/internal/jsx"
	"miservice/internal/logx"
)

const (
	defaultSAPBatchSize = 50
	defaultSAPRetries   = 3
)

// SAPSpec is the sap_enrich step argument:
//
//	rows: 'in_scope && ... && forth_pn not in ["", "CREATE P/N"]'   # rows sent to SAP
//	employee_id: "106049"
//	fields:              # SAP answer keys to read, in order of preference
//	  status: [mmsta, mstae, zblock, status]
//	  vendor_name: [vname]
//	  vendor_code: [lifnr]
//	  stock: [labst, stock]
//
// Text fields take the first non-empty value, stock the first key present.
// Connection settings (host, port, tcode, werks, batch size, retries) and
// sap_enabled stay in config.json.
type SAPSpec struct {
	Rows       string `yaml:"rows"`
	EmployeeID string `yaml:"employee_id"`
	Fields     struct {
		Status     []string `yaml:"status"`
		VendorName []string `yaml:"vendor_name"`
		VendorCode []string `yaml:"vendor_code"`
		Stock      []string `yaml:"stock"`
	} `yaml:"fields"`
}

type sapStep struct {
	SAPSpec
	rows rule
}

func (s SAPSpec) compile() (*sapStep, error) {
	st := &sapStep{SAPSpec: s}
	var err error
	if st.rows, err = compileRule(s.Rows); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	if strings.TrimSpace(s.EmployeeID) == "" {
		return nil, fmt.Errorf("employee_id must not be blank")
	}
	f := s.Fields
	if len(f.Status) == 0 || len(f.VendorName) == 0 || len(f.VendorCode) == 0 || len(f.Stock) == 0 {
		return nil, fmt.Errorf("fields needs status, vendor_name, vendor_code and stock")
	}
	return st, nil
}

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

// enrich ports sapEnrich: block status, vendor and stock per Forth P/N.
func (st *sapStep) enrich(ctx context.Context, j *Job, client *sap.Client) {
	cfg := j.Cfg.SAP
	if !cfg.Enabled {
		return
	}
	var eligible []*Row
	for _, r := range j.Rows {
		if st.rows.test(envOf(r, j.Policy)) {
			eligible = append(eligible, r)
		}
	}
	matnrs := uniqueSortedForthPNs(eligible)
	if len(matnrs) == 0 {
		j.Log.Print("SAP enrichment: no eligible Forth P/N rows to query.")
		return
	}
	info := fetchSAPInfo(ctx, client, cfg, st.EmployeeID, matnrs, j.Log)

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
		st.apply(r, rec)
	}
	j.Log.Printf("SAP enrichment: matched %d / %d rows.", matched, len(eligible))
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
func fetchSAPInfo(ctx context.Context, client *sap.Client, cfg config.SAP, employeeID string, matnrs []string, log logx.Logger) map[string]map[string]any {
	batchSize := orDefault(cfg.BatchSize, defaultSAPBatchSize)
	retries := orDefault(cfg.Retries, defaultSAPRetries)
	ep := sap.Endpoint{Host: cfg.Host, Port: cfg.Port, Tcode: cfg.Tcode, EmployeeID: employeeID}
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

// apply copies status, vendor and stock from one SAP record.
func (st *sapStep) apply(r *Row, rec map[string]any) {
	f := st.Fields
	status := ""
	if v := firstTruthy(rec, f.Status...); v != nil {
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
	if v := firstTruthy(rec, f.VendorName...); v != nil {
		vname = jsx.Trim(jsx.String(v))
	}
	if v := firstTruthy(rec, f.VendorCode...); v != nil {
		lifnr = jsx.Trim(jsx.String(v))
	}
	r.VName, r.VendorCode = vname, lifnr
	r.Vendor = vname
	if r.Vendor == "" {
		r.Vendor = lifnr
	}

	var stockVal any
	for _, k := range f.Stock {
		if stockVal = rec[k]; stockVal != nil {
			break
		}
	}
	r.Stock = toFloatAny(stockVal)
}
