package bom

// Output workbook layout (excelWriter.js): which column holds what.
//
// The "BOM" sheet has columns A-O. The "debug" sheet has the same A-O, then
// four spacer columns P-S and the debug block T-AA. All indexes are 0-based.
const (
	colMfgName     = 9 // J
	colMfrPN       = 10
	colSeparator   = 11 // L: black separator
	colBlockStatus = 12
	colForthPN     = 13
	colPriority    = 14
	nBaseCols      = 15 // A-O

	colDebugSpacerFirst = 15 // P
	colDebugSpacerLast  = 18 // S
	colDebugP1          = 19 // T
	colDebugVName       = 20
	colDebugVendorCode  = 21
	colDebugStock       = 22
	colDebugForthPN     = 23
	colDebugEligible    = 24
	colDebugReason      = 25
	colDebugGroupID     = 26 // AA
)

const (
	StandardSheetName = "BOM"
	DebugSheetName    = "debug"
)

var outputHeaders = []string{
	"Level", "Part Number", "Part Type", "Description", "Rev",
	"BOM.Qty", "BOM.UOM", "BOM.Find Num", "BOM.Ref Des",
	"Manufacturers.Mfg Name", "Manufacturers.Mfr. Part Number",
	"", // col 12 separator
	"Block Status", "Forth P/N", "Priority",
}

// P-S are spacers, T-AA the debug columns.
var debugHeadersTOnward = []string{
	"", "", "", "",
	"Debug: Priority (P1)", "Debug: SAP Vendor Name", "Debug: SAP Vendor Code", "Debug: SAP Stock Qty",
	"Debug: Forth Material No", "Debug: Eligibility Status", "Debug: Decision Reason", "Debug: Group ID",
}

var colWidths = []float64{6, 14, 10, 34, 14, 8, 8, 12, 12, 24, 20, 4, 12, 16, 10}
var debugColWidths = []float64{2, 2, 2, 2, 16, 28, 16, 16, 20, 24, 48, 12}
