const http = require('http');
function h(s){let x=0;for(const c of s)x=(x*31+c.charCodeAt(0))>>>0;return x;}
http.createServer((req,res)=>{ let b=''; req.on('data',d=>b+=d); req.on('end',()=>{
  const p=JSON.parse(b); const mats=p.DATA.filter(d=>d.MATNR).map(d=>d.MATNR);
  const rows=[]; for(const m of mats){ const x=h(m); if(x%5===0) continue; // some unmatched
    const row={ MATNR: x%3===0 ? '000'+m : m, VNAME: x%4===0?'':('VENDOR'+(x%7)), LIFNR: x%2?('L'+(x%90)):'0', LABST: (x%11===0)?'1,250.5':(x%6)*100, MMSTA: x%13===0?'Z1':(x%9===0?'Z2':'') };
    if (x%17===0) { delete row.LABST; row.STOCK = 42; }
    rows.push(row);}
  const shape=h(mats.join(','))%3;
  const body = shape===0 ? {tables:[{rows}]} : shape===1 ? {DATA:rows} : rows;
  res.setHeader('content-type','application/json'); res.end(JSON.stringify(body)); }); }).listen(+process.argv[2]);
