#!/usr/bin/env bash
# Dedicated, disposable, loopback-only PostgreSQL/Redis/NATS services.
set -euo pipefail
umask 077
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

action=${1:-start}
if [[ "$action" == start ]]; then
  : "${PG_BIN:?Set PG_BIN to the PostgreSQL bin directory}"
  : "${REDIS_SERVER:?Set REDIS_SERVER to the redis-server executable}"
  : "${NATS_SERVER:?Set NATS_SERVER to the nats-server executable}"
  command -v nohup >/dev/null
  command -v setsid >/dev/null
  state=$(mktemp -d "${TMPDIR:-/tmp}/mcmods-test-services.XXXXXXXX")
  printf '%s\n' "$state" > "$state/owned-directory"
  export MCMODS_TEST_STATE="$state"
  python3 - <<'PY'
import os, secrets, shlex, json
from pathlib import Path
p = Path(os.environ['MCMODS_TEST_STATE'])
password = secrets.token_hex(24)
env = {
    'MCMODS_TEST_STATE': str(p),
    'APP_ENV': 'test', 'APP_ADDR': '127.0.0.1:18080',
    'FRONTEND_ORIGIN': 'http://127.0.0.1:13000',
    'DB_HOST': '127.0.0.1', 'DB_PORT': '55432', 'DB_NAME': 'mcmods_audit',
    'DB_USER': 'mcmods_audit', 'DB_PASSWORD': password, 'DB_SSLMODE': 'disable',
    'DATABASE_URL': '', 'DB_RESET_ON_START': 'false', 'DB_RESET_CONFIRM': '',
    'JWT_SECRET': secrets.token_hex(32), 'SETTINGS_ENCRYPTION_KEY': secrets.token_hex(32),
    'ANTI_ABUSE_HMAC_SECRET': secrets.token_hex(32), 'ANTI_ABUSE_IP_HASH_SECRET': secrets.token_hex(32),
    'SEED_ADMIN_PASSWORD': secrets.token_hex(24), 'SMTP_HOST': '',
    'REDIS_ENABLED': 'true', 'REDIS_REQUIRED': 'true',
    'REDIS_ADDR': '127.0.0.1:56379', 'REDIS_PASSWORD': secrets.token_hex(24),
    'REDIS_NAMESPACE': 'audit_isolated', 'REDIS_DB': '0',
    'NATS_ENABLED': 'true', 'NATS_URL': 'nats://127.0.0.1:54222',
    'NATS_JETSTREAM_ENABLED': 'true', 'NATS_JETSTREAM_STREAM': 'MCMODS_AUDIT_TASKS',
    'NATS_USERNAME': '', 'NATS_PASSWORD': '', 'NATS_TOKEN': '',
    'TYPESENSE_ENABLED': 'false', 'YGGDRASIL_ENABLED': 'false',
    'MCMODS_RUN_TYPESENSE_INTEGRATION': '',
    'MCMODS_TEST_TYPESENSE_URL': '', 'MCMODS_TEST_TYPESENSE_KEY': '',
    'MODRINTH_TOKEN': '', 'CURSEFORGE_API_KEY': '', 'GITHUB_TOKEN': '',
    'TURNSTILE_SECRET_KEY': '',
    'MCMODS_RUN_DB_INTEGRATION': '1', 'MCMODS_RUN_NATS_INTEGRATION': '1',
    'MCMODS_TEST_DATABASE_URL': f'postgres://mcmods_audit:{password}@127.0.0.1:55432/mcmods_audit?sslmode=disable',
    'MCMODS_TEST_NATS_URL': 'nats://127.0.0.1:54222',
}
(p / 'env.sh').write_text(''.join(f'export {k}={shlex.quote(v)}\n' for k,v in env.items()))
(p / 'pg-password').write_text(password+'\n')
(p / 'redis.conf').write_text(f'bind 127.0.0.1\nport 56379\nprotected-mode yes\nrequirepass {env["REDIS_PASSWORD"]}\ndir {p}\npidfile {p}/redis.pid\nlogfile {p}/redis.log\nsave ""\n')
(p / 'nats.conf').write_text(f'host: "127.0.0.1"\nport: 54222\npid_file: "{p}/nats.pid"\njetstream {{ store_dir: "{p}/jetstream" }}\n')
(p / 'identity.json').write_text(json.dumps({'directory':str(p),'uid':os.getuid(),'database':'mcmods_audit','pg_bin':os.environ['PG_BIN']})+'\n')
if os.environ.get('TYPESENSE_SERVER'):
    typesense_key = secrets.token_hex(24)
    (p/'typesense').mkdir()
    (p/'typesense-key').write_text(typesense_key+'\n')
    (p/'typesense.conf').write_text('[server]\napi-key='+typesense_key+'\ndata-dir='+str(p/'typesense')+'\napi-address=127.0.0.1\napi-port=58108\npeering-address=127.0.0.1\npeering-port=58107\nthread-pool-size=2\nenable-access-logging=false\nenable-search-logging=false\n')
    with (p/'env.sh').open('a') as output:
        for key,value in {'MCMODS_RUN_TYPESENSE_INTEGRATION':'1','MCMODS_TEST_TYPESENSE_URL':'http://127.0.0.1:58108','MCMODS_TEST_TYPESENSE_KEY':typesense_key}.items():
            output.write(f'export {key}={shlex.quote(value)}\n')
PY
  "$PG_BIN/initdb" -D "$state/postgres" -U mcmods_audit --auth-local=trust --auth-host=scram-sha-256 --pwfile="$state/pg-password" > "$state/initdb.log"
  "$PG_BIN/pg_ctl" -D "$state/postgres" -l "$state/postgres.log" -o "-h 127.0.0.1 -p 55432 -k $state" -w start
  # The socket belongs to this newly initialized cluster, never a default server.
  "$PG_BIN/createdb" -h "$state" -p 55432 -U mcmods_audit mcmods_audit
  # A separate session survives the task runner ending this launch shell's
  # process group. The readiness checker still requires this exact child PID.
  nohup setsid "$REDIS_SERVER" "$state/redis.conf" </dev/null > "$state/redis-start.log" 2>&1 &
  printf '%s\n' "$!" > "$state/redis-started.pid"
  nohup setsid "$NATS_SERVER" -c "$state/nats.conf" </dev/null > "$state/nats.log" 2>&1 &
  printf '%s\n' "$!" > "$state/nats-started.pid"
  if [[ -n "${TYPESENSE_SERVER:-}" ]]; then
    nohup setsid "$TYPESENSE_SERVER" --config="$state/typesense.conf" </dev/null > "$state/typesense.log" 2>&1 &
    printf '%s\n' "$!" > "$state/typesense.pid"
    cp "$state/typesense.pid" "$state/typesense-started.pid"
  fi
  python3 "$script_dir/service-process-owner.py" ready "$state"
  printf 'Created disposable test services; environment: %s/env.sh\n' "$state"
elif [[ "$action" == stop ]]; then
  state=${2:?Supply the exact directory printed by start}
  python3 "$script_dir/service-process-owner.py" stop "$state"
  pg_bin=$(python3 - "$state" <<'PY'
import json,sys
from pathlib import Path
print(json.loads((Path(sys.argv[1])/'identity.json').read_text())['pg_bin'])
PY
)
  "$pg_bin/pg_ctl" -D "$state/postgres" -m fast -w stop
  printf 'Stopped owned services; retained evidence in %s\n' "$state"
else
  printf 'Usage: %s start | stop <owned-directory>\n' "$0" >&2
  exit 2
fi
