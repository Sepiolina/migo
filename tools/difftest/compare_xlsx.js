#!/usr/bin/env node
// Cell-by-cell comparison of two output workbooks (e.g. Node vs Go output).
// Usage: node tools/compare_xlsx.js a.xlsx b.xlsx   (needs `npm i exceljs`)
// Compares sheet names/order, frozen panes, column widths, row heights and, for
// every cell: type, value, fill, font, border, alignment, number format.
// Values that ExcelJS reports identically but that differ only in how the
// file stores them (bgColor indexed 64, font family/scheme, theme-vs-default
// font) are normalised away -- they render identically.
const ExcelJS = require(process.env.EXCELJS || 'exceljs');

const DEFAULT_FONT = { name: 'Calibri', size: 11, bold: false, color: 'theme1' };
function normFont(f) {
  if (!f || (!f.name && !f.size && !f.bold && !f.color)) return DEFAULT_FONT;
  const color = f.color ? (f.color.argb || (f.color.theme !== undefined ? 'theme' + f.color.theme : '')) : 'theme1';
  return { name: f.name || 'Calibri', size: f.size || 11, bold: !!f.bold, color };
}
function normFill(f) {
  if (!f || f.pattern === 'none' || !f.pattern) return null;
  return { pattern: f.pattern, fg: f.fgColor && f.fgColor.argb };
}
function normBorder(b) {
  const o = {};
  for (const s of ['left', 'right', 'top', 'bottom']) {
    if (b && b[s] && b[s].style) o[s] = b[s].style + ':' + (b[s].color && b[s].color.argb);
  }
  return o;
}
function normCell(c) {
  const v = c.value;
  return {
    t: c.type === 2 ? 'num' : c.type === 3 ? 'str' : c.type,
    v: v === null || v === undefined ? null : v,
    fill: normFill(c.fill), font: normFont(c.font), border: normBorder(c.border),
    align: c.alignment ? { h: c.alignment.horizontal, v: c.alignment.vertical, w: !!c.alignment.wrapText } : null,
    numFmt: c.numFmt && c.numFmt !== 'General' ? c.numFmt : null,
  };
}
async function load(p) { const wb = new ExcelJS.Workbook(); await wb.xlsx.readFile(p); return wb; }
(async () => {
  const [a, b] = await Promise.all([load(process.argv[2]), load(process.argv[3])]);
  const diffs = [];
  const na = a.worksheets.map(s => s.name), nb = b.worksheets.map(s => s.name);
  if (JSON.stringify(na) !== JSON.stringify(nb)) diffs.push(`sheet names: ${na} vs ${nb}`);
  for (const wa of a.worksheets) {
    const wb2 = b.getWorksheet(wa.name); if (!wb2) continue;
    const pa = wa.views[0] || {}, pb = wb2.views[0] || {};
    if (pa.state !== pb.state || pa.ySplit !== pb.ySplit || pa.topLeftCell !== pb.topLeftCell) diffs.push(`${wa.name}: panes differ`);
    const nc = Math.max(wa.columnCount, wb2.columnCount);
    for (let c = 1; c <= nc; c++) {
      const x = wa.getColumn(c), y = wb2.getColumn(c);
      if (x.width !== y.width) diffs.push(`${wa.name}: col ${c} width ${x.width} vs ${y.width}`);
    }
    const nr = Math.max(wa.rowCount, wb2.rowCount);
    for (let r = 1; r <= nr; r++) {
      if ((wa.getRow(r).height || 0) !== (wb2.getRow(r).height || 0)) diffs.push(`${wa.name}: row ${r} height differs`);
      for (let c = 1; c <= nc; c++) {
        const x = normCell(wa.getCell(r, c)), y = normCell(wb2.getCell(r, c));
        const sx = JSON.stringify(x), sy = JSON.stringify(y);
        if (sx !== sy) diffs.push(`${wa.name}!${wa.getCell(r, c).address}\n   A: ${sx}\n   B: ${sy}`);
      }
    }
  }
  if (diffs.length) { console.log(`DIFFERENT (${diffs.length}):`); diffs.slice(0, +process.env.MAXDIFF || 15).forEach(d => console.log(' -', d)); process.exit(1); }
  console.log('IDENTICAL');
})();
