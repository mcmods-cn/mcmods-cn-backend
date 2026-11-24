#!/usr/bin/env python3
"""Read-only final059b native/PG/JetStream/asset seal; never writes STOP.

Requires the exact live owned TestAuditFinalGameJourneyFixture. Compiling it or
running this observer without its native browser report is not product PASS.
"""
import argparse
import copy
import datetime
import hashlib
import io
import json
import os
import pathlib
import re
import socket
import ssl
import stat
import sys
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import zipfile

from PIL import Image

EXPECTED_SHA = '059b1c56921a3f70b934bd577fd940486982da03b24d2f6dca7c0efcfb5af2eb'
EXPECTED_PRODUCER = '806214802c1e3447a9895fa5311e73f3be6edfcc'
CORE_SETTLE_SECONDS = 120
RAW_OBSERVATION_CEILING_SECONDS = 3660
RAW_NORMAL_POLL_SECONDS = 15
RAW_POLL_MARGIN_SECONDS = 30


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def load(path):
    return json.loads(path.read_text())


def instant(value):
    parsed = datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
    assert parsed.tzinfo is not None
    return parsed


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        return None


def origin(raw, scheme):
    parsed = urllib.parse.urlsplit(raw)
    assert parsed.scheme == scheme and parsed.hostname == '127.0.0.1' and parsed.port
    assert not parsed.username and not parsed.password and parsed.path in ('', '/') and not parsed.query and not parsed.fragment
    return parsed.scheme + '://' + parsed.netloc


def main():
    observer_started = time.monotonic()
    parser = argparse.ArgumentParser()
    parser.add_argument('runtime', type=pathlib.Path)
    parser.add_argument('native_report', type=pathlib.Path)
    parser.add_argument('output', type=pathlib.Path)
    opt = parser.parse_args()
    runtime_path = opt.runtime.resolve()
    directory = runtime_path.parent
    output = opt.output.resolve()
    assert output.parent == directory and not output.exists(), 'Use a fresh child evidence directory'
    output.mkdir(mode=0o700)
    report = {'status': 'IN_PROGRESS', 'recordedAt': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'readOnly': True, 'observerSHA256': sha(pathlib.Path(__file__).read_bytes()), 'checks': []}

    def check(name, condition):
        report['checks'].append({'name': name, 'result': 'PASS' if condition else 'FAIL'})
        assert condition, name

    try:
        runtime = load(runtime_path)
        assert runtime['owner'] == 'mcmods-final-game-journey' and runtime['state'] == 'READY'
        check('Explicit owned new fixture, source namespace and default sample', runtime['sampleSha256'] == EXPECTED_SHA and runtime['sampleBytes'] == 18290 and runtime['sampleProducerCommit'] == EXPECTED_PRODUCER and runtime['namespace'] == 'auditfixture' and runtime['minecraftVersion'] == '1.20.1' and runtime['loader'] == 'fabric' and runtime['modVersion'] == '1.0.0')
        process = pathlib.Path('/proc') / str(runtime['pid'])
        assert process.stat().st_uid == os.getuid()
        assert b'TestAuditFinalGameJourneyFixture' in (process / 'cmdline').read_bytes()
        assert (process / 'cwd').resolve() == pathlib.Path(runtime['testPackageWorkingDirectory']).resolve()
        owner_file = directory / 'OWNER'
        assert owner_file.stat().st_uid == os.getuid() and owner_file.read_text().strip() == f"mcmods-final-game-journey pid={runtime['pid']}"
        auth_path = pathlib.Path(runtime['authPath']).resolve()
        assert auth_path.parent == directory / '.private' and stat.S_IMODE(auth_path.stat().st_mode) == 0o600 and auth_path.stat().st_uid == os.getuid()
        api = origin(runtime['apiOrigin'], 'http')
        gateway = origin(runtime['frontendOrigin'], 'https')
        oss = origin(runtime['ossOrigin'], 'https')
        assert gateway == oss
        tls = runtime['tls']
        assert tls['verified'] is True and tls['insecureSkipVerify'] is False
        bundle = pathlib.Path(tls['trustedCABundlePath'])
        assert bundle.is_absolute() and sha(bundle.read_bytes()) == tls['trustedCABundleSHA256']
        tls_context = ssl.create_default_context(cafile=str(bundle))
        assert tls_context.check_hostname and tls_context.verify_mode == ssl.CERT_REQUIRED
        with socket.create_connection(('127.0.0.1', urllib.parse.urlsplit(gateway).port), timeout=5) as raw_tls_socket:
            with tls_context.wrap_socket(raw_tls_socket, server_hostname='127.0.0.1') as verified_socket:
                check('Independent verified HTTPS chain and exact owned leaf certificate', sha(verified_socket.getpeercert(binary_form=True)) == tls['leafCertificateDERSHA256'])
        build = runtime['frontendProductionBuild']
        manifest_path = pathlib.Path(build['manifestPath'])
        assert manifest_path.is_absolute() and sha(manifest_path.read_bytes()) == build['manifestSHA256']
        manifest = load(manifest_path)
        assert manifest['sourceCommit'] == build['sourceCommit'] == runtime['repoRevisions']['mcmods-cn-frontend']['commit']
        assert manifest['buildId'] == build['buildId'] and build['buildId'].strip()
        assert manifest['embeddedSiteOrigin'] == build['embeddedSiteOrigin'] == gateway
        gateway_api = build['embeddedAPIOrigin']
        assert manifest['embeddedApiOrigin'] == gateway_api and gateway_api in (gateway, gateway + '/service')
        deploy = pathlib.Path(build['deploymentPath'])
        check('Actual pinned standalone production artifact, no runtime origin override', deploy.is_absolute() and (deploy / '.next/BUILD_ID').read_text().strip() == build['buildId'] and sha((deploy / 'server.js').read_bytes()) == build['serverSHA256'])
        before_seal = load(directory / 'pre-business-source-executable-seal.json')
        assert before_seal['tls'] == tls and before_seal['frontendProductionBuild'] == build
        check('Pre-business source/executable/sample seal retained', before_seal['repoRevisions'] == runtime['repoRevisions'] and before_seal['compiledExecutable'] == runtime['compiledExecutable'] and before_seal['sampleSha256'] == EXPECTED_SHA and before_seal['sampleBytes'] == 18290 and before_seal['settingsSHA256'] == runtime['initialSettingsSHA256'])
        actual_executable = hashlib.sha256()
        actual_size = 0
        with (process / 'exe').open('rb') as stream:
            for chunk in iter(lambda: stream.read(1 << 20), b''):
                actual_executable.update(chunk)
                actual_size += len(chunk)
        check('Actual live executable matches pre-business hash', actual_executable.hexdigest() == runtime['compiledExecutable']['sha256'] and actual_size == runtime['compiledExecutable']['bytes'])
        for name, pin in runtime['repoRevisions'].items():
            path = pathlib.Path(pin['checkoutPath'])
            actual = subprocess.check_output(['git', '-C', str(path), 'rev-parse', 'HEAD'], text=True).strip()
            dirty = subprocess.check_output(['git', '-C', str(path), 'status', '--porcelain'])
            check('Same clean startup/seal source ' + name, actual == pin['commit'] and not dirty.strip() and pin['dirty'] is False)
        source = pathlib.Path(runtime['samplePath']).read_bytes()
        check('Raw originally game-produced059b bytes unchanged', len(source) == 18290 and sha(source) == EXPECTED_SHA)
        archive = zipfile.ZipFile(io.BytesIO(source))
        assert archive.testzip() is None
        for name in archive.namelist():
            assert not name.startswith('/') and '..' not in pathlib.PurePosixPath(name).parts
        source_images = {name: archive.read(name) for name in archive.namelist() if name.endswith('.png')}
        archive.close()
        native_path = opt.native_report.resolve()
        assert native_path.parent == directory and native_path.name.startswith('browser-journey-')
        native = load(native_path)
        check('Actual native chain and exact source triple', native['status'] == 'BROWSER_CHAIN_PASS_PENDING_DATABASE_QUEUE_POSTCONDITION_REVIEW' and native['apiInterception'] is False and not native['reactErrors'] and native['runtimeRevisions'] == runtime['repoRevisions'] and native['frontendCommit'] == runtime['repoRevisions']['mcmods-cn-frontend']['commit'] and native['sampleSha256'] == EXPECTED_SHA and native['sampleBytes'] == 18290)
        check('Ordinary creator and distinct independent reviewer', native['creatorId'] == runtime['creatorPublicId'] and native['reviewerId'] == runtime['reviewerPublicId'] and native['creatorId'] != native['reviewerId'])
        check('Native stages include actual upload, worker, review and anonymous pixels', {row['stage'] for row in native['milestones']} == {'browser_upload_created_actual_job', 'actual_worker_ready', 'independent_browser_review_committed', 'anonymous_frontend_recipe_and_source_pixels_verified'} and native['imageEvidence'] and all(row['decodedRGBAEqual'] for row in native['imageEvidence']))
        baseline = load(directory / 'database-before-game.json')
        check('No earlier import/reward facts inherited', all(baseline[key] == 0 for key in ['jobs', 'revisions', 'recipes', 'activeTasks', 'taskProgress', 'experienceRows', 'experienceTransactions', 'currencyTransactions']))

        schema = runtime['ownedSchema']
        assert re.fullmatch(r'mcmods_audit_[0-9a-f]{16}', schema)
        assert re.fullmatch(r'[a-z0-9]{9}', runtime['projectPublicId']) and re.fullmatch(r'[a-z0-9]{9}', runtime['creatorPublicId'])
        for key in ('jobId', 'revisionId'):
            assert str(uuid.UUID(native[key])) == native[key]
        environment = dict(os.environ)
        for key in ['DOCKER_HOST', 'DOCKER_CONTEXT', 'DOCKER_TLS', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH']:
            environment.pop(key, None)
        docker = ['docker', '--host=unix:///var/run/docker.sock']
        sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / 'testing'))
        import isolated_environment
        lab = pathlib.Path(os.environ['MCMODS_AUDIT_OWNED_ENV_DIR'])
        assert lab.is_absolute()
        record = isolated_environment.read_resource_record(lab)
        container = record['containers']['postgres']
        description = isolated_environment.verify_container(container, record['owner'])
        binding = runtime['databaseBinding']
        ports = description['NetworkSettings']['Ports']['5432/tcp']
        assert ports == [{'HostIp': '127.0.0.1', 'HostPort': str(binding['port'])}] and binding['host'] == '127.0.0.1'
        env_cfg = load(lab / 'environment.json')
        control = urllib.parse.urlsplit(env_cfg['DATABASE_URL'])
        assert control.scheme in ('postgres', 'postgresql') and control.hostname == binding['host'] and control.port == binding['port']
        assert control.username == binding['role'] == binding['owner'] and control.path == '/' + record['database']
        database = binding['name']
        assert database == record['database'] or re.fullmatch(r'test_mcmods_journey_[0-9a-f]{16}', database)
        def sql(statement):
            run = subprocess.run(docker + ['exec', '-i', container, 'psql', '-U', binding['role'], '-d', database, '-X', '-qAt', '-v', 'ON_ERROR_STOP=1'], input=statement, capture_output=True, text=True, env=environment)
            if run.returncode:
                raise AssertionError('Exact owned read-only PostgreSQL query failed: ' + run.stderr[-1000:])
            return run.stdout.strip()

        db_oid, db_owner, db_marker = sql("select oid,pg_get_userbyid(datdba),coalesce(shobj_description(oid,'pg_database'),'') from pg_database where datname=current_database()").split('|', 2)
        assert int(db_oid) == binding['oid'] and db_owner == binding['owner']
        if database != record['database']:
            assert re.fullmatch('owned-export-journey-' + database.removeprefix('test_mcmods_journey_') + '[0-9a-f]{16}', db_marker)
        owner, marker = sql(f"select pg_get_userbyid(nspowner),obj_description(oid,'pg_namespace') from pg_namespace where nspname='{schema}'").split('|', 1)
        assert owner == binding['role'] and re.fullmatch('mcmods-owned-test:' + schema.removeprefix('mcmods_audit_') + '[0-9a-f]{16}', marker)

        # Read-only JetStream API requests over the exact owned embedded broker.
        # No consumer delivery, ACK, message replay or business subject publish.
        def jetstream_request(subject):
            parsed = urllib.parse.urlsplit(runtime['natsURL'])
            assert parsed.scheme == 'nats' and parsed.hostname == '127.0.0.1' and parsed.port and not parsed.username and not parsed.password
            assert subject.startswith('$JS.API.STREAM.INFO.') or subject.startswith('$JS.API.CONSUMER.INFO.')
            inbox = '_INBOX.mcmods_final_readonly_' + uuid.uuid4().hex
            with socket.create_connection(('127.0.0.1', parsed.port), timeout=5) as connection:
                stream = connection.makefile('rb')
                info = stream.readline(65536)
                assert info.startswith(b'INFO ')
                server = json.loads(info[5:])
                assert server['server_id'] == runtime['natsServerID'] and not server.get('auth_required', False)
                connection.sendall(('CONNECT {"verbose":false,"pedantic":false,"lang":"python","version":"audit-owned","name":"mcmods-final-readonly"}\r\nSUB ' + inbox + ' 1\r\nPUB ' + subject + ' ' + inbox + ' 0\r\n\r\n').encode())
                for _ in range(16):
                    line = stream.readline(65536)
                    if line == b'PING\r\n':
                        connection.sendall(b'PONG\r\n')
                    elif line.startswith(b'MSG '):
                        parts = line.decode().strip().split()
                        assert parts[1] == inbox and 0 < int(parts[-1]) < 1 << 20
                        body = stream.read(int(parts[-1])); assert stream.read(2) == b'\r\n'
                        result = json.loads(body); assert 'error' not in result
                        return result
                    else:
                        assert line in (b'+OK\r\n', b'PONG\r\n'), 'Unexpected owned broker protocol response'
            raise AssertionError('Owned JetStream read-only API response missing')

        def database():
            # Values above are validated public IDs / UUIDs; schema is guarded.
            return json.loads(sql(f"""begin transaction isolation level repeatable read read only;
set local statement_timeout='10s'; set local lock_timeout='3s'; set local search_path={schema},pg_catalog;
with project as(select * from mods where project_code='{runtime['projectPublicId']}'),
actor as(select * from users where public_id='{runtime['creatorPublicId']}'),
jobs as(select job.*,package.sha256 package_sha,package.content_verified_at,package.archive_file_id,package.minecraft_version,package.loader,
 version.public_id version_public,version.label version_label,source_file.public_id source_public,source_file.status source_status,
 source_file.source_hash_verified,source_file.sha256 source_sha,source_file.stored_sha256,source_file.size_bytes source_size,
 binding.id binding_id,binding.status binding_status,binding.bucket,binding.endpoint,binding.upload_object_key,binding.sealed_source_key,binding.stored_object_key,
 binding.source_verified_sha256,binding.stored_verified_sha256,binding.source_verified_size_bytes,binding.stored_verified_size_bytes,
 capability.max_expires_at raw_cap_max_expires_at
 from catalog_import_jobs job join project on project.id=job.mod_id join catalog_import_packages package on package.id=job.package_id
 join mod_content_versions version on version.id=job.target_version_id join oss_files source_file on source_file.id=package.archive_file_id
 left join oss_upload_byte_bindings binding on binding.file_id=source_file.id and binding.status='committed'
 left join oss_upload_capabilities capability on capability.bucket=binding.bucket and capability.endpoint=binding.endpoint and capability.object_key=binding.upload_object_key),
revisions as(select revision.* from catalog_import_revisions revision join project on project.id=revision.mod_id)
select jsonb_build_object('readOnly',current_setting('transaction_read_only'),'isolation',current_setting('transaction_isolation'),'snapshot',pg_current_snapshot()::text,'databaseNow',clock_timestamp(),
 'jobs',(select coalesce(jsonb_agg(to_jsonb(jobs)),'[]'::jsonb) from jobs),
 'revisions',(select coalesce(jsonb_agg(jsonb_build_object('id',revision.id,'jobId',revision.job_id,'status',revision.status,'active',revision.is_active,'namespace',revision.source_namespace,'submittedBy',usr.public_id)),'[]'::jsonb) from revisions revision left join users usr on usr.id=revision.submitted_by),
 'recipes',(select coalesce(jsonb_agg(jsonb_build_object('sourceId',snapshot.source_recipe_id,'type',type.canonical_id,'ownerNamespace',revision.source_namespace,'sourceModId',snapshot.source_mod_id,'sourceModVersion',snapshot.source_mod_version,'ownerMatches',recipe.owner_mod_id=project.id)),'[]'::jsonb) from recipe_import_snapshots snapshot join revisions revision on revision.id=snapshot.revision_id join recipes recipe on recipe.entity_id=snapshot.recipe_id join recipe_types type on type.entity_id=recipe.recipe_type_id cross join project),
 'candidates',(select coalesce(jsonb_agg(jsonb_build_object('recipeId',snapshot.source_recipe_id,'slot',binding.source_slot_id,'kind',candidate.ingredient_kind,'resource',resource.canonical_id,'amount',candidate.amount,'chanceAvailable',candidate.chance_available,'chance',candidate.chance,'chancePercent',candidate.chance_percent)),'[]'::jsonb) from recipe_import_binding_candidates candidate join recipe_import_bindings binding on binding.id=candidate.binding_id join recipe_import_snapshots snapshot on snapshot.id=binding.recipe_snapshot_id join revisions revision on revision.id=snapshot.revision_id left join game_resources resource on resource.entity_id=candidate.resource_id),
 'audits',(select coalesce(jsonb_agg(jsonb_build_object('revisionId',event.aggregate_key,'action',event.action,'actor',usr.public_id,'decision',event.metadata->>'decision','afterActive',event.metadata->'afterActive')),'[]'::jsonb) from audit_events event join revisions revision on revision.id=event.aggregate_key left join users usr on usr.id=event.actor_id where event.aggregate_type='catalog_import_revision'),
 'media',(select coalesce(jsonb_agg(jsonb_build_object('path',media.asset_path,'type',media.content_type,'sha256',media.sha256,'bytes',media.byte_length,'fileStatus',file.status)),'[]'::jsonb) from catalog_import_media media join revisions revision on revision.id=media.revision_id join oss_files file on file.id=media.oss_file_id),
 'outbox',(select coalesce(jsonb_agg(jsonb_build_object('eventId',event_id,'type',event_type,'subject',subject,'aggregateId',aggregate_id,'status',status)),'[]'::jsonb) from nats_outbox),
 'deletions',(select coalesce(jsonb_agg(jsonb_build_object('key',deletion.object_key,'status',deletion.status,'attempts',deletion.attempts,'deletedAt',deletion.deleted_at,'reason',deletion.reason,'nextAttemptAt',deletion.next_attempt_at,'rawBindingId',deletion.raw_upload_binding_id,'holdReason',deletion.raw_hold_reason,'failureClass',deletion.failure_class,'lastError',deletion.last_error)),'[]'::jsonb) from oss_object_deletion_outbox deletion where exists(select 1 from jobs where deletion.bucket=jobs.bucket and deletion.endpoint=jobs.endpoint and deletion.object_key in(jobs.upload_object_key,jobs.sealed_source_key,jobs.stored_object_key))),
 'pendingActivity',(select count(*) from activity_event_outbox),
 'creatorActivities',(select coalesce(jsonb_agg(jsonb_build_object('action',dictionary.code,'objectType',object.code,'n',entry.n)),'[]'::jsonb) from (select action_id,object_type_id,count(*) n from user_activity_events cross join actor where user_activity_events.user_id=actor.id and action_id in({7},{2}) group by action_id,object_type_id) entry join activity_actions dictionary on dictionary.id=entry.action_id join activity_object_types object on object.id=entry.object_type_id),
 'notificationCount',(select count(*) from notifications notification cross join actor where notification.recipient_id=actor.id and notification.template_key='mod_import_success' and notification.source_event_id in(select event_id from nats_outbox where event_type='notification.direct')),
 'activeTasks',(select coalesce(jsonb_agg(jsonb_build_object('id',id,'code',code,'condition',condition,'rewards',rewards)),'[]'::jsonb) from task_definitions where status='active'),
 'taskProgress',(select count(*) from user_task_progress),'experienceRows',(select count(*) from user_experience),'experienceTransactions',(select count(*) from experience_transactions),'currencyTransactions',(select count(*) from currency_transactions),
 'datasetVersion',(select version from catalog_dataset_state where singleton)); commit;"""))

        # Resolve actual dictionary IDs from the immutable existing public contract,
        # rather than accepting an arbitrary SQL error as a negative assertion.
        action_rows = sql(f"begin read only; set local search_path={schema},pg_catalog; select code||'|'||id from activity_actions where code in ('upload','create'); commit;")
        action_ids = dict(line.split('|') for line in action_rows.splitlines())
        # The source contract currently assigns these IDs; fail clearly if changed.
        assert action_ids == {'create': '2', 'upload': '7'}, 'Declared activity dictionary changed; update the frozen observer before business'

        settle = []
        observation_started = observer_started
        until = observation_started + CORE_SETTLE_SECONDS
        core_stream_name = runtime['natsStream']
        assert core_stream_name == 'MCMODS_AUDIT_FINAL_GAME_JOURNEY'
        core_log = output / 'core-settling.jsonl'
        core_log.touch(exist_ok=False)
        while True:
            data = database()
            assert data['readOnly'] == 'on' and data['isolation'] == 'repeatable read'
            assert len(data['jobs']) == 1
            job = data['jobs'][0]
            keys = {job['upload_object_key'], job['sealed_source_key'], job['stored_object_key']}
            protected_source_keys = {job['sealed_source_key'], job['stored_object_key']}
            done_keys = {entry['key'] for entry in data['deletions'] if entry['status'] == 'completed' and entry['deletedAt']}
            raw_intents = [entry for entry in data['deletions'] if entry['key'] == job['upload_object_key'] and entry['rawBindingId'] == job['binding_id']]
            # A new supported upload must have pre-disclosure capability evidence.
            # Never substitute binding.created_at/current configuration for it.
            assert job['raw_cap_max_expires_at'], 'Fresh source missing its typed actual issued-capability expiry'
            assert len(raw_intents) == 1, 'Fresh consumed raw alias missing one exact binding-scoped durable retirement intent'
            raw_intent = raw_intents[0]
            max_expiry = instant(job['raw_cap_max_expires_at'])
            raw_not_before = instant(raw_intent['nextAttemptAt'])
            assert raw_not_before >= max_expiry + datetime.timedelta(seconds=1), 'Raw retirement intent precedes actual maximum capability expiry+1s'
            queue_observations = [jetstream_request('$JS.API.CONSUMER.INFO.' + core_stream_name + '.' + name) for name in ['mcmods-export-import-workers', 'mcmods-notification-workers']]
            queue_ready = all(info['num_pending'] == info['num_ack_pending'] == info['num_redelivered'] == 0 and info['delivered']['consumer_seq'] == info['ack_floor']['consumer_seq'] == 1 for info in queue_observations)
            current_metrics = load(runtime_path)['activityMetrics']
            monitor_ready = current_metrics['flushedDurable'] == 3 and current_metrics['durableBacklog'] == 0 and current_metrics['flushFailures'] == 0
            ready = queue_ready and monitor_ready and data['pendingActivity'] == 0 and data['notificationCount'] == 1 and protected_source_keys <= done_keys and len(data['outbox']) == 2 and all(entry['status'] == 'published' for entry in data['outbox'])
            settle.append({'at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'databaseNow': data['databaseNow'], 'pendingActivity': data['pendingActivity'], 'notificationCount': data['notificationCount'], 'distinctSourceKeys': len(keys), 'completedSourceKeys': len(done_keys), 'rawStatus': raw_intent['status'], 'rawAttempts': raw_intent['attempts'], 'rawNotBefore': raw_intent['nextAttemptAt'], 'rawHoldReason': raw_intent['holdReason'], 'rawFailureClass': raw_intent['failureClass'], 'actualCapabilityMaxExpiry': job['raw_cap_max_expires_at'], 'publishedNATS': sum(entry['status'] == 'published' for entry in data['outbox']), 'actualQueueDrained': queue_ready, 'actualMonitorDrained': monitor_ready})
            with core_log.open('a') as stream:
                stream.write(json.dumps(settle[-1]) + '\n')
            if ready:
                break
            assert time.monotonic() < until, 'Declared120s core queue/activity/notification/sealed-source settling budget exceeded'
            time.sleep(1)
        (output / 'core-settling.json').write_text(json.dumps(settle, indent=2) + '\n')
        check('Fresh typed raw retirement waits for actual maximum capability expiry', instant(data['databaseNow']) < raw_not_before and raw_intent['status'] == 'pending' and raw_intent['attempts'] == 0 and raw_intent['holdReason'] in ('', 'raw_capability_live') and raw_intent['failureClass'] == raw_intent['lastError'] == '')
        raw_remaining_seconds = (raw_not_before - instant(data['databaseNow'])).total_seconds()
        raw_allowed_seconds = raw_remaining_seconds + RAW_NORMAL_POLL_SECONDS + RAW_POLL_MARGIN_SECONDS
        check('Real frontend60min direct-PUT lifecycle fits the predeclared61min observation', 0 < raw_allowed_seconds <= RAW_OBSERVATION_CEILING_SECONDS)
        raw_deadline = min(observation_started + RAW_OBSERVATION_CEILING_SECONDS, time.monotonic() + raw_allowed_seconds)

        preflight = load(runtime_path).get('rawMultipartListPreflight', {})
        check('Actual SDK strict-TLS exact observed raw-key root listing preflight', preflight.get('status') == 'PASS' and preflight.get('actualSDK') is True and preflight.get('readOnly') is True and preflight.get('rawKeySHA256') == sha(job['upload_object_key'].encode()) and preflight.get('prefixExact') is True and preflight.get('maxUploads') == 32 and preflight.get('encodingType') == 'url' and preflight.get('emptyMarkers') is True and preflight.get('performedBeforeExpiry') is True and preflight.get('tlsVerifiedNormally') is True)
        check('One browser-created actual ready job and released run lease', job['id'] == native['jobId'] and job['status'] == 'ready' and job['progress'] == 100 and job['attempt_count'] == 1 and job['run_token'] == '')
        check('Exact verified source package and sealed-byte receipt', job['package_sha'] == job['source_sha'] == job['stored_sha256'] == job['source_verified_sha256'] == job['stored_verified_sha256'] == EXPECTED_SHA and job['content_verified_at'] and job['source_hash_verified'] and job['source_size'] == job['source_verified_size_bytes'] == job['stored_verified_size_bytes'] == 18290 and job['binding_status'] == 'committed')
        check('Correct target/current source namespace', job['version_public'] == runtime['targetVersionPublicId'] and job['version_label'] == '1.0.0' and job['minecraft_version'] == '1.20.1' and job['loader'] == 'fabric')
        check('One independently approved active revision', len(data['revisions']) == 1 and data['revisions'][0] == {'id': native['revisionId'], 'jobId': native['jobId'], 'status': 'ready', 'active': True, 'namespace': 'auditfixture', 'submittedBy': native['creatorId']} and len(data['audits']) == 1 and data['audits'][0]['actor'] == native['reviewerId'] and data['audits'][0]['decision'] == 'approved' and data['audits'][0]['afterActive'] is True)
        check('Two original owned crafting recipes', {entry['sourceId'] for entry in data['recipes']} == {'auditfixture:diamond_from_dirt', 'auditfixture:stone_from_cobblestone'} and len(data['recipes']) == 2 and all(entry['type'] == 'minecraft:crafting' and entry['ownerNamespace'] == entry['sourceModId'] == 'auditfixture' and entry['sourceModVersion'] == '1.0.0' and entry['ownerMatches'] for entry in data['recipes']))
        check('Original quantities and absent probability retained', {(entry['recipeId'], entry['resource'], entry['amount']) for entry in data['candidates'] if entry['slot'] == 'output_0'} == {('auditfixture:diamond_from_dirt', 'minecraft:diamond', 2), ('auditfixture:stone_from_cobblestone', 'minecraft:stone', 1)} and all(entry['chanceAvailable'] is False and entry['chance'] is None and entry['chancePercent'] is None for entry in data['candidates']))
        check('Actual normalized durable activities, no backlog', data['pendingActivity'] == 0 and {(entry['action'], entry['objectType'], entry['n']) for entry in data['creatorActivities']} == {('upload', 'mod', 3), ('create', 'mod', 1)})
        check('Existing default no-task policy, no invented XP/currency rewards', data['activeTasks'] == [] and data['taskProgress'] == data['experienceRows'] == data['experienceTransactions'] == data['currencyTransactions'] == 0)
        check('Actual exactly-once import notification and published NATS intents', data['notificationCount'] == 1 and {entry['type'] for entry in data['outbox']} == {'mod.catalog_import.requested', 'notification.direct'} and len(data['outbox']) == 2 and all(entry['status'] == 'published' for entry in data['outbox']))
        check('Consumed source tombstone and actual sealed/stored cleanup while raw is safely pending', job['source_status'] == 'deleted' and len(keys) == 2 and protected_source_keys <= done_keys and all(entry['attempts'] == 1 for entry in data['deletions'] if entry['key'] in protected_source_keys))

        opener = urllib.request.build_opener(NoRedirect, urllib.request.HTTPSHandler(context=tls_context))
        allowed_origins = {api, gateway, oss}

        def fetch(url, method='GET'):
            parsed = urllib.parse.urlsplit(url)
            assert parsed.scheme + '://' + parsed.netloc in allowed_origins and not parsed.username and not parsed.password
            request = urllib.request.Request(url, method=method, headers={'Accept-Encoding': 'identity', 'User-Agent': 'MCMods-owned-final-readonly'})
            try:
                response = opener.open(request, timeout=10)
            except urllib.error.HTTPError as error:
                response = error
            body = response.read(4 << 20)
            if response.status in (301, 302, 303, 307, 308):
                location = urllib.parse.urljoin(url, response.headers['Location'])
                next_url = urllib.parse.urlsplit(location)
                assert next_url.scheme + '://' + next_url.netloc in allowed_origins
                try:
                    final = opener.open(urllib.request.Request(location, method=method, headers={'Accept-Encoding': 'identity'}), timeout=10)
                except urllib.error.HTTPError as error:
                    final = error
                body = final.read(4 << 20)
                return final.status, body, final.headers
            return response.status, body, response.headers

        early_source_checks = []
        for role, key in [('upload', job['upload_object_key']), ('sealed', job['sealed_source_key']), ('stored', job['stored_object_key'])]:
            status, _, _ = fetch(oss + '/' + urllib.parse.quote(key, safe='/'), 'HEAD')
            early_source_checks.append({'role': role, 'keySHA256': sha(key.encode()), 'actualOwnedStorageHEAD': status})
            check('Actual early source lifecycle ' + role, status == (200 if role == 'upload' else 404))
        status, raw, _ = fetch(api + '/api/v1/oss/files/' + job['source_public'] + '/content')
        check('Anonymous consumed source cannot become a public inline asset', status == 404 and json.loads(raw)['error'] == 'public OSS file does not exist')
        media_checks = []
        for media in data['media']:
            assert media['fileStatus'] == 'active'
            path = '/api/v1/export-revisions/' + native['revisionId'] + '/assets/content?' + urllib.parse.urlencode({'path': media['path']})
            status, body, _ = fetch(api + path)
            check('Actual derived media bytes ' + media['path'], status == 200 and len(body) == media['bytes'] and sha(body) == media['sha256'])
            item = {'path': media['path'], 'bytes': len(body), 'SHA256': sha(body), 'status': status}
            if media['type'] == 'image/png':
                assert media['path'] in source_images
                original = source_images[media['path']]
                assert original[:8] == b'\x89PNG\r\n\x1a\n' and original[24] == 8
                source_image = Image.open(io.BytesIO(original)); actual_image = Image.open(io.BytesIO(body))
                check('Source PNG dimensions/pixels/alpha survive actual public storage', source_image.size == actual_image.size == (496, 248) and source_image.convert('RGBA').tobytes() == actual_image.convert('RGBA').tobytes())
                item['RGBA_SHA256'] = sha(actual_image.convert('RGBA').tobytes()); item['dimensions'] = list(actual_image.size)
            media_checks.append(item)
        check('Exactly three actual public media rows', len(media_checks) == 3)

        stream_name = runtime['natsStream']; assert stream_name == 'MCMODS_AUDIT_FINAL_GAME_JOURNEY'
        stream = jetstream_request('$JS.API.STREAM.INFO.' + stream_name)
        check('Actual official file-backed JetStream', stream['config']['storage'] == 'file' and stream['config']['name'] == stream_name)
        consumers = []
        for name in ['mcmods-export-import-workers', 'mcmods-notification-workers', 'mcmods-project-update-workers', 'mcmods-audit-final-game-journey-dead-letter-recovery']:
            info = jetstream_request('$JS.API.CONSUMER.INFO.' + stream_name + '.' + name)
            check('Actual consumer drained ' + name, info['name'] == name and info['num_pending'] == info['num_ack_pending'] == info['num_redelivered'] == 0)
            if name in ['mcmods-export-import-workers', 'mcmods-notification-workers']:
                check('Exactly one actual delivery and ACK ' + name, info['delivered']['consumer_seq'] == info['ack_floor']['consumer_seq'] == 1)
            consumers.append(info)
        (output / 'actual-jetstream.json').write_text(json.dumps({'stream': stream, 'consumers': consumers, 'readOnlyAPI': True}, indent=2) + '\n')

        current = load(runtime_path)
        check('Actual Monitor consumed durable events with default production options', current['activityScope']['actualMonitor'] and current['activityScope']['actualProgression'] and current['activityScope']['noSyntheticRewardPolicy'] and current['activityMetrics']['flushedDurable'] == 3 and current['activityMetrics']['durableBacklog'] == 0 and current['activityMetrics']['flushFailures'] == 0)
        check('Catalog dataset moved once for independent publication', baseline['activeRevisions'] == 0 and data['datasetVersion'] == 2)
        attempts = [json.loads(line) for line in (directory / 'http-attempts.ndjson').read_text().splitlines()]
        expected_paths = [('/api/v1/mods/' + runtime['projectSiteId'] + '/export-imports/uploads/presign', 'POST'), ('/api/v1/mods/' + runtime['projectSiteId'] + '/export-imports/uploads/complete', 'POST'), ('/api/v1/mods/' + runtime['projectSiteId'] + '/export-imports', 'POST'), ('/api/v1/admin/export-revisions/' + native['revisionId'] + '/activate', 'PATCH')]
        core_attempts = [entry for entry in attempts if entry['path'].startswith('/api/v1/mods/' + runtime['projectSiteId'] + '/export-imports') or entry['path'] == expected_paths[-1][0]]
        check('Exact ordinary browser core write route set, no direct Worker/SQL job', [(entry['path'], entry['method']) for entry in core_attempts] == expected_paths and [entry['status'] for entry in core_attempts] == [200, 201, 202, 200] and [entry['actorPublicId'] for entry in core_attempts] == [native['creatorId']] * 3 + [native['reviewerId']])
        check('Current actual settings unchanged from before business', sha(sql(f"begin read only; set local search_path={schema},pg_catalog; select coalesce(jsonb_agg(jsonb_build_array(key,value) order by key),'[]'::jsonb) from system_settings; commit;").encode()) == runtime['initialSettingsSHA256'])
        public_checks = []
        status, raw, _ = fetch(api + '/api/v1/recipe-types?canonicalId=minecraft%3Acrafting&limit=100')
        assert status == 200
        recipe_type = next(entry for entry in json.loads(raw)['data']['items'] if entry['canonicalId'] == 'minecraft:crafting')
        paths = ['/api/v1/recipe-types?canonicalId=minecraft%3Acrafting&limit=100', '/api/v1/recipe-types/' + recipe_type['publicId'] + '/catalog?limit=100', '/api/v1/catalog/recipes/' + native['recipePublicId'] + '/render?locale=en-US&secondaryLocale=zh-CN']
        meaningful_headers = ['Content-Type', 'Cache-Control', 'Cross-Origin-Resource-Policy', 'X-Content-Type-Options', 'X-Frame-Options', 'Referrer-Policy', 'Permissions-Policy', 'Content-Security-Policy']
        for path in paths:
            pair = []
            for base in (api, gateway_api):
                status, body, headers = fetch(base + path)
                pair.append({'status': status, 'bodySHA256': sha(body), 'bytes': len(body), 'headers': {key: headers.get(key, '') for key in meaningful_headers}, 'varyTokens': sorted({token.strip().lower() for field in headers.get_all('Vary', []) for token in field.split(',')})})
                if '/catalog?' in path:
                    catalog = json.loads(body)['data']
                    check('Anonymous exact original recipe catalog at ' + base, {entry['recipeId'] for entry in catalog['recipes'] if entry['recipeId'].startswith('auditfixture:')} == {'auditfixture:diamond_from_dirt', 'auditfixture:stone_from_cobblestone'})
            check('Faithful same-origin body/security/cache/Vary ' + path, pair[0] == pair[1] and pair[0]['status'] == 200)
            public_checks.append({'path': path, 'responses': pair})

        def safe_snapshot(snapshot):
            sanitized = copy.deepcopy(snapshot)
            for entry in sanitized['jobs']:
                for key in ['upload_object_key', 'sealed_source_key', 'stored_object_key']:
                    entry[key + '_SHA256'] = sha(entry.pop(key).encode())
            for entry in sanitized['deletions']:
                entry['keySHA256'] = sha(entry.pop('key').encode())
            return sanitized

        initial_database = copy.deepcopy(data)
        check('All core native/PG/queue/asset/cache assertions fit the declared120s observer phase', time.monotonic() <= observer_started + CORE_SETTLE_SECONDS)
        (output / 'database-core.json').write_text(json.dumps(safe_snapshot(initial_database), indent=2) + '\n')
        (output / 'core-acceptance.json').write_text(json.dumps({'status': 'CORE_ASSERTIONS_PASS_PENDING_REAL_RAW_EXPIRY_AND_PHYSICAL_DELETION', 'sourceRevisions': runtime['repoRevisions'], 'sampleSHA256': EXPECTED_SHA, 'checks': list(report['checks']), 'earlySourceChecks': early_source_checks, 'rawActualCapabilityMaxExpiry': job['raw_cap_max_expires_at'], 'rawNotBefore': raw_intent['nextAttemptAt'], 'rawPhysicalObservationCeilingSeconds': RAW_OBSERVATION_CEILING_SECONDS, 'completeJourneyOrIssueAcceptance': False}, indent=2) + '\n')

        # This is a separate predeclared real-wall-clock phase, not an extension
        # of the120s core budget. No TTL/clock/next_attempt_at is changed.
        raw_observations = []
        raw_log = output / 'raw-expiry-observations.jsonl'
        raw_log.touch(exist_ok=False)
        while True:
            final_data = database()
            assert len(final_data['jobs']) == 1
            final_job = final_data['jobs'][0]
            assert final_job['raw_cap_max_expires_at'] == job['raw_cap_max_expires_at'], 'Unexpected new capability issuance outside declared direct-PUT native route set'
            current_raw = [entry for entry in final_data['deletions'] if entry['key'] == job['upload_object_key'] and entry['rawBindingId'] == job['binding_id']]
            assert len(current_raw) == 1
            current_raw = current_raw[0]
            db_now = instant(final_data['databaseNow'])
            actual_raw_status, _, _ = fetch(oss + '/' + urllib.parse.quote(job['upload_object_key'], safe='/'), 'HEAD')
            raw_observations.append({'at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'databaseNow': final_data['databaseNow'], 'status': current_raw['status'], 'attempts': current_raw['attempts'], 'notBefore': current_raw['nextAttemptAt'], 'actualRawStorageHEAD': actual_raw_status, 'holdReason': current_raw['holdReason'], 'failureClass': current_raw['failureClass']})
            with raw_log.open('a') as stream:
                stream.write(json.dumps(raw_observations[-1]) + '\n')
            if db_now < raw_not_before:
                assert current_raw['status'] == 'pending' and current_raw['attempts'] == 0 and actual_raw_status == 200, 'Live capability raw cleanup occurred early or eligibility consumed failure budget'
            if current_raw['status'] == 'completed' and current_raw['deletedAt'] and actual_raw_status == 404:
                assert db_now >= max_expiry + datetime.timedelta(seconds=1) and current_raw['attempts'] == 1 and current_raw['holdReason'] == current_raw['failureClass'] == current_raw['lastError'] == ''
                assert instant(current_raw['deletedAt']) <= max_expiry + datetime.timedelta(seconds=1 + RAW_NORMAL_POLL_SECONDS + RAW_POLL_MARGIN_SECONDS), 'Actual raw retirement missed its normal expiry+poll/margin deadline'
                break
            assert current_raw['status'] in ('pending', 'processing'), 'Raw retirement entered a real failure/HOLD state'
            assert time.monotonic() < raw_deadline, 'Predeclared actual maxExpiry+normal poll/margin raw physical window exceeded'
            time.sleep(1)
        (output / 'raw-expiry-observations.json').write_text(json.dumps(raw_observations, indent=2) + '\n')
        check('Actual ready job/source receipt unchanged during real expiry phase', final_job == job)
        for key in ['revisions', 'recipes', 'candidates', 'audits', 'media', 'outbox', 'pendingActivity', 'creatorActivities', 'notificationCount', 'activeTasks', 'taskProgress', 'experienceRows', 'experienceTransactions', 'currencyTransactions', 'datasetVersion']:
            before, after = initial_database[key], final_data[key]
            if isinstance(before, list):
                # PostgreSQL rowset traversal order is not a business-array
                # order. Preserve cardinality/duplicates/all fields exactly.
                before = sorted(json.dumps(row, sort_keys=True) for row in before)
                after = sorted(json.dumps(row, sort_keys=True) for row in after)
            check('Core postcondition unchanged after actual raw cleanup ' + key, before == after)
        source_checks = []
        for role, key in [('upload', job['upload_object_key']), ('sealed', job['sealed_source_key']), ('stored', job['stored_object_key'])]:
            status, _, _ = fetch(oss + '/' + urllib.parse.quote(key, safe='/'), 'HEAD')
            source_checks.append({'role': role, 'keySHA256': sha(key.encode()), 'actualOwnedStorageHEAD': status})
            check('Actual final consumed source storage absent ' + role, status == 404)
        final_done_keys = {entry['key'] for entry in final_data['deletions'] if entry['status'] == 'completed' and entry['deletedAt']}
        check('Every exact typed consumed alias has an actually completed deletion', keys == final_done_keys)
        for media in final_data['media']:
            status, body, _ = fetch(api + '/api/v1/export-revisions/' + native['revisionId'] + '/assets/content?' + urllib.parse.urlencode({'path': media['path']}))
            check('Public derived media preserved after real raw retirement ' + media['path'], status == 200 and len(body) == media['bytes'] and sha(body) == media['sha256'])
        safe_database = safe_snapshot(final_data)
        (output / 'database.json').write_text(json.dumps(safe_database, indent=2) + '\n')
        (output / 'source-lifecycle.json').write_text(json.dumps({'sourceTombstone': job['source_status'], 'earlyRoles': early_source_checks, 'finalRoles': source_checks, 'actualCapabilityMaxExpiry': job['raw_cap_max_expires_at'], 'originalRawNotBefore': raw_intent['nextAttemptAt'], 'actualDeletionIntentStates': safe_database['deletions'], 'noRetentionBypass': True, 'privateProductionBucketACLClaim': False}, indent=2) + '\n')
        (output / 'public-assets.json').write_text(json.dumps(media_checks, indent=2) + '\n')
        (output / 'gateway-cache.json').write_text(json.dumps(public_checks, indent=2) + '\n')
        check('Complete physical observation fits the predeclared61min ceiling', time.monotonic() <= observer_started + RAW_OBSERVATION_CEILING_SECONDS)
        report.update(status='PASS_FINAL_FIXED_COMBINATION_RAW059B_NATIVE_QUEUE_HTTP_PG', sampleSHA256=EXPECTED_SHA, backendCommit=runtime['repoRevisions']['mcmods-cn-backend']['commit'], sourceRevisions=runtime['repoRevisions'], nativeReport=native_path.name, nativeReportSHA256=sha(native_path.read_bytes()), nativeSourceCommit=runtime['repoRevisions']['mcmods-cn-frontend']['commit'], frontendProductionBuild=build, tls={'verified': True, 'leafCertificateDERSHA256': tls['leafCertificateDERSHA256'], 'trustedCABundleSHA256': tls['trustedCABundleSHA256']}, jobId=native['jobId'], revisionId=native['revisionId'], creatorPublicId=native['creatorId'], reviewerPublicId=native['reviewerId'], productionExecuted=False, remoteWrite=False, limits=['Original producer8062 raw Fabric1.20.1 sample reused unchanged; no newly generated game result.', 'Controlled synthetic OSS; actual production bucket ACL/lifecycle and official suppliers not certified.', 'Actual default Monitor/Progression with zero configured reward tasks correctly produces no XP/currency awards; positive reward branch remains separately tested.', 'Actual pinned standalone production frontend and owned verified HTTPS gateway; controlled OSS protocol, other loader profiles, cloud lifecycle/distributed cache faults and all175/release judgment remain separate.'])
        # The live fixture additionally checks actual PostgreSQL facts on normal
        # STOP. This file authorizes no production action and never sends STOP.
        (output / 'acceptance.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        target = directory / 'terminal-acceptance.json'
        with target.open('x') as stream:
            json.dump(report, stream, ensure_ascii=False, indent=2); stream.write('\n')
    except Exception as error:
        report.update(status='FAIL', errorType=type(error).__name__, error=str(error))
        raise
    finally:
        (output / 'acceptance.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps({'status': report['status'], 'checks': len(report['checks']), 'evidence': str(output), 'productionExecuted': False}))


if __name__ == '__main__':
    main()
