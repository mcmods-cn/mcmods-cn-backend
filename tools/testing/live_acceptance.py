#!/usr/bin/env python3
"""Real-API acceptance: fresh owned child DB, current Go + independent Next dev."""
import argparse
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import selectors
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

BE = Path(__file__).resolve().parents[2]
FE = STATE = SCOPE = COPY = BASE = None
ADOPT_PREPARED_COPY = False
SCOPE_CREATED_THIS_CALL = False
CLEANUP_GO_SOURCE = r'''
// Private test-resource cleanup; Python verifies container owners and endpoints.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := run(); err != nil {
		// Avoid rendering sensitive provider URLs or environment values.
		fmt.Fprintln(os.Stderr, "exact owned namespace cleanup failed; inspect private input/guards")
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return errors.New("private environment file is required")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	var env map[string]string
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	namespace := env["REDIS_NAMESPACE"]
	if !regexp.MustCompile(`^live_[a-f0-9]{32}$`).MatchString(namespace) {
		return errors.New("invalid owned namespace")
	}
	nonce := strings.TrimPrefix(namespace, "live_")
	stream := "LIVE_" + strings.ToUpper(nonce)
	if env["NATS_JETSTREAM_STREAM"] != stream || env["NATS_SUBJECT_PREFIX"] != "live."+nonce {
		return errors.New("NATS namespace mismatch")
	}
	if env["REDIS_PREFIX"] != "" && env["REDIS_PREFIX"] != "mcmods" {
		return errors.New("unexpected Redis prefix")
	}
	if env["REDIS_DB"] != "" && env["REDIS_DB"] != "0" {
		return errors.New("unexpected Redis DB")
	}
	for _, key := range []string{"REDIS_USERNAME", "REDIS_PASSWORD", "NATS_TOKEN", "NATS_USERNAME", "NATS_PASSWORD"} {
		if env[key] != "" {
			return errors.New("credentials are not expected in this fresh scope")
		}
	}
	host, _, err := net.SplitHostPort(env["REDIS_ADDR"])
	if err != nil || host != "127.0.0.1" {
		return errors.New("Redis is not loopback")
	}
	u, err := url.Parse(env["NATS_URL"])
	if err != nil || u.Scheme != "nats" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("NATS is not a plain loopback endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: env["REDIS_ADDR"], DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second})
	defer client.Close()
	prefix := "mcmods:" + namespace + ":"
	var removed int64
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return err
		}
		for _, key := range keys {
			if !strings.HasPrefix(key, prefix) {
				return errors.New("Redis key escaped scope")
			}
		}
		if len(keys) > 0 {
			count, err := client.Del(ctx, keys...).Result()
			if err != nil {
				return err
			}
			removed += count
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	cursor = 0
	for {
		left, next, err := client.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil || len(left) > 0 {
			return errors.New("Redis own keys remain")
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	nc, err := nats.Connect(env["NATS_URL"], nats.Timeout(3*time.Second), nats.NoReconnect())
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := nc.JetStream(nats.MaxWait(3 * time.Second))
	if err != nil {
		return err
	}
	info, err := js.StreamInfo(stream, nats.Context(ctx))
	streamRemoved := false
	if !errors.Is(err, nats.ErrStreamNotFound) {
		if err != nil {
			return err
		}
		if info.Config.Name != stream || len(info.Config.Subjects) != 1 || info.Config.Subjects[0] != "live."+nonce+".>" {
			return errors.New("stream subjects are not the exact owned namespace")
		}
		if err := js.DeleteStream(stream, nats.Context(ctx)); err != nil {
			return err
		}
		streamRemoved = true
	}
	if _, err := js.StreamInfo(stream, nats.Context(ctx)); !errors.Is(err, nats.ErrStreamNotFound) {
		return errors.New("stream deletion was not confirmed")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"redis_removed_keys": removed, "redis_namespace_empty": true, "nats_owned_stream_removed": streamRemoved, "nats_owned_stream_absent": true, "other_namespaces_untouched": true})
}
'''
PATTERN = re.compile(r'^test_mcmods_live_[a-f0-9]{20}$')
spec = importlib.util.spec_from_file_location('owned_services', BE / 'tools/testing/isolated_environment.py')
owned = importlib.util.module_from_spec(spec)
spec.loader.exec_module(owned)


def fingerprint(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def private_json(path, value):
    # Receipts authorize later cleanup. Readers must see the old complete value
    # or the new complete value, never a truncated/partially written document.
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode='w',prefix='.'+path.name+'.',dir=path.parent,delete=False) as output:
            temporary = Path(output.name)
            output.write(json.dumps(value, indent=2) + '\n')
            output.flush()
            os.fsync(output.fileno())
        temporary.chmod(0o600)
        os.replace(temporary,path)
        directory = os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def guarded_parent():
    record = json.loads((STATE / 'resources.json').read_text())
    environment = json.loads((STATE / 'environment.json').read_text())
    if not re.fullmatch('[a-f0-9]{32}', record.get('owner', '')) or not re.fullmatch('test_mcmods_audit_[a-f0-9]{16}', record.get('database', '')):
        raise RuntimeError('state is not an owned isolated_environment record')
    infos = {name: owned.verify_container(identifier, record['owner'])
             for name, identifier in record['containers'].items()}
    connection = urllib.parse.urlsplit(environment['DATABASE_URL'])
    binding = infos['postgres']['NetworkSettings']['Ports']['5432/tcp'][0]
    if (connection.scheme != 'postgres' or connection.hostname != '127.0.0.1'
            or connection.port != int(binding['HostPort']) or binding['HostIp'] != '127.0.0.1'
            or connection.username != 'mcmods' or connection.fragment
            or urllib.parse.parse_qsl(connection.query) != [('sslmode','disable')]
            or connection.path != '/' + record['database']
            or environment['DATABASE_URL'] != environment['MCMODS_TEST_DATABASE_URL']):
        raise RuntimeError('owned parent identity mismatch; refusing child operations')
    for name,port,key in [('redis','6379/tcp','REDIS_ADDR'),('nats','4222/tcp','NATS_URL')]:
        binding = infos[name]['NetworkSettings']['Ports'][port][0]
        wanted = '127.0.0.1:'+binding['HostPort']
        if name == 'nats':
            url = urllib.parse.urlsplit(environment[key])
            if url.scheme != 'nats' or url.username or url.password or url.path or url.query or url.fragment:
                raise RuntimeError('NATS target must be the plain owned loopback endpoint')
            actual = url.netloc
        else:
            actual = environment[key]
        if binding['HostIp'] != '127.0.0.1' or actual != wanted:
            raise RuntimeError('owned service endpoint identity mismatch')
    return record, environment


def sql(record, database, text):
    if database != record['database'] and not PATTERN.fullmatch(database):
        raise RuntimeError('refusing non-owned database identifier')
    command = ['docker', '--host=unix:///var/run/docker.sock', 'exec', '-i',
               record['containers']['postgres'], 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
               '-U', 'mcmods', '-d', database]
    clean_env = os.environ.copy()
    for key in ('DOCKER_HOST', 'DOCKER_CONTEXT', 'DOCKER_TLS', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH'):
        clean_env.pop(key, None)
    result = subprocess.run(command, input=text, text=True, capture_output=True,
                            env=clean_env, timeout=30)
    if result.returncode:
        # SQL contains only controlled identifiers; no DSN/key is rendered.
        raise RuntimeError('owned SQL failed: ' + result.stderr.strip()[:1000])
    return result.stdout.strip()


def source_files():
    return {str(path.relative_to(BE)): fingerprint(path) for path in BE.rglob('*.go')
            if '.git' not in path.parts and 'vendor' not in path.parts}


def frontend_sync():
    if not COPY.is_dir() or not (COPY / 'node_modules').is_dir():
        raise RuntimeError('root-prepared independent frontend copy is missing')
    if any(COPY.glob('.env*')):
        raise RuntimeError('refusing development copy containing dotenv files')
    for directory in ['app', 'lib', 'public']:
        target = COPY/directory
        if target.is_symlink() or any(p.is_symlink() for p in target.rglob('*')):
            raise RuntimeError('refusing symlinked frontend-copy source paths')
    names = ['next.config.ts', 'package.json', 'package-lock.json', 'tsconfig.json', 'postcss.config.mjs']
    paths = [p for directory in ['app', 'lib', 'public'] for p in (FE / directory).rglob('*') if p.is_file()]
    paths += [FE / name for name in names if (FE / name).is_file()]
    manifest = {}
    for source in paths:
        if source.is_symlink() or not source.resolve().is_relative_to(FE.resolve()):
            raise RuntimeError('refusing frontend symlink or external source')
        relative = source.relative_to(FE)
        target = COPY / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        if not target.exists() or fingerprint(target) != fingerprint(source):
            target.write_bytes(source.read_bytes())
        manifest[str(relative)] = fingerprint(source)
    for directory in ['app', 'lib', 'public']:
        if any(str(p.relative_to(COPY)) not in manifest for p in (COPY / directory).rglob('*') if p.is_file()):
            raise RuntimeError('unexpected stale source in independent copy; refusing cleanup')
    if any(fingerprint(COPY / name) != value for name, value in manifest.items()):
        raise RuntimeError('frontend source changed while copying')
    private_json(SCOPE / 'frontend-source.json', manifest)
    return manifest


def build_current():
    before = source_files()
    with (SCOPE / 'build.private.log').open('w') as output:
        result = subprocess.run(['go', 'build', '-p', '1', '-o', str(SCOPE / 'backend-current'), '.'],
                                cwd=BE, env=os.environ | {'GOMAXPROCS': '2'}, stdout=output,
                                stderr=subprocess.STDOUT, timeout=180)
    if result.returncode or before != source_files():
        raise RuntimeError('current Go build failed or source changed while compiling')
    private_json(SCOPE / 'backend-source.json', before)


def scope_record(parent):
    record = json.loads((SCOPE / 'scope.json').read_text())
    if (record['parent_owner'] != parent['owner'] or record['parent_container'] != parent['containers']['postgres']
            or not record['created'] or not PATTERN.fullmatch(record['database'])):
        raise RuntimeError('scope identity mismatch; refusing child operation')
    return record


def prepare():
    global SCOPE_CREATED_THIS_CALL
    parent, environment = guarded_parent()
    SCOPE.mkdir(mode=0o700)
    SCOPE_CREATED_THIS_CALL = True
    database = 'test_mcmods_live_' + secrets.token_hex(10)
    record = {'parent_owner': parent['owner'], 'parent_container': parent['containers']['postgres'],
              'database': database, 'created': False, 'runtime_started': False, 'nonce_namespace': secrets.token_hex(16)}
    private_json(SCOPE / 'scope.json', record)
    sql(parent, parent['database'], 'create database "' + database + '";')
    record['created'] = True
    private_json(SCOPE / 'scope.json', record)
    connection = urllib.parse.urlsplit(environment['DATABASE_URL'])
    child_url = urllib.parse.urlunsplit(connection._replace(path='/' + database))
    for key in ('MODRINTH_TOKEN','CURSEFORGE_API_KEY','GITHUB_TOKEN','SMTP_PASSWORD','SMTP_USERNAME',
                'SMTP_HOST','REDIS_USERNAME','REDIS_PASSWORD','NATS_TOKEN','NATS_USERNAME','NATS_PASSWORD',
                'TYPESENSE_API_KEY','YGGDRASIL_PRIVATE_KEY_BASE64','TURNSTILE_SECRET_KEY','TURNSTILE_SITE_KEY'):
        environment[key] = ''
    environment.update(DATABASE_URL=child_url, MCMODS_TEST_DATABASE_URL=child_url,
                       DB_NAME=database, DB_RESET_ON_START='false', DB_RESET_CONFIRM='', APP_ENV='test',
                       SEED_ADMIN_PASSWORD=secrets.token_urlsafe(32), JWT_SECRET=secrets.token_hex(32),
                       SETTINGS_ENCRYPTION_KEY=secrets.token_hex(32), ANTI_ABUSE_HMAC_SECRET=secrets.token_hex(32),
                       ANTI_ABUSE_IP_HASH_SECRET=secrets.token_hex(32), APP_REPLICA_COUNT='1',
                       REDIS_NAMESPACE='live_' + record['nonce_namespace'],
                       NATS_SUBJECT_PREFIX='live.' + record['nonce_namespace'],
                       NATS_JETSTREAM_STREAM='LIVE_' + record['nonce_namespace'].upper(),
                       SMTP_ENABLED='false', TYPESENSE_ENABLED='false', MCMODS_RUN_DB_INTEGRATION='0',
                       MCMODS_RUN_ACTIVITY_LOAD='0', MCMODS_SKIP_DOTENV='true')
    private_json(SCOPE / 'environment.json', environment)
    record['environment_sha256'] = fingerprint(SCOPE/'environment.json')
    private_json(SCOPE/'scope.json',record)
    build_current()
    init_env = os.environ | environment | {'GOMAXPROCS': '2', 'APP_ENV': 'development',
               'DB_RESET_ON_START': 'true', 'DB_RESET_CONFIRM': 'RESET ' + database}
    with (SCOPE / 'initialize.private.log').open('w') as output:
        result = subprocess.run(['go', 'run', '-p', '1', './cmd/db-reset'], cwd=BE, env=init_env,
                                stdout=output, stderr=subprocess.STDOUT, timeout=180)
    if result.returncode:
        raise RuntimeError('owned child initialization failed; see private log')
    identity = sql(parent, database, 'select current_database();')
    if identity != database:
        raise RuntimeError('child identity check failed')
    checks = sql(parent, database, "select (select count(*) from information_schema.tables where table_schema='public' and table_type='BASE TABLE'),"
       "(select count(*) from users where username='admin' and password_hash<>'password-login-disabled'),"
       "(select count(*) from seed_crawler_configs where enabled),"
       "(select count(*) from seed_crawler_runs),"
       "(select count(*) from ai_tasks),"
       "(select count(*) from system_settings where key in ('ai.config','oss.aliyun','smtp.config'));" )
    values = checks.split('|')
    if len(values) != 6 or int(values[0]) < 250 or values[1] != '1' or values[2:] != ['0','0','0','0']:
        raise RuntimeError('full schema/seed/no-provider safety check failed')
    index = sql(parent, database, "select indexname,indexdef from pg_indexes where schemaname='public' "
                "and tablename='catalog_tags' order by indexname;")
    frontend = frontend_sync()
    private_json(SCOPE / 'prepared.json', {'status':'PASS','tables':int(values[0]),'synthetic_admin_seeded':True,
        'crawler_enabled':False,'crawler_runs':0,'ai_tasks':0,'supplier_settings_absent':True,
        'catalog_tag_indexes':index.splitlines(),'backend_binary_sha256':fingerprint(SCOPE/'backend-current'),
        'backend_source_files':len(source_files()),'frontend_source_files':len(frontend),
        'boundary':'Fresh child full schema, current Go build and exact current source copy; no runtime/browser started.'})
    print('Prepared current code, full owned child schema and synthetic seed; runtime not started.', flush=True)


def available_port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1',0))
        return listener.getsockname()[1]


def wait_response(url, wanted, processes, seconds=90):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=2) as response:
                status = response.status
        except urllib.error.HTTPError as error:
            status = error.code
            error.close()
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            status = None
        if status == wanted:
            return
        if any(process.poll() is not None for process in processes):
            raise RuntimeError('owned runtime terminated before readiness')
        time.sleep(.2)
    raise RuntimeError('owned readiness exceeded startup bound')


def pidfd_has_exited(handle):
    """A pinned pidfd becomes readable on exit, including an unreaped zombie."""
    with selectors.DefaultSelector() as selector:
        selector.register(handle,selectors.EVENT_READ)
        return bool(selector.select(0))


def owned_group_handles(processes, owner):
    """Pin live members of our recorded sessions; never signal a reused PID."""
    groups = {process.pid for process in processes}
    handles = []
    try:
        for entry in Path('/proc').iterdir():
            if not entry.name.isdigit():
                continue
            handle = None
            try:
                fields = (entry/'stat').read_text().rsplit(')',1)[1].split()
                if int(fields[2]) not in groups or fields[2] != fields[3] or fields[0] == 'Z':
                    continue
                handle = os.pidfd_open(int(entry.name))
                environment = (entry/'environ').read_bytes().split(b'\0')
                current = (entry/'stat').read_text().rsplit(')',1)[1].split()
                if pidfd_has_exited(handle):
                    continue
                if current[19] != fields[19] or current[2:4] != fields[2:4]:
                    raise RuntimeError('process identity changed while pinning owned session')
                if ('MCMODS_LIVE_PROCESS_OWNER='+owner).encode() not in environment:
                    raise RuntimeError('session member lacks this live run owner; refusing signal')
                handles.append(handle)
                handle = None
            except (FileNotFoundError,ProcessLookupError):
                pass
            except PermissionError:
                # Linux can revoke /proc/environ access after our live stat
                # and pin, when that exact process exits. Never treat a live
                # unreadable process, or an unpinned identity, as stopped.
                if handle is None or not pidfd_has_exited(handle):
                    raise
            finally:
                if handle is not None:
                    os.close(handle)
        return handles
    except Exception:
        for handle in handles:
            os.close(handle)
        raise


def stop_owned_groups(processes, owner, grace_seconds=10, force_seconds=5):
    """The npm/docker leader may already be reaped while children remain."""
    started = time.monotonic()
    while True:
        handles = owned_group_handles(processes,owner)
        if not handles:
            for process in processes:
                process.wait(timeout=1)
            return True
        elapsed = time.monotonic()-started
        try:
            if elapsed > grace_seconds+force_seconds:
                raise RuntimeError('owned process sessions did not stop within the cleanup bound')
            signum = signal.SIGTERM if elapsed < grace_seconds else signal.SIGKILL
            for handle in handles:
                try:
                    signal.pidfd_send_signal(handle,signum)
                except ProcessLookupError:
                    pass
        finally:
            for handle in handles:
                os.close(handle)
        time.sleep(.05)


def guard_scope_environment(parent_environment, record, environment):
    """Revalidate persisted input before build/start; prepare is not authority."""
    nonce = record.get('nonce_namespace','')
    if not re.fullmatch('[a-f0-9]{32}',nonce):
        raise RuntimeError('run scope nonce is invalid')
    parent_url = urllib.parse.urlsplit(parent_environment['DATABASE_URL'])
    expected_url = urllib.parse.urlunsplit(parent_url._replace(path='/'+record['database']))
    expected = {'DATABASE_URL':expected_url,'MCMODS_TEST_DATABASE_URL':expected_url,
        'DB_NAME':record['database'],'DB_RESET_ON_START':'false','DB_RESET_CONFIRM':'',
        'APP_ENV':'test','MCMODS_SKIP_DOTENV':'true','SMTP_ENABLED':'false','TYPESENSE_ENABLED':'false',
        'REDIS_ADDR':parent_environment['REDIS_ADDR'],'REDIS_NAMESPACE':'live_'+nonce,
        'REDIS_ENABLED':'true','NATS_ENABLED':'true','NATS_JETSTREAM_ENABLED':'true',
        'NATS_URL':parent_environment['NATS_URL'],'NATS_SUBJECT_PREFIX':'live.'+nonce,
        'NATS_JETSTREAM_STREAM':'LIVE_'+nonce.upper()}
    for key,value in expected.items():
        if environment.get(key) != value:
            raise RuntimeError('run environment differs from exact child/namespace identity')
    for key in ('MODRINTH_TOKEN','CURSEFORGE_API_KEY','GITHUB_TOKEN','SMTP_PASSWORD','SMTP_USERNAME',
                'SMTP_HOST','REDIS_USERNAME','REDIS_PASSWORD','NATS_TOKEN','NATS_USERNAME','NATS_PASSWORD',
                'TYPESENSE_API_KEY','YGGDRASIL_PRIVATE_KEY_BASE64','TURNSTILE_SECRET_KEY','TURNSTILE_SITE_KEY'):
        if environment.get(key) != '':
            raise RuntimeError('run environment unexpectedly contains external credentials')
    if environment.get('REDIS_PREFIX','mcmods') != 'mcmods' or environment.get('REDIS_DB','0') != '0':
        raise RuntimeError('run cache layout differs from exact namespace cleanup')
    if fingerprint(SCOPE/'environment.json') != record.get('environment_sha256'):
        raise RuntimeError('private environment changed after this exact scope was prepared')


def run():
    parent, parent_environment = guarded_parent()
    record = scope_record(parent)
    environment = json.loads((SCOPE/'environment.json').read_text())
    guard_scope_environment(parent_environment,record,environment)
    result_path = BASE/'backend-a-live-current-result.json'
    previous = json.loads(result_path.read_text()) if result_path.exists() else {}
    if record.get('runtime_started') and (not previous.get('own_process_groups_stopped')
            or not record.get('runtime_run_id') or previous.get('run_id') != record['runtime_run_id']):
        raise RuntimeError('previous runtime is not confirmed stopped; refusing another run')
    if json.loads((SCOPE/'backend-source.json').read_text()) != source_files():
        build_current()
    frontend = frontend_sync()
    backend_port, frontend_port = available_port(), available_port()
    api, origin = f'http://127.0.0.1:{backend_port}', f'http://127.0.0.1:{frontend_port}'
    environment.update(APP_ADDR=f'127.0.0.1:{backend_port}', FRONTEND_ORIGIN=origin,
        NEXT_PUBLIC_API_BASE_URL=api, NEXT_PUBLIC_SITE_URL=origin,
        NEXT_PUBLIC_YGGDRASIL_API_ROOT=api+'/api/yggdrasil/',
        MCMODS_LIVE_FRONTEND_ORIGIN=origin,MCMODS_LIVE_API_ORIGIN=api,
        MCMODS_LIVE_ACCOUNT='admin',MCMODS_LIVE_PASSWORD=environment['SEED_ADMIN_PASSWORD'],
        NODE_ENV='development')
    # Runtime begins with a minimal ambient environment; no inherited paid,
    # proxy/provider credentials, instrumentation or SSR fetch fixture hooks.
    allowed = ['PATH','HOME','LANG','TZ','GOROOT','GOCACHE','GOMODCACHE','GOPATH',
               'PLAYWRIGHT_BROWSERS_PATH','SSL_CERT_FILE','SSL_CERT_DIR','NODE_EXTRA_CA_CERTS']
    runtime_env = {key:os.environ[key] for key in allowed if key in os.environ} | environment
    process_owner = record['nonce_namespace']
    runtime_env['MCMODS_LIVE_PROCESS_OWNER'] = process_owner
    processes, outputs = [], []
    run_id = secrets.token_hex(16)
    outcome = {'run_id':run_id,'boundary':'Current newly compiled Go + exact current Next dev copy; all own browser/API requests real; owned PostgreSQL child; external browser media blocked only; no paid AI/SMTP/provider key.',
               'frontend_source_files':len(frontend),'backend_binary_sha256':fingerprint(SCOPE/'backend-current'),
               'backend_source_snapshot_sha256':fingerprint(SCOPE/'backend-source.json'),
               'frontend_source_snapshot_sha256':fingerprint(SCOPE/'frontend-source.json'),
               'external_scheduler_prevented':'Dedicated child advisory lease held before application startup; normal production scheduler skips competing lease.'}
    # Invalidate a previous successful receipt before starting any process.
    # SIGKILL/power loss can prevent finally; cleanup must then fail closed.
    private_json(result_path,{'run_id':run_id,'status':'RUNNING','own_process_groups_stopped':False})
    record['runtime_started'] = True
    record['runtime_run_id'] = run_id
    private_json(SCOPE/'scope.json',record)
    try:
        source = (BE/'internal/httpapi/minecraft_loader_version_sources.go').read_text()
        key_match = re.search(r'minecraftVersionSyncAdvisoryLockKey\s+int64\s*=\s*(0x[0-9a-fA-F]+)',source)
        if not key_match:
            raise RuntimeError('cannot derive current startup scheduler lock key')
        lease_log = (SCOPE/'scheduler-lease.private.log').open('w');outputs.append(lease_log)
        lease = subprocess.Popen(['docker','--host=unix:///var/run/docker.sock','exec','-i',parent['containers']['postgres'],
             'psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','mcmods','-d',record['database']],
             stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=lease_log,text=True,start_new_session=True,env=runtime_env)
        processes.append(lease)
        lease.stdin.write('select pg_advisory_lock('+str(int(key_match.group(1),16))+');\nselect \'owned-scheduler-lease-held\';\n');lease.stdin.flush()
        # Do not let a stalled psql/stdout block startup indefinitely.
        expected = b"\nowned-scheduler-lease-held\n"
        data = b""
        deadline = time.monotonic()+15
        with selectors.DefaultSelector() as selector:
            selector.register(lease.stdout,selectors.EVENT_READ)
            while expected not in data:
                remaining = deadline-time.monotonic()
                if remaining <= 0 or not selector.select(remaining):
                    raise RuntimeError('startup advisory lease exceeded its acquisition bound')
                chunk = os.read(lease.stdout.fileno(),1024)
                if not chunk or len(data)+len(chunk)>1024:
                    raise RuntimeError('startup advisory lease response is invalid')
                data += chunk
        for command,cwd,name in [([str(SCOPE/'backend-current')],BE,'backend'),
             (['npm','run','dev','--','--hostname','127.0.0.1','--port',str(frontend_port)],COPY,'frontend')]:
            output=(SCOPE/(name+'.private.log')).open('w');outputs.append(output)
            process=subprocess.Popen(command,cwd=cwd,env=runtime_env,stdout=output,stderr=subprocess.STDOUT,start_new_session=True)
            processes.append(process)
        wait_response(api+'/api/v1/auth/me',401,processes)
        wait_response(origin+'/login',200,processes)
        with (BASE/'backend-a-live-current.log').open('w') as output:
            browser_test=subprocess.Popen(['npm','run','test:live'],cwd=FE,env=runtime_env,
                stdout=output,stderr=subprocess.STDOUT,start_new_session=True)
            processes.append(browser_test)
            outcome['exit_code']=browser_test.wait(timeout=120)
        if outcome['exit_code'] == 0:
            values = sql(parent,record['database'],
                "select (select count(*) from auth_sessions where revoked_at is null),"
                "(select count(*) from favorite_collections where name like 'audit-live-%'),"
                "(select count(*) from ai_tasks),"
                "(select count(*) from ai_task_logs where event='provider_request_usage'),"
                "(select count(*) from seed_crawler_runs);").split('|')
            outcome['post_journey_zero_counts'] = dict(zip(
                ['active_sessions','journey_collections','ai_tasks','provider_request_usage','crawler_runs'],
                map(int,values),strict=True))
            if any(value != 0 for value in outcome['post_journey_zero_counts'].values()):
                raise RuntimeError('real journey persistence/session/no-provider postconditions failed')
        backend_log=(SCOPE/'backend.private.log').read_text()
        outcome['startup_scheduler_skipped']= 'skip Minecraft version synchronization: Minecraft version synchronization is already in progress' in backend_log
        if not outcome['startup_scheduler_skipped']:
            raise RuntimeError('startup scheduler skip evidence missing')
        original = json.loads((SCOPE/'backend-source.json').read_text())
        current = source_files()
        outcome['backend_source_drift'] = sorted(set(original) ^ set(current)) + [
            name for name in original.keys() & current.keys() if original[name] != current[name]]
        current_frontend_paths = [p for directory in ['app','lib','public']
            for p in (FE/directory).rglob('*') if p.is_file()]
        current_frontend = {str(p.relative_to(FE)):fingerprint(p) for p in current_frontend_paths}
        current_frontend.update({name:fingerprint(FE/name) for name in ['next.config.ts','package.json','package-lock.json','tsconfig.json','postcss.config.mjs'] if (FE/name).is_file()})
        outcome['frontend_copy_drift'] = sorted(set(frontend)^set(current_frontend))+[
            name for name in frontend.keys()&current_frontend.keys() if frontend[name]!=current_frontend[name]]
        if outcome['backend_source_drift'] or outcome['frontend_copy_drift']:
            outcome['exit_code'] = 1
            outcome['failure_summary'] = 'Source changed during acceptance; rebuild and repeat this bounded journey.'
    except Exception as error:
        outcome['exit_code']=1
        outcome['failure_type']=type(error).__name__
        # Do not render Popen command environments or secret-bearing URLs.
        outcome['failure_summary']='Owned live run failed; inspect private runtime/initialization/browser logs.'
    finally:
        try:
            outcome['own_process_groups_stopped'] = stop_owned_groups(processes,process_owner)
        except Exception as error:
            outcome['own_process_groups_stopped'] = False
            outcome['exit_code'] = 1
            outcome['cleanup_failure_type'] = type(error).__name__
            outcome['failure_summary'] = 'Owned session cleanup failed; private scope resources retained.'
        for output in outputs:output.close()
        private_json(result_path,outcome)
    print(json.dumps(outcome),flush=True)
    return outcome['exit_code']


def cleanup():
    parent,parent_environment=guarded_parent()
    record=scope_record(parent)
    result_path=BASE/'backend-a-live-current-result.json'
    result=json.loads(result_path.read_text()) if result_path.exists() else {}
    if record.get('runtime_started') and (not result.get('own_process_groups_stopped')
            or not record.get('runtime_run_id') or result.get('run_id') != record['runtime_run_id']):
        raise RuntimeError('own runtime is not confirmed stopped; refusing resource cleanup')
    environment=json.loads((SCOPE/'environment.json').read_text())
    guard_scope_environment(parent_environment,record,environment)
    nonce=record['nonce_namespace']
    if not re.fullmatch('[a-f0-9]{32}',nonce):
        raise RuntimeError('scope namespace mismatch')
    if (environment['REDIS_NAMESPACE'] != 'live_'+nonce
            or environment['NATS_SUBJECT_PREFIX'] != 'live.'+nonce
            or environment['NATS_JETSTREAM_STREAM'] != 'LIVE_'+nonce.upper()):
        raise RuntimeError('refusing unowned namespace cleanup')
    for name,port,key in [('redis','6379/tcp','REDIS_ADDR'),('nats','4222/tcp','NATS_URL')]:
        info=owned.verify_container(parent['containers'][name],parent['owner'])
        binding=info['NetworkSettings']['Ports'][port][0]
        wanted='127.0.0.1:'+binding['HostPort']
        actual=environment[key] if name=='redis' else urllib.parse.urlsplit(environment[key]).netloc
        if binding['HostIp']!='127.0.0.1' or actual!=wanted:
            raise RuntimeError('service endpoint identity mismatch; refusing cleanup')
    # Public-client-only imports permit a temp Go file outside this repository.
    # It can never create a package visible to go test ./... or staged output.
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode='w',suffix='.go',prefix='live_cleanup_',dir=SCOPE,delete=False) as source:
            source.write(CLEANUP_GO_SOURCE)
            temporary = Path(source.name)
        temporary.chmod(0o600)
        with (SCOPE/'namespace-cleanup.private.log').open('w') as output:
            completed=subprocess.run(['go','run','-p','1',str(temporary),str(SCOPE/'environment.json')],
                cwd=BE,env=os.environ|{'GOMAXPROCS':'2'},stdout=output,stderr=subprocess.STDOUT,timeout=60)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
    if completed.returncode:
        raise RuntimeError('owned namespace cleanup failed; no database drop attempted')
    namespaces=json.loads((SCOPE/'namespace-cleanup.private.log').read_text())
    sql(parent,parent['database'],'drop database "'+record['database']+'" with (force);')
    remaining=sql(parent,parent['database'],"select count(*) from pg_database where datname='"+record['database']+"';")
    if remaining!='0':
        raise RuntimeError('owned child removal not confirmed')
    record['created']=False
    private_json(SCOPE/'scope.json',record)
    private_json(BASE/'backend-a-live-cleanup.json',{'status':'PASS','own_process_groups_stopped':True,
        'exact_child_dropped_and_absent':True,'namespace_cleanup':namespaces,
        'parent_containers_owner_verified':True,'parent_services_untouched':True})
    print('Removed only the recorded nonce child and exact Redis/NATS namespaces; parent services untouched.',flush=True)


def copy_guard(parent):
    if any(COPY == repo or COPY.is_relative_to(repo) or repo.is_relative_to(COPY) for repo in (BE,FE)):
        raise RuntimeError('frontend-copy must be an independent disposable directory')
    if not COPY.is_dir() or not (COPY/'node_modules').is_dir():
        raise RuntimeError('prepare a dedicated copy with matching installed dependencies first')
    if fingerprint(COPY/'package-lock.json') != fingerprint(FE/'package-lock.json'):
        raise RuntimeError('dedicated copy has a different dependency lockfile')
    if any(COPY.glob('.env*')):
        raise RuntimeError('refusing any dotenv file in the independent copy')
    marker=COPY/'.mcmods-live-copy-owner.json'
    if marker.is_symlink() or (COPY/'.mcmods-live-run.lock').is_symlink():
        raise RuntimeError('refusing symlinked frontend ownership/lock record')
    expected={'parent_owner':parent['owner'],'frontend_source':str(FE.resolve())}
    if marker.exists():
        if json.loads(marker.read_text()) != expected:
            raise RuntimeError('independent frontend copy has another owner/source')
    elif ADOPT_PREPARED_COPY:
        # Explicit opt-in for an operator-created disposable copy. Never delete
        # stale/unrecognized files or operate on the source repository itself.
        private_json(marker,expected)
    else:
        raise RuntimeError('new prepared copy requires explicit --adopt-prepared-copy')


def main():
    global FE,STATE,SCOPE,COPY,BASE,ADOPT_PREPARED_COPY
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--state',required=True,type=Path,help='state created by isolated_environment.py')
    parser.add_argument('--frontend',required=True,type=Path,help='actual frontend repository')
    parser.add_argument('--scope',required=True,type=Path,help='new private directory per acceptance run')
    parser.add_argument('--frontend-copy',required=True,type=Path,help='dedicated dependency-installed disposable Next copy')
    parser.add_argument('--adopt-prepared-copy',action='store_true',help='explicitly confirm that frontend-copy is disposable')
    parser.add_argument('action',choices=['prepare','run','cleanup','all'],nargs='?',default='all')
    args=parser.parse_args()
    os.umask(0o077)
    if not hasattr(os,'pidfd_open') or not hasattr(signal,'pidfd_send_signal'):
        raise RuntimeError('live acceptance requires Linux pidfd process identity support')
    FE,STATE,SCOPE,COPY=[p.resolve() for p in [args.frontend,args.state,args.scope,args.frontend_copy]]
    BASE=SCOPE
    ADOPT_PREPARED_COPY=args.adopt_prepared_copy
    if args.action in ('prepare','all') and SCOPE.exists():
        raise RuntimeError('new acceptance needs a new private scope; existing scopes are never adopted')
    if SCOPE==STATE or SCOPE.is_relative_to(STATE) or STATE.is_relative_to(SCOPE):
        raise RuntimeError('run scope must be separate from the parent environment state')
    if SCOPE.is_relative_to(BE) or SCOPE.is_relative_to(FE) or BE.is_relative_to(SCOPE) or FE.is_relative_to(SCOPE):
        raise RuntimeError('private run scope must be outside both repositories')
    parent,_=guarded_parent()
    copy_guard(parent)
    # Serialize acceptance against this exact dedicated Next copy, not against
    # other workers/services/isolated copies in the task.
    with (COPY/'.mcmods-live-run.lock').open('a') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
        try:
            if args.action=='prepare':
                prepare();return 0
            if args.action=='run':return run()
            if args.action=='cleanup':cleanup();return 0
            try:
                prepare()
                return run()
            finally:
                record_path=SCOPE/'scope.json'
                if SCOPE_CREATED_THIS_CALL and record_path.exists() and json.loads(record_path.read_text()).get('created'):
                    cleanup()
        finally:
            fcntl.flock(lock,fcntl.LOCK_UN)


if __name__=='__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        # Never render arbitrary subprocess environments, credential-bearing
        # URLs or private filenames on stdout/stderr. Raw evidence stays private.
        print('Live acceptance failed ('+type(error).__name__+'); inspect the private scope evidence.',flush=True)
        raise SystemExit(1)
