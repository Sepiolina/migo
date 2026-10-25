// Package bom holds the BOM-processor steps that workflows/bom.yaml strings
// together: read a customer BOM plus a Priority workbook, write the annotated
// workbook. The business rules that change (CFT-001 vendors) live in the
// workflow file; this package is the machinery.
//
//	steps.go          the step registry (Ops) and the shared Job
//	readers.go        input workbooks -> Rows
//	grouping.go       forward fill, find-num groups, qty fix, PCB scope
//	pcb.go            PCB detection and MPN reformatting
//	vendorpolicy.go   CFT-001 engine; the rules come from vendor_policy
//	materialmaster.go Forth P/N lookup in SQL Server
//	sapenrich.go      block status / vendor / stock from SAP
//	selection.go      CFT-002: MAIN / SUBn per group
//	sheetbuilder.go   Rows -> cells and colours;  layout.go / format.go: constants
//	writer.go         cells -> .xlsx file
package bom
