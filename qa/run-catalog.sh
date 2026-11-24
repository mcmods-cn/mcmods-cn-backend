set -euo pipefail
umask 077
export BPIN=$(git -C "$BASE" rev-parse HEAD) FPIN=$(git -C "$FRONTEND_CHECKOUT" rev-parse HEAD) EPIN=$(git -C "$EXPORTER_CHECKOUT" rev-parse HEAD)
export MCMODS_AUDIT_EXPECT_BACKEND_COMMIT="$BPIN" MCMODS_AUDIT_FRONTEND_REPO="$FRONTEND_CHECKOUT" MCMODS_AUDIT_EXPORTER_REPO="$EXPORTER_CHECKOUT"
export MCMODS_TEST_DATABASE_URL="$DATABASE_URL" MCMODS_RUN_DB_INTEGRATION=1 MCMODS_SKIP_DOTENV=true MCMODS_AUDIT_PG_LOCK="$RUNNER_TEMP/catalog-db.lock"
case "$CASE" in
  TestAuditLiveRecipeCatalogFixture) kind=recipe;edge=3341;export MCMODS_RUN_LIVE_RECIPE_CATALOG=1 MCMODS_AUDIT_RECIPE_CATALOG_DIR="$OUT";;
  TestAuditLiveRecipeGeometryFixture) kind=geometry;edge=3361;export MCMODS_RUN_LIVE_RECIPE_GEOMETRY=1 MCMODS_AUDIT_RECIPE_GEOMETRY_DIR="$OUT" MCMODS_AUDIT_GEOMETRY_WITH_ASSETS=1 MCMODS_AUDIT_GEOMETRY_VERIFY_WITHDRAWAL=1;;
  TestAuditLiveGlobalCatalogFixture) kind=global;edge=3311;export MCMODS_RUN_LIVE_GLOBAL_CATALOG=1 MCMODS_AUDIT_GLOBAL_CATALOG_DIR="$OUT";;
  *) exit 81;;
esac
export NODE_ENV=development NEXT_TELEMETRY_DISABLED=1
export MCMODS_AUDIT_FRONTEND_ORIGIN="http://127.0.0.1:$edge" NEXT_PUBLIC_API_BASE_URL="http://127.0.0.1:$edge" NEXT_PUBLIC_SITE_URL="http://127.0.0.1:$edge" NEXT_PUBLIC_YGGDRASIL_API_ROOT="http://127.0.0.1:$edge/api/yggdrasil/"
export MCMODS_AUDIT_RECIPE_RUNTIME="$OUT/runtime.json" MCMODS_AUDIT_FRONTEND_CHECKOUT="$FRONTEND_CHECKOUT" CATALOG_KIND="$kind"
cd "$BASE";go run ./cmd/db-initialize;go test -c ./internal/httpapi -o "$RUNNER_TEMP/catalog.test"
cd "$BASE/internal/httpapi"
"$RUNNER_TEMP/catalog.test" -test.v -test.count=1 -test.timeout=34m -test.run "^$CASE$" >"$RUNNER_TEMP/catalog.fixture.private.log" 2>&1 & fixture=$!
next_pid='' mounted=''
finish(){
 if test -n "$mounted";then kill "$mounted" 2>/dev/null || true;fi
 if test -n "$next_pid";then kill "$next_pid" 2>/dev/null || true;fi
 if test -e "$OUT/OWNER";then touch "$OUT/STOP";fi
 wait "$fixture" || true
}
trap finish EXIT
cd "$FRONTEND_CHECKOUT"
node node_modules/next/dist/bin/next dev -H 127.0.0.1 -p 3991 >"$RUNNER_TEMP/catalog.next.private.log" 2>&1 & next_pid=$!
for i in $(seq 1 360);do
 if test -s "$OUT/runtime.json" && python3 -m json.tool "$OUT/runtime.json" >/dev/null 2>&1;then break;fi
 if ! kill -0 "$fixture" 2>/dev/null;then echo "fixture preparation failed";exit 1;fi
 sleep 1
done
test -s "$OUT/runtime.json"
node "$HARNESS/qa/mount-catalog.mjs" "$OUT/runtime.json" >"$RUNNER_TEMP/catalog.mount.log" 2>&1 & mounted=$!
for i in $(seq 1 40);do if curl -fsS --max-time 12 "$MCMODS_AUDIT_FRONTEND_ORIGIN/recipe-types" >/dev/null;then break;fi;sleep 1;done
if test "$kind" = global;then
 node "$HARNESS/qa/global-catalog-browser.mjs"
else
 python3 "$HARNESS/qa/derive-recipe.browser.py" "$FRONTEND_CHECKOUT/browser-tests/audit-recipe-live.mjs" "$RUNNER_TEMP/catalog-browser.mjs" "$kind"
 xvfb-run -a node "$RUNNER_TEMP/catalog-browser.mjs"
fi
touch "$OUT/STOP";wait "$fixture"
grep -F -- "--- PASS: $CASE" "$RUNNER_TEMP/catalog.fixture.private.log"
python3 - "$OUT" "$kind" <<'PY'
import json,os,sys,pathlib,hashlib
p=pathlib.Path(sys.argv[1]);kind=sys.argv[2];r=json.loads((p/'runtime.json').read_text())
assert r.get('state')=='STOPPED'
j=json.loads((p/'ui-operations.json' if kind=='global' else next(p.glob('browser-recipe-*/browser-result.json'))).read_text());assert j['status'].startswith('PASS')
if kind=='geometry': assert r['independentWithdrawalVerified'] and len(j['geometryCases'])>=6
if kind=='recipe': assert r['sampleSHA256']=='059b1c56921a3f70b934bd577fd940486982da03b24d2f6dca7c0efcfb5af2eb' and len(j['milestones'])>=12
summary={'case':os.environ['CASE'],'source_backend':os.environ['BPIN'],'source_frontend':os.environ['FPIN'],'source_exporter':os.environ['EPIN'],'runner':'GitHub hosted PostgreSQL, ordinary Go fixture, current Next dev/Chromium through faithful native loopback mount; actual OCI is verified separately','sample_sha256':r.get('sampleSHA256'),'producer_commit':r.get('actualGameProducerCommit'),'fixture_status':r['state'],'test_passed':True,'browser_status':j['status'],'milestones':len(j.get('milestones',[])),'geometry_checks':len(j.get('geometryCases',[])),'actual_worker':'direct authorized invocation without NATS' if kind!='global' else 'ordinary authoring and independent review','sample_type': 'Unmodified real Fabric/JEI archive plus explicitly synthetic mixed fixture' if kind=='recipe' else 'synthetic fixture'}
(p/'acceptance.json').write_text(json.dumps(summary,indent=2)+'\n')
PY
