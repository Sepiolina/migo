const path = require('path'), fs = require('fs');
const root = process.env.REF || '/tmp/ref';
// stub the DB module so no mssql is needed
const dbPath = require.resolve(root + '/src/features/bom-processor/db.js');
let stubMap = {};
if (process.env.STUBDB) stubMap = JSON.parse(fs.readFileSync(process.env.STUBDB, 'utf8'));
const { norm } = require(root + '/src/shared/helpers');
require.cache[dbPath] = { id: dbPath, filename: dbPath, loaded: true, exports: {
  lookupMaterialMaster: async (mpns) => { const m = new Map(); for (const x of mpns) { const k = norm(x); if (stubMap[k]) m.set(k, stubMap[k]); } return m; },
  closePool: async () => {},
}};
const { processPipeline } = require(root + '/src/features/bom-processor/pipeline');
const [,, bom, prio, out, cfgJson] = process.argv;
const cfg = Object.assign({ db_enabled: false, sap_enabled: false }, cfgJson ? JSON.parse(cfgJson) : {});
processPipeline(bom, prio, out, cfg, (m) => console.log('LOG:', m)).then(() => console.log('done')).catch(e => { console.error('ERR', e.message); process.exit(1); });
