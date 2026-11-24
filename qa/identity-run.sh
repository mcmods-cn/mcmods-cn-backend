set -euo pipefail
umask 077
state="$OUT"; work="$RUNNER_TEMP"; pub="$work/identity-public"; mkdir -m 700 "$pub"
export MCMODS_RUN_DB_INTEGRATION=1 MCMODS_TEST_DATABASE_URL="$DATABASE_URL" MCMODS_SKIP_DOTENV=true SMTP_ENABLED=false REDIS_ENABLED=false REDIS_REQUIRED=false NATS_ENABLED=false NATS_REQUIRED=false NATS_JETSTREAM_ENABLED=false
export FRONTEND_ORIGIN=http://127.0.0.1:3311 MCMODS_AUDIT_PG_LOCK="$work/schema.lock" NEXT_TELEMETRY_DISABLED=1
export MCMODS_AUDIT_EXPORTER_REPO="$EXPORTER_CHECKOUT"; export MCMODS_AUDIT_EXPECT_BACKEND_COMMIT; MCMODS_AUDIT_EXPECT_BACKEND_COMMIT=$(git -C "$BASE" rev-parse HEAD)
case "$CASE" in
 ai) name=TestAIConfigLiveBrowserFixture; port=3329; route=/admin;export MCMODS_RUN_AI_CONFIG_BROWSER=1 MCMODS_AI_CONFIG_BROWSER_DIR="$state";;
 fid) name=TestIndependentP1FID001LiveBrowserFixture; port=3311; route='/user?section=economy';export MCMODS_RUN_LIVE_TRANSFER=1 MCMODS_AUDIT_TRANSFER_DIR="$state" MCMODS_AUDIT_TRANSFER_AUTH="$work/private-transfer";;
 skin) name=TestAuditLivePrivateSkinBrowserFixture; port=3311;export MCMODS_RUN_LIVE_PRIVATE_SKIN=1 MCMODS_AUDIT_PRIVATE_SKIN_DIR="$state" MCMODS_AUDIT_PRIVATE_SKIN_AUTH="$work/private-skin";;
 creator)
  name=TestAuditLiveOSS001CreatorAvatar20261101Fixture;port=3465;route=/login
  export MCMODS_RUN_LIVE_OSS001_CREATOR_20261101=1 MCMODS_AUDIT_OSS001_DIR="$state" MCMODS_AUDIT_OSS001_PRIVATE_AUTH="$work/private-creator" MCMODS_AUDIT_OSS001_FRONTEND_REPO="$FRONTEND_CHECKOUT"
  openssl req -new -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -keyout "$work/ca.key" -out "$work/ca.pem" -days 2 -subj '/CN=MCMods fixture CA' >/dev/null 2>&1
  openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -keyout "$work/leaf.key" -out "$work/leaf.csr" -subj '/CN=127.0.0.1' >/dev/null 2>&1
  printf '%s\n' 'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature' 'extendedKeyUsage=serverAuth' 'subjectAltName=IP:127.0.0.1' > "$work/leaf.ext"
  openssl x509 -req -in "$work/leaf.csr" -CA "$work/ca.pem" -CAkey "$work/ca.key" -CAcreateserial -days 2 -out "$work/leaf.pem" -extfile "$work/leaf.ext" > /dev/null 2>&1
  cat /etc/ssl/certs/ca-certificates.crt "$work/ca.pem" > "$work/cas.pem"
  export SSL_CERT_FILE="$work/cas.pem" NODE_EXTRA_CA_CERTS="$work/ca.pem"
  export MCMODS_AUDIT_EXPECT_TLS_LEAF_SHA256; MCMODS_AUDIT_EXPECT_TLS_LEAF_SHA256=$(openssl x509 -in "$work/leaf.pem" -outform DER | sha256sum | cut -d' ' -f1)
  openssl x509 -in "$work/leaf.pem" -pubkey -noout | openssl pkey -pubin -outform DER 2>/dev/null | openssl dgst -sha256 -binary | base64 > "$work/spki";;
 *) exit 2;;
esac
cd "$BASE"; go run ./cmd/db-initialize; go test -c -o "$work/identity.test" ./internal/httpapi
cd "$BASE/internal/httpapi"
go tool test2json -t -p mcmods-cn-backend/internal/httpapi "$work/identity.test" -test.v=test2json -test.count=1 -test.run "^$name$" -test.timeout=26m > "$work/go.private" 2>&1 & service=$!
front=''; gateway=''; finished=''
finish(){
  test -z "$front" || kill "$front" 2>/dev/null || true
  test -z "$gateway" || kill "$gateway" 2>/dev/null || true
  if test -z "$finished"; then
    kill "$service" 2>/dev/null || true
    set +e; wait "$service"; code=$?; set -e; echo "$code" > "$work/go.exit"
  fi
  python3 "$HARNESS/qa/identity-result.py" "$work" "$name" "$BASE" "$FRONTEND_CHECKOUT"
}
trap finish EXIT
for i in $(seq 1 120); do test ! -s "$state/runtime.json" || break;kill -0 "$service" || exit 1;sleep 1;done
test -s "$state/runtime.json"
export NEXT_PUBLIC_API_BASE_URL NEXT_PUBLIC_SITE_URL NEXT_PUBLIC_YGGDRASIL_API_ROOT
NEXT_PUBLIC_API_BASE_URL=$(jq -r .apiOrigin "$state/runtime.json"); NEXT_PUBLIC_SITE_URL=$(jq -r .frontendOrigin "$state/runtime.json")
if test "$CASE" = creator; then
 node "$HARNESS/qa/identity-tls.mjs" "$state" > "$work/gateway.log" 2>&1 & gateway=$!
 NEXT_PUBLIC_API_BASE_URL="$NEXT_PUBLIC_SITE_URL"
 for i in $(seq 1 120); do test "$(jq -r .state "$state/runtime.json")" != READY || break;kill -0 "$service" || exit 1;sleep 1;done
 test "$(jq -r .state "$state/runtime.json")" = READY
fi
NEXT_PUBLIC_YGGDRASIL_API_ROOT="$NEXT_PUBLIC_API_BASE_URL/api/yggdrasil/"
if test "$CASE" = skin; then route="/skins/$(jq -r .skinPublicId "$state/runtime.json")"; fi
cd "$FRONTEND_CHECKOUT"; node node_modules/next/dist/bin/next dev -H 127.0.0.1 -p "$port" > "$work/next.private" 2>&1 & front=$!
for i in $(seq 1 50); do (echo >/dev/tcp/127.0.0.1/"$port") 2>/dev/null && break; sleep 1; done
curl --fail --silent --max-time 160 "$NEXT_PUBLIC_SITE_URL$route" >/dev/null
set +e
if test "$CASE" = ai; then node "$BASE/tools/testing/ai_configuration_browser.mjs" "$state" "$FRONTEND_CHECKOUT" > "$work/browser.private" 2>&1
elif test "$CASE" = creator; then node "$HARNESS/qa/identity-creator.mjs" "$state" > "$work/browser.private" 2>&1
else node "$HARNESS/qa/identity-browser.mjs" "$state" "$CASE" > "$work/browser.private" 2>&1; fi
browser_code=$?; echo "$browser_code" > "$work/browser.exit"; test "$browser_code" -eq 0 || exit "$browser_code"
wait "$service"; go_code=$?; finished=1; echo "$go_code" > "$work/go.exit"; set -e; test "$go_code" -eq 0
jq -e -s --arg n "$name" 'any(.[];.Action=="pass" and .Test==$n) and all(.[];.Action!="skip" and .Action!="fail")' "$work/go.private" >/dev/null
cp "$state/browser-result.json" "$pub/"; for file in "$state"/*.png "$state/backend-postconditions.json"; do test ! -f "$file" || cp "$file" "$pub/"; done
if test "$CASE" = creator; then
 jq -e '.state=="STOPPED" and (.independentNativeReviewReceipts|length)==4' "$state/runtime.json" >/dev/null
 jq '{state,draftObservations,finalOwnedDatabaseFacts,privateProtocolObservations,reviewCount:(.independentNativeReviewReceipts|length)}' "$state/runtime.json" > "$pub/backend-postconditions.json"
fi
