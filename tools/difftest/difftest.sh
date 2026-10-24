#!/bin/bash
# Differential test: random BOM/Priority files (.xlsx and .xls) are processed by the ORIGINAL
# Node pipeline and by the Go port (with the same stubbed Material Master and a fake SAP server);
# the two output workbooks are compared cell by cell (values, fills, fonts, borders, alignment,
# number formats, widths, panes) and their log lines are diffed.
#
# Setup (once):  git clone/unzip the Node project to $REF, then in $REF run
#   npm install exceljs xlsx@0.18.5 axios zod     (xlsx 0.18.5 = same parser API as the 0.20.3 tarball)
#   go build -o bomrun ./cmd/bomrun               (from the Go project)
# Run:  REF=/path/to/miservice-main BOMRUN=./bomrun tools/difftest/difftest.sh 1 100
# usage: difftest.sh <fromSeed> <toSeed>
REF=${REF:-/tmp/ref}; BOMRUN=${BOMRUN:-/tmp/bomrun}; export REF
HERE=$(cd "$(dirname "$0")" && pwd)
WORK=${WORK:-$HERE/work}; mkdir -p "$WORK/cases"; cd "$WORK"
cp "$HERE"/*.js . 
node fakesap.js 4599 & SAP=$!; sleep 1
fail=0
for seed in $(seq $1 $2); do
  node -e "require('./gen.js')($seed,'cases')"
  for ext in xlsx xls; do
    CFG='{"db_enabled":true,"sap_enabled":true,"sap_host":"127.0.0.1","sap_port":4599,"sap_tcode":"T1","sap_werks":"1000","api_batch_size":5}'
    STUBDB=cases/stub_$seed.json node run_ref.js cases/bom_$seed.$ext cases/prio_$seed.$ext cases/node_${seed}_$ext.xlsx "$CFG" > cases/node_${seed}_$ext.log 2>&1 || { echo "seed $seed $ext: node failed"; tail -2 cases/node_${seed}_$ext.log; }
    $BOMRUN -bom cases/bom_$seed.$ext -prio cases/prio_$seed.$ext -out cases/go_${seed}_$ext.xlsx -cfg "$CFG" -stubdb cases/stub_$seed.json > cases/go_${seed}_$ext.log 2>&1 || { echo "seed $seed $ext: go failed"; tail -2 cases/go_${seed}_$ext.log; }
    r=$(EXCELJS=$REF/node_modules/exceljs MAXDIFF=3 node ./compare_xlsx.js cases/node_${seed}_$ext.xlsx cases/go_${seed}_$ext.xlsx 2>&1)
    l=$(diff <(grep -v 'Successfully processed' cases/node_${seed}_$ext.log | sed 's/^LOG: //' | grep -v '^done') <(grep -v 'Successfully processed' cases/go_${seed}_$ext.log | sed 's/^LOG: //' | grep -v '^done') >/dev/null && echo same || echo LOGDIFF)
    if [ "$r" != "IDENTICAL" ] || [ "$l" != "same" ]; then echo "seed $seed $ext: $l"; echo "$r" | head -12; fail=1; fi
  done
done
kill $SAP 2>/dev/null
[ $fail = 0 ] && echo "ALL IDENTICAL ($1..$2)"
