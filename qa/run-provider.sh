set -euo pipefail
umask 077
BPIN=$(git -C "$BASE" rev-parse HEAD)
FPIN=$(git -C "$FRONTEND_CHECKOUT" rev-parse HEAD)
EPIN=$(git -C "$EXPORTER_CHECKOUT" rev-parse HEAD)
export BPIN FPIN EPIN
export MCMODS_AUDIT_EXPECT_BACKEND_COMMIT="$BPIN" MCMODS_AUDIT_EXPECT_FRONTEND_COMMIT="$FPIN" MCMODS_AUDIT_EXPECT_EXPORTER_COMMIT="$EPIN"
export MCMODS_AUDIT_FRONTEND_REPO="$FRONTEND_CHECKOUT" MCMODS_AUDIT_EXPORTER_REPO="$EXPORTER_CHECKOUT" MCMODS_AUDIT_TP008_FRONTEND_CHECKOUT="$FRONTEND_CHECKOUT" MCMODS_AUDIT_TP008_EXPORTER_CHECKOUT="$EXPORTER_CHECKOUT"
export MCMODS_AUDIT_TP008_DIR="$OUT" MCMODS_AUDIT_PG_LOCK="$RUNNER_TEMP/schema-setup.lock"
export MCMODS_RUN_DB_INTEGRATION=1 MCMODS_TEST_DATABASE_URL="$DATABASE_URL" MCMODS_SKIP_DOTENV=true
export NODE_ENV=development NEXT_TELEMETRY_DISABLED=1
if test "$CASE" = TestAuditLiveTP008Fixture; then
  export MCMODS_RUN_LIVE_TP008=1
  edge=3311
else
  export MCMODS_RUN_LIVE_TP008_WORKER=1
  edge=3551
fi
export NEXT_PUBLIC_API_BASE_URL="http://127.0.0.1:$edge" NEXT_PUBLIC_SITE_URL="http://127.0.0.1:$edge" NEXT_PUBLIC_YGGDRASIL_API_ROOT="http://127.0.0.1:$edge/api/yggdrasil/"
cd "$BASE"
go run ./cmd/db-initialize
go test -c ./internal/httpapi -o "$RUNNER_TEMP/provider.test"
cd "$BASE/internal/httpapi"
"$RUNNER_TEMP/provider.test" -test.v -test.count=1 -test.timeout=35m -test.run "^$CASE$" > "$RUNNER_TEMP/fixture.log" 2>&1 &
fixture=$!
next_pid=''
finish() {
  if test -n "$next_pid"; then kill "$next_pid" 2>/dev/null || true; fi
  if test -d "$OUT"; then touch "$OUT/STOP"; fi
  wait "$fixture" || true
}
trap finish EXIT
for i in $(seq 1 150); do
  if python3 - "$OUT/runtime.json" <<'PY' 2>/dev/null
import json,sys
assert json.load(open(sys.argv[1]))['state']=='READY'
PY
  then break; fi
  if ! kill -0 "$fixture" 2>/dev/null; then cat "$RUNNER_TEMP/fixture.log"; exit 1; fi
  sleep 1
done
test -s "$OUT/runtime.json"
cd "$FRONTEND_CHECKOUT"
NEXT_PUBLIC_API_BASE_URL=$(jq -r .apiOrigin "$OUT/runtime.json")
NEXT_PUBLIC_YGGDRASIL_API_ROOT="$NEXT_PUBLIC_API_BASE_URL/api/yggdrasil/"
node node_modules/next/dist/bin/next dev -H 127.0.0.1 -p "$edge" > "$RUNNER_TEMP/next.log" 2>&1 &
next_pid=$!
for i in $(seq 1 90); do
  if (echo > /dev/tcp/127.0.0.1/"$edge") 2>/dev/null; then break; fi
  sleep 1
done
curl --fail --silent --max-time 120 "$NEXT_PUBLIC_SITE_URL/plugins/new" > /dev/null
node "$HARNESS/qa/provider-browser.mjs"
touch "$OUT/STOP"
wait "$fixture"
grep -F -- "--- PASS: $CASE" "$RUNNER_TEMP/fixture.log"
python3 - "$OUT" <<'PY'
import json,sys,pathlib
p=pathlib.Path(sys.argv[1]);r=json.loads((p/'runtime.json').read_text());j=json.loads((p/'acceptance.json').read_text())
assert r['state']=='STOPPED' and not r['snapshotError'] and j['status']=='PASS' and len(j['accepted'])==11
jobs=r['jobs']
assert len(jobs) in (5,11) and all(x['status']=='completed' for x in jobs)
j['job_total']=len(jobs);j['broker']='isolated file-backed JetStream';j['fixture_terminal']='PASS';j['snapshot_terminal']='STOPPED'
(p/'acceptance.json').write_text(json.dumps(j,indent=2)+'\n')
PY
