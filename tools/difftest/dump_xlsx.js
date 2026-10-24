// Dumps an xlsx (via ExcelJS) to a normalized JSON structure for cell-level comparison.
const ExcelJS = require((process.env.REF||'/tmp/ref')+'/node_modules/exceljs');
async function dump(file) {
  const wb = new ExcelJS.Workbook();
  await wb.xlsx.readFile(file);
  const out = { sheets: [] };
  wb.eachSheet(ws => {
    const s = { name: ws.name, views: ws.views, columns: [], rowHeights: {}, cells: [] };
    ws.columns.forEach((c, i) => s.columns.push({ n: i + 1, width: c.width, style: c.style && c.style.alignment }));
    ws.eachRow({ includeEmpty: true }, (row, rn) => {
      if (row.height) s.rowHeights[rn] = row.height;
      row.eachCell({ includeEmpty: true }, (cell, cn) => {
        const v = cell.value;
        s.cells.push({
          a: cell.address, t: cell.type, v: (v === null || v === undefined) ? null : (typeof v === 'object' ? JSON.stringify(v) : v),
          fill: cell.fill || null, font: cell.font || null, border: cell.border || null,
          align: cell.alignment || null, numFmt: cell.numFmt || null,
        });
      });
    });
    out.sheets.push(s);
  });
  return out;
}
if (require.main === module) dump(process.argv[2]).then(o => console.log(JSON.stringify(o, null, 1)));
module.exports = { dump };
