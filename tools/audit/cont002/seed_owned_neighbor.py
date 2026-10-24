#!/usr/bin/env python3
"""Add only synthetic CONT-002 manual neighbors to a sealed, live task fixture.

Refuses arbitrary databases, unsealed/expired fixtures and owner mismatch. This
is not manual authoring/review acceptance and never certifies browser rendering.
"""
import argparse
import datetime
import hashlib
import json
import pathlib
import re
import subprocess
import urllib.parse

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--runtime', required=True, type=pathlib.Path)
p.add_argument('--namespace', required=True)
p.add_argument('--mixed-type', default='minecraft:crafting')
p.add_argument('--container', default='mcmods-audit-20261024-postgres18')
p.add_argument('--apply', action='store_true', help='Without this, only writes validated SQL; no mutation.')
a = p.parse_args()
if not re.fullmatch(r'auditbrowser[a-z0-9_]{2,30}', a.namespace):
    p.error('namespace must be a unique synthetic auditbrowser namespace')
if not re.fullmatch(r'[a-z0-9_.-]+:[a-z0-9_./-]+', a.mixed_type):
    p.error('invalid canonical mixed recipe type')
runtime_path = a.runtime.resolve()
root = runtime_path.parent
runtime = json.loads(runtime_path.read_text())
if runtime.get('state') in {'EXPIRED', 'STOPPED', 'FAILED'}:
    raise SystemExit('fixture is no longer live; start a fresh fixture')
if not (root / 'GAME_PHASE_SEALED').is_file():
    raise SystemExit('seal and independently review original game phase before adding neighbor rows')
pid = runtime['pid']
if not isinstance(pid, int) or pid <= 1:
    raise SystemExit('invalid owned process')
if (root / 'OWNER').read_text().strip() != f'mcmods-live-journey pid={pid}':
    raise SystemExit('fixture OWNER does not match runtime')
cmdline = pathlib.Path(f'/proc/{pid}/cmdline').read_bytes().replace(b'\0', b' ')
if b'TestAuditLiveGameJourneyFixture' not in cmdline:
    raise SystemExit('process is not the expected live fixture')
api = urllib.parse.urlsplit(runtime['apiOrigin'])
if api.scheme != 'http' or api.hostname != '127.0.0.1':
    raise SystemExit('only owned loopback fixture API is supported')
sample = pathlib.Path(runtime['samplePath']).read_bytes()
if hashlib.sha256(sample).hexdigest() != runtime['sampleSha256']:
    raise SystemExit('original game sample hash changed')
terminal = json.loads((root / 'database-terminal.json').read_text())
schema = terminal['ownedSchema']
if not re.fullmatch(r'mcmods_audit_[0-9a-f]{16}', schema):
    raise SystemExit('not an owned isolated test schema')
label = subprocess.check_output(['docker', 'inspect', '--format', '{{ index .Config.Labels "mcmods.audit.task" }}', a.container], text=True).strip()
if label != '20261024':
    raise SystemExit('task container ownership label mismatch')

def sql(query):
    return subprocess.check_output(['docker', 'exec', '-i', a.container, 'psql', '-U', 'mcmods_audit', '-d', 'mcmods_audit_test', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1'], input=query, text=True).strip()

owner, marker = sql(f"select pg_get_userbyid(nspowner),obj_description(oid,'pg_namespace') from pg_namespace where nspname='{schema}'").split('|', 1)
if owner != 'mcmods_audit' or not re.fullmatch('mcmods-owned-test:' + schema.removeprefix('mcmods_audit_') + '[0-9a-f]{16}', marker):
    raise SystemExit('schema owner/comment mismatch')
out = root / ('cont002-browser-neighbor-' + a.namespace)
out.mkdir(mode=0o700, exist_ok=False)
preflight = dict(recordedAt=datetime.datetime.now(datetime.timezone.utc).isoformat(), schema=schema, marker=marker, process=pid, namespace=a.namespace, sampleSha256=runtime['sampleSha256'], source=runtime['repoRevisions'], scope='Additional synthetic authority seeds only; original game phase stays sealed. Manual authoring/review workflow is not tested.')
(out / 'ownership-preflight.json').write_text(json.dumps(preflight, indent=2) + '\n')

def identity(kind, key, prefix):
    return prefix + '_' + hashlib.sha256(('mcmods-catalog/v1\0'+kind+'\0'+key.lower().strip()).encode()).hexdigest()[:32]

seed = (pathlib.Path(__file__).parent / 'manual-neighbor.sql.in').read_text()
for k, v in {'__SCHEMA__': schema, '__MARKER__': marker, '__NAMESPACE__': a.namespace, '__MIXED_TYPE__': a.mixed_type,
             '__RESOURCE_IDENTITY__': identity('resource', 'minecraft.item\0'+a.namespace+':manual_result', 'res'),
             '__TYPE_IDENTITY__': identity('recipe_type', a.namespace+':manual_only', 'rct')}.items():
    seed = seed.replace(k, v)
(out / 'seed.sql').write_text(seed)
tables = ['catalog_entities','recipe_types','game_resources','recipes','recipe_layout_templates','recipe_template_slots','recipe_definitions','recipe_bindings','recipe_binding_candidates','recipe_import_snapshots','recipe_version_bindings','public_routes']

def snapshot():
    parts = [f"'{t}',(select coalesce(jsonb_agg(to_jsonb(r) order by to_jsonb(r)::text),'[]'::jsonb) from {schema}.{t} r)" for t in tables]
    return json.loads(sql('select jsonb_build_object(' + ','.join(parts) + ')'))

before = snapshot()
(out / 'catalog-before.json').write_text(json.dumps(before, indent=2) + '\n')
if not a.apply:
    print(json.dumps(dict(result='DRY_RUN_SQL_WRITTEN', directory=str(out), productionExecuted=False)))
    raise SystemExit(0)
# SQL repeats the exact owner marker inside its transaction. Namespace collisions
# fail unique constraints and roll back rather than overwrite existing content.
result = sql(seed)
(out / 'seed-result.json').write_text(result + '\n')
after = snapshot()
(out / 'catalog-after.json').write_text(json.dumps(after, indent=2) + '\n')
checks = {}
for table in tables:
    old = {json.dumps(row, sort_keys=True) for row in before[table]}
    new = {json.dumps(row, sort_keys=True) for row in after[table]}
    checks[table] = dict(before=len(old), after=len(new), allOriginalRowsUnchanged=old <= new)
if not all(c['allOriginalRowsUnchanged'] for c in checks.values()):
    raise SystemExit('original rows changed; preserve both snapshots and inspect')
(out / 'acceptance.json').write_text(json.dumps(dict(scope=preflight['scope'], originalRows=checks, browserStatus='NOT_RUN', productionExecuted=False), indent=2) + '\n')
print(json.dumps(dict(result='SYNTHETIC_NEIGHBORS_CREATED_BROWSER_NOT_RUN', directory=str(out))))
