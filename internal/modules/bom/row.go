package bom

import "miservice/internal/jsx"

// Row is one BOM line moving through the pipeline. It mirrors the row object
// of the Node service field for field (the snake_case names there are noted
// in comments where they are not obvious).
//
// The Node code kept "not yet set by a later stage" as JS `undefined` and had
// a Zod schema (schema.js) that logged a NOTE if a stage forgot one. In Go
// every field has a zero value and the compiler catches a misspelt field, so
// that runtime check has no counterpart; the *output* is unaffected because
// every place that read an unset value in JS treated it as "" already.
//
// A new stage that needs to remember something about a row adds a field here
// (the stage-by-stage comments show who sets what).
type Row struct {
	// Raw columns, already trimmed (set by ReadBom).
	Level, PartNumber, PartType, Description, Rev string
	Qty, UOM, FindNum, RefDes, MfgName, MfrPN     string

	OriginalIndex int    // 1-based row index within the sheet grid (original_index)
	OrigMfrPN     string // orig_mfr_pn
	InScope       bool   // false for rows below the last PCB row
	IsLevel0      bool
	GroupID       int
	HasABCDEFG    bool // row carries any of the A-G columns
	HasIJK        bool // row carries any of the I-K columns

	// Vendor policy (CFT-001).
	IsForbidden bool

	// Material Master / SAP enrichment.
	IsZ1        bool
	BlockStatus string
	Vendor      string
	VName       string
	VendorCode  string
	Stock       float64

	// Group stage: forward-filled values.
	EffectiveLevel, EffectivePartNumber, EffectivePartType string
	EffectiveDescription, EffectiveRev, EffectiveQty       string
	EffectiveUOM, EffectiveFindNum, EffectiveRefDes        string
	GroupFindNum                                           string // _group_find_num
	DisplayFindNum                                         string

	// Material Master lookup / CFT-002.
	ForthPN            string
	Priority           string // "MAIN", "SUBn" or ""
	IsDuplicateForthPN bool
	PriorityValue      *float64 // nil == JS null

	// Debug sheet columns (filled by CFT-002).
	DebugP1         string
	DebugVName      string
	DebugVendorCode string
	DebugStockQty   DebugStock
	DebugForthPN    string
	DebugEligible   string
	DebugReason     string
	DebugGroupID    string
}

// DebugStock is debug_stock_qty: a number for in-scope rows, "-" otherwise.
type DebugStock struct {
	IsNumber bool
	Num      float64
	Str      string
}

func cellToStr(s string) string { return jsx.Trim(s) }
