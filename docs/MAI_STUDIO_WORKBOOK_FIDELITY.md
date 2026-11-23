# Mai Studio saved workbook fidelity

## Reference workbook

The production Mai Studio Go runner's offline `run` command generated the reference from `workflows/bom.yaml`, `testdata/bom_basic.xlsx`, and `testdata/prio_basic.xlsx`. Two independent runs produced identical bytes. The 9,308-byte workbook has SHA-256 `e28750653cd93b324a2015bb37a1b57128b912d1898eb04abd4df380cf7a80b0`. The review copy is at `/workspace/shared/mai-studio-bom-basic.xlsx`.

The saved workbook's ZIP parts give the following reference. Each sheet range includes the header row.

| Sheet and range | Saved value and formatting |
| --- | --- |
| BOM!A1:O1 | All 15 headers, including the empty separator label at L1. Centered horizontally and vertically, bold 10 pt Calibri. Row 1 height is 28 pt and the top row is frozen in the file. |
| BOM!A2:O2 | Yellow `#FFFF00`, with a thick black bottom border. |
| BOM!J6:K6 | `YAGEO CORPORATION` and `RC0402FR-0710KL_REV_B2`. Red `#FF0000`, white bold 10 pt Calibri, centered, with a thick black bottom border. |
| BOM!O3, O5 | `MAIN`. Blue `#0070C0`, white bold 10 pt Calibri, centered. O5 has a thick black bottom border. |
| BOM!O4 | `SUB1`. Green `#2E7D32`, white bold 10 pt Calibri, centered. |
| BOM!L3:L6 | Empty cells with black `#000000` fill. The separator column has width 4 in Excel column-width units. |
| debug!P1:S1 | Four empty separator headers in 11 pt Calibri. Each column has width 2. |
| debug!T1:AA1 | Eight debug headers, starting with `Debug: Priority (P1)`. Navy `#2C3E50` with white bold 10 pt Calibri, centered. |
| debug!T2:AA7 | Debug values including numeric stock, grey grid borders, group-ending borders and status-specific colors. |

`BOM` is 7 rows by 15 columns. Its width sequence is `6, 14, 10, 34, 14, 8, 8, 12, 12, 24, 20, 4, 12, 16, 10`. The full header sequence is Level, Part Number, Part Type, Description, Rev, BOM.Qty, BOM.UOM, BOM.Find Num, BOM.Ref Des, Manufacturers.Mfg Name, Manufacturers.Mfr. Part Number, empty, Block Status, Forth P/N, Priority.

`debug` is 7 rows by 27 columns. It repeats the BOM columns and adds widths `2, 2, 2, 2, 16, 28, 16, 16, 20, 24, 48, 12`. For focused screenshot comparisons, use BOM J1:O7 and debug T1:AA7.

## Reproduce and verify

From the repository root, run the following with Python 3.8 or later and the project's Go toolchain. Set `MAI_GO` to a Go executable if needed, or pass `--runner /path/to/mai-runner` to reuse a build.

```sh
python3 apps/studio/scripts/smoke-workbook.py --save /tmp/mai-studio-bom-basic.xlsx
```

The script generates a fresh artifact through the real runner in a temporary workspace and checks its SHA-256 against the runner's response. It resolves sheets through the workbook relationships, reads shared strings and worksheet cells, and decodes the style and width tables with Python's standard library. It compares both full sheets and two offset pages through Studio's `inspectWorkbookPage` command. Comparisons cover values, empty styled cells, dimensions, widths, row heights, bold, font size and family, explicit font and fill colors, alignment, and borders.

## Studio rendering

In the Studio browser check, the saved 28 pt header occupied approximately 37 CSS pixels and the four-unit separator occupied 28 CSS pixels. The header, blue priority cells, red manufacturer cells, and blank black separator retained their workbook styles. The check exercised sheet tabs, address jumps, and a 390-pixel viewport with the real Go runner. Studio discloses the saved frozen pane under **Preview limitations**. Excel column-width conversion assumes a seven-pixel maximum digit width at 96 DPI. Installed fonts and display scaling can change the visual result.

## LibreOffice Calc reference

LibreOfficeDev Calc 26.8.0.0.alpha0 opened the exact saved workbook and exported both sheets as one page each. The BOM raster contains the exact red, blue, black, and yellow RGB fills from the workbook. PDF text extraction contains both long manufacturer headers and the red-row part number. The K1 heading visibly clips at its saved width in the raster. Calc embedded Carlito and Carlito Bold as substitutes for the workbook’s Calibri. The PDF and full-sheet rasters are at `/workspace/shared/mai-studio-bom-calc.pdf`, `/workspace/shared/mai-studio-bom-calc.png`, and `/workspace/shared/mai-studio-bom-calc-debug.png`.

This environment has the headless Calc export build. Microsoft Excel and an interactive Calc desktop session were unavailable, so the visual reference is Calc's print rendering. An interactive comparison can open the same saved XLSX and inspect the ranges above.

```sh
soffice --headless --convert-to 'pdf:calc_pdf_Export:{"SinglePageSheets":{"type":"boolean","value":"true"}}' --outdir /tmp /tmp/mai-studio-bom-basic.xlsx
pdftoppm -r 108 -png /tmp/mai-studio-bom-basic.pdf /tmp/mai-studio-bom-calc
```

## Embedded renderer comparison

A disposable Vite app imported the same 9,308-byte workbook with LuckyExcel 1.0.1 and displayed it with FortuneSheet 1.0.4. Chromium ran with the network disabled after loading the app. Both sheets opened in the renderer, including the red manufacturer cells, blue MAIN cells, black blank separator, yellow assembly row, and navy debug headers. The import reported Calibri 10 bold on the colored cells, and widths of 190, 158, 30 and 78 px for BOM columns J, K, L and O. The stock test cells import as the string `0` with format `#,##0`. This fixture's zero values have the same display with or without the numeric format. Screenshots are at `/workspace/shared/mai-studio-bom-embedded.png` and `/workspace/shared/mai-studio-bom-embedded-debug.png`.

ArrowRight moved J5 to K5, Shift+ArrowDown extended the range to K6, Tab moved to L5, and Ctrl+C copied the selected manufacturer text. Enter, Home, End, PageDown and PageUp left the selection in place on this fixture. The sheet controls switched sheets by click. Enter and Space on the focused BOM control left the debug sheet active. The canvas had zero accessible grid or gridcell roles. The name box opened with `A1:NaN`, and entering O3 left J5 selected.

The same-app Vite build grew from 187,355 to 3,242,701 bytes with the embedded renderer and importer. The sum of individually gzipped build files rose from 59,320 to 739,678 bytes. The whole-workbook importer produced a 60,796-byte browser-local model from the small reference file. Production integration would need to supply the complete ZIP through Studio's workspace-scoped boundary. FortuneSheet and LuckyExcel use MIT licenses, with distribution notices to account for both packages and their transitive dependencies. [FortuneSheet 1.0.4](https://www.npmjs.com/package/@fortune-sheet/react/v/1.0.4) · [LuckyExcel 1.0.1](https://www.npmjs.com/package/luckyexcel/v/1.0.1)

The native preview uses the existing Studio grid and retrieves bounded row and column pages. It keeps the selection and accessibility behavior in Studio and avoids shipping a separate spreadsheet UI and ZIP importer. The renderer experiment provides a visual comparison for supported BOM styles. Its larger bundle, full-file import, and canvas accessibility gaps favor the native path for this read-only preview.
