// Package bom holds the BOM-processor steps that workflows/bom.yaml strings
// together: read a customer BOM plus a Priority workbook, write the annotated
// workbook. The business rules (CFT-001 vendors, Material_Master query and
// coverage, SAP fields, CFT-002 eligibility and MAIN conditions) live in the
// workflow file as expressions; this package is the machinery.
//
//	steps.go          the step registry (Ops) and the shared Job
//	rules.go          the row fields rule expressions can read
//	readers.go        input workbooks -> Rows
//	grouping.go       forward fill, find-num groups, qty fix, PCB scope
//	pcb.go            PCB detection and MPN reformatting
//	vendorpolicy.go   CFT-001 engine (vendor_policy)
//	materialmaster.go Forth P/N lookup in SQL Server (material_master)
//	sapenrich.go      block status / vendor / stock from SAP (sap_enrich)
//	selection.go      CFT-002: MAIN / SUBn per group (select_priority)
//	sheetbuilder.go   Rows -> cells and colours;  layout.go / format.go: constants
//	writer.go         cells -> .xlsx file
package bom
