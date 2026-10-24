// Random BOM / Priority generator -> xlsx + xls via SheetJS
const XLSX=require((process.env.REF||'/tmp/ref')+'/node_modules/xlsx'); const fs=require('fs');
function rng(seed){let s=seed>>>0;return()=>{s=(s*1664525+1013904223)>>>0;return s/4294967296;};}
module.exports=function gen(seed, dir){
  const R=rng(seed), pick=a=>a[Math.floor(R()*a.length)], int=n=>Math.floor(R()*n);
  const vendors=['MURATA','AVX','YAGEO CORPORATION','TDK','SAMSUNG ELECTRO-MECHANICS','walsin technology corporation','KDS','TDK CORP','Vishay',' panasonic ','SEMCO',''];
  const descs=['CAP CERAMIC 0.1UF','RES 10K 1%','IC MCU','INDUCTOR POWER','CONN HEADER','PCB, Main Board','pcb assy','DIODE','','CAP TANT'];
  const mpn=()=>pick(['GRM188R71H104KA93D','CGA3E2X7R1H104K080AA','CL10B104KB8NNNC','CL21A106KAYNNNE','CL31P226MQHNNNE','RC0402FR-0710KL','LM358','abc-123 ','ABC-123','NOT-CGA-1','TPS5430','']);
  const fpns=[]; for(let i=0;i<5;i++) fpns.push('F'+(1000+i));
  const rows=[]; const W=53;
  const hdr=new Array(W).fill(''); Object.entries({0:'Level',1:'Part Number',2:'Part Type',3:'Description',8:'Rev',25:'BOM.Qty',26:'BOM.UOM',28:'BOM.Find Num',29:'BOM.Ref Des',51:'Manufacturers.Mfg Name',52:'Manufacturers.Mfr. Part Number'}).forEach(([k,v])=>hdr[k]=v);
  const useHdr = R()>0.15; const lead = useHdr? int(3):0;
  for(let i=0;i<lead;i++) rows.push(new Array(W).fill(''));
  if(useHdr) rows.push(hdr);
  const hasPcb=R()>0.7; const n=15+int(60); let find=0;
  const base = new Array(W).fill('');
  for(let i=0;i<n;i++){
    const r=new Array(W).fill('');
    if(i===0 && R()>0.2){ r[0]= R()>0.5?0:'0'; r[1]='TOP-'+seed; r[2]='ASSY'; r[3]='Top assembly'; r[8]='A'; rows.push(r); continue; }
    const newGroup=R()>0.45;
    if(newGroup){ find+=1; r[28]= R()>0.5? find : String(find); r[0]=pick([1,'1','',2]); r[1]=pick(['P-'+find,'',' PN'+find+' ']); r[2]=pick(['COTS','','MECH']); r[3]=pick(descs.filter(d=>hasPcb||!/pcb/i.test(d))); r[8]=pick(['','B','B2 x','C',1]); r[25]=pick([1,2,'0',0,'',' 3 ','1,000',4.5]); r[26]=pick(['EA','','PC']); }
    else if(R()>0.7){ r[28]=''; }
    if(R()>0.2) r[29]=pick(['C1','R2, R3','','U5']);
    r[51]=pick(vendors); r[52]=pick([mpn(), mpn(), mpn(), hasPcb?'PCB-'+int(3)+'-X':mpn()]);
    if(R()>0.9) r[52]=int(100000); 
    if(R()>0.95){ r[52]=''; r[51]=''; r[29]=''; if(R()>0.5) continue; }
    rows.push(r);
  }
  const bomWs=XLSX.utils.aoa_to_sheet(rows);
  // priority
  const pr=[]; for(let i=0;i<167;i++) pr.push(i%50===0?['x','y']:[]);
  fpns.forEach(f=>{ const row=new Array(13).fill(''); row[0]=pick([f,f.toLowerCase(),' '+f]); row[12]=pick([1,1,'1','P1','01.0',2,'1.0']); pr.push(row); });
  const prWs=XLSX.utils.aoa_to_sheet(pr);
  const stub={}; fpns.forEach((f,i)=>{}); 
  // stub DB: MPN -> materialNo from fpns
  ['GRM188R71H104KA93D','CGA3E2X7R1H104K080AA','CL10B104KB8NNNC','LM358','ABC-123','TPS5430','PCB-1-X','PCB-2-X_SANMINA','RC0402FR-0710KL','CL31P226MQHNNNE'].forEach((m,i)=>{ stub[m.toUpperCase()]={materialNo:fpns[(i*3+seed)%fpns.length], zBlock: i%6===5?'Z1':''}; });
  stub['TOP-'+seed+'_REV_A']={materialNo:fpns[0],zBlock:''};
  for (const bt of ['xlsx','biff8']) {
    const ext=bt==='xlsx'?'xlsx':'xls';
    const wb=XLSX.utils.book_new(); XLSX.utils.book_append_sheet(wb,bomWs,'BOM'); XLSX.writeFile(wb,`${dir}/bom_${seed}.${ext}`,{bookType:bt});
    const wp=XLSX.utils.book_new(); XLSX.utils.book_append_sheet(wp,XLSX.utils.aoa_to_sheet([['junk']]),'Other'); XLSX.utils.book_append_sheet(wp,prWs,'Shortage'); XLSX.writeFile(wp,`${dir}/prio_${seed}.${ext}`,{bookType:bt});
  }
  fs.writeFileSync(`${dir}/stub_${seed}.json`,JSON.stringify(stub));
};
