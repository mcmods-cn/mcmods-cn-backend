"""V2 ownership/cleanup components; no real services, Go builds or signals."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock


spec = importlib.util.spec_from_file_location('live_acceptance_v2', Path(__file__).with_name('live_acceptance_v2.py'))
live = importlib.util.module_from_spec(spec)
spec.loader.exec_module(live)


class V2DatabaseOwnershipTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        live.SCOPE = self.root / 'new-owned-scope'
        live.SCOPE.mkdir()
        live.BASE = live.SCOPE
        self.parent = {'owner': 'a' * 32, 'database': 'test_mcmods_audit_' + 'b' * 16,
                       'containers': {'postgres': 'owned-postgres', 'redis': 'owned-redis', 'nats': 'owned-nats'}}
        self.identity = {'name': 'test_mcmods_live_' + 'c' * 20, 'oid': 12345, 'owner': 'mcmods',
                         'marker': 'mcmods-live-v2-owned:' + 'd' * 32, 'current_user': 'mcmods'}
        self.proof = {k: self.identity[k] for k in ('name', 'oid', 'owner', 'marker')}
        self.proof['proof_state'] = 'CONFIRMED'
        self.record = {'parent_owner': self.parent['owner'], 'parent_container': 'owned-postgres',
                       'database': self.identity['name'], 'created': True, 'runtime_started': False,
                       'nonce_namespace': 'e' * 32, 'cleanup_version': 2, 'database_creation': self.proof}
        self.environment = {'REDIS_NAMESPACE': 'live_' + 'e' * 32,
                            'NATS_SUBJECT_PREFIX': 'live.' + 'e' * 32,
                            'NATS_JETSTREAM_STREAM': 'LIVE_' + 'E' * 32,
                            'REDIS_ADDR': '127.0.0.1:16379', 'NATS_URL': 'nats://127.0.0.1:14222'}
        self.write_record(self.record)
        (live.SCOPE / 'environment.json').write_text(json.dumps(self.environment))

    def write_record(self, record):
        (live.SCOPE / 'scope.json').write_text(json.dumps(record))

    def test_legacy_incomplete_or_malformed_creation_never_reaches_sql(self):
        changes = ['missing-version', 'wrong-version-type', 'missing-proof', 'pending-nonce',
                   'unknown-oid', 'bool-oid', 'wrong-name', 'wrong-owner', 'wrong-marker', 'unknown-field']
        for change in changes:
            with self.subTest(change=change):
                record = copy.deepcopy(self.record)
                if change == 'missing-version':
                    record.pop('cleanup_version')
                elif change == 'wrong-version-type':
                    record['cleanup_version'] = 2.0
                elif change == 'missing-proof':
                    record.pop('database_creation')
                else:
                    key, value = {'pending-nonce': ('proof_state', 'COMMENT_PENDING'),
                                  'unknown-oid': ('oid', 0), 'bool-oid': ('oid', True),
                                  'wrong-name': ('name', 'unrelated_database'),
                                  'wrong-owner': ('owner', 'other_owned_role'),
                                  'wrong-marker': ('marker', 'unknown'),
                                  'unknown-field': ('guessed_from_current_name', True)}[change]
                    record['database_creation'][key] = value
                self.write_record(record)
                before = (live.SCOPE / 'scope.json').read_bytes()
                with mock.patch.object(live, 'sql') as sql, mock.patch.object(live.subprocess, 'run') as command:
                    with self.assertRaisesRegex(RuntimeError, 'HOLD, never adopt'):
                        live.scope_record(self.parent)
                    sql.assert_not_called()
                    command.assert_not_called()
                self.assertEqual((live.SCOPE / 'scope.json').read_bytes(), before)

    def test_foreign_name_is_rejected_before_sql(self):
        with mock.patch.object(live, 'sql') as sql:
            with self.assertRaisesRegex(RuntimeError, 'unowned child'):
                live.read_child_database_identity(self.parent, self.parent['database'])
            sql.assert_not_called()

    def test_current_identity_mismatch_prevents_namespace_and_drop(self):
        for key, value in [('oid', 12346), ('owner', 'another_role'),
                           ('marker', 'mcmods-live-v2-owned:' + 'f' * 32)]:
            with self.subTest(key=key):
                current = dict(self.identity, **{key: value})
                before = (live.SCOPE / 'scope.json').read_bytes()
                with mock.patch.object(live, 'guarded_parent', return_value=(self.parent, {})), \
                        mock.patch.object(live, 'sql', return_value=json.dumps(current)) as sql, \
                        mock.patch.object(live.subprocess, 'run') as command:
                    with self.assertRaisesRegex(RuntimeError, 'identity changed'):
                        live.cleanup()
                    command.assert_not_called()
                    self.assertEqual(sql.call_count, 1)
                    self.assertTrue(sql.call_args.args[2].startswith('select json_build_object'))
                self.assertEqual((live.SCOPE / 'scope.json').read_bytes(), before)

    def test_catalog_metadata_validation_fails_closed(self):
        for patch in [{'oid': True}, {'oid': '12345'}, {'oid': 0}, {'current_user': 'unexpected_role'}]:
            with self.subTest(patch=patch), mock.patch.object(live, 'sql', return_value=json.dumps(dict(self.identity, **patch))):
                with self.assertRaisesRegex(RuntimeError, 'metadata is invalid'):
                    live.read_child_database_identity(self.parent, self.record['database'])

    def test_fresh_creation_persists_pending_before_confirming_nonce(self):
        record = {k: v for k, v in self.record.items() if k != 'database_creation'}
        record['created'] = False
        marker = 'mcmods-live-v2-owned:' + 'a' * 32
        initial = dict(self.identity, marker='')
        confirmed = dict(self.identity, marker=marker)
        with mock.patch.object(live, 'sql', side_effect=[json.dumps(initial), '', json.dumps(confirmed)]) as sql, \
                mock.patch.object(live.secrets, 'token_hex', return_value='a' * 32):
            live.record_original_database_creation(self.parent, record)
        self.assertEqual(sql.call_count, 3)
        self.assertEqual(record['database_creation']['proof_state'], 'CONFIRMED')
        self.assertFalse(record['created'])  # Only prepare marks complete after this helper.
        pending = json.loads((live.SCOPE / 'scope.json').read_text())
        self.assertFalse(pending['created'])
        self.assertEqual(pending['database_creation']['oid'], 12345)
        self.assertEqual(pending['database_creation']['proof_state'], 'COMMENT_PENDING')
        self.assertIn('comment on database "' + self.record['database'] + '"', sql.call_args_list[1].args[2])

    def test_comment_failure_retains_incomplete_original_receipt_and_never_drops(self):
        record = {k: v for k, v in self.record.items() if k != 'database_creation'}
        record['created'] = False
        with mock.patch.object(live, 'sql', side_effect=[json.dumps(dict(self.identity, marker='')),
                                                       RuntimeError('owned SQL failed: XX001 nonce persistence fault')]) as sql:
            with self.assertRaisesRegex(RuntimeError, 'XX001'):
                live.record_original_database_creation(self.parent, record)
        self.assertEqual(sql.call_count, 2)
        pending = json.loads((live.SCOPE / 'scope.json').read_text())
        self.assertFalse(pending['created'])
        self.assertEqual(pending['database_creation']['proof_state'], 'COMMENT_PENDING')
        self.assertTrue(all('drop database' not in call.args[2].lower() for call in sql.call_args_list))

    def test_confirmation_drift_does_not_turn_partial_receipt_into_authority(self):
        record = {k: v for k, v in self.record.items() if k != 'database_creation'}
        record['created'] = False
        confirmed = dict(self.identity, marker='mcmods-live-v2-owned:' + 'a' * 32, oid=12346)
        with mock.patch.object(live, 'sql', side_effect=[json.dumps(dict(self.identity, marker='')), '', json.dumps(confirmed)]), \
                mock.patch.object(live.secrets, 'token_hex', return_value='a' * 32):
            with self.assertRaisesRegex(RuntimeError, 'nonce was not confirmed'):
                live.record_original_database_creation(self.parent, record)
        self.assertEqual(record['database_creation']['proof_state'], 'COMMENT_PENDING')
        self.assertFalse(record['created'])

    def test_drop_uses_fresh_exact_identity_ordinary_sql_and_absence_confirmation(self):
        with mock.patch.object(live, 'sql', side_effect=[json.dumps(self.identity), '', '0']) as sql:
            self.assertEqual(live.drop_original_child_database(self.parent, self.record), self.proof)
        commands = [call.args[2] for call in sql.call_args_list]
        self.assertEqual(commands[1], '\\set VERBOSITY verbose\ndrop database "' + self.record['database'] + '";')
        self.assertTrue(commands[2].startswith('select count(*) from pg_catalog.pg_database'))
        self.assertTrue(all('force' not in value.lower() and 'terminate_backend' not in value.lower() for value in commands))

    def namespace_result(self, *args, **kwargs):
        kwargs['stdout'].write(json.dumps({'redis_namespace_empty': True, 'nats_owned_stream_absent': True}))
        return SimpleNamespace(returncode=0)

    def cleanup_context(self, sql_responses):
        class Patches:
            def __enter__(inner):
                inner.stack = __import__('contextlib').ExitStack()
                enter = inner.stack.enter_context
                enter(mock.patch.object(live, 'guarded_parent', return_value=(self.parent, {})))
                enter(mock.patch.object(live, 'guard_scope_environment'))
                enter(mock.patch.object(live.owned, 'verify_container', return_value={
                    'NetworkSettings': {'Ports': {'6379/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '16379'}],
                                                 '4222/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '14222'}]}}}))
                inner.command = enter(mock.patch.object(live.subprocess, 'run', side_effect=self.namespace_result))
                inner.sql = enter(mock.patch.object(live, 'sql', side_effect=sql_responses))
                return inner

            def __exit__(inner, *error):
                return inner.stack.__exit__(*error)
        return Patches()

    def test_final_recheck_after_namespace_cleanup_rejects_metadata_drift(self):
        with self.cleanup_context([json.dumps(self.identity), json.dumps(dict(self.identity, oid=12346))]) as patch:
            with self.assertRaisesRegex(RuntimeError, 'identity changed'):
                live.cleanup()
            self.assertEqual(patch.command.call_count, 1)
            self.assertEqual(patch.sql.call_count, 2)
            self.assertTrue(all('drop database' not in call.args[2].lower() for call in patch.sql.call_args_list))
        self.assertTrue(json.loads((live.SCOPE / 'scope.json').read_text())['created'])
        self.assertFalse((live.BASE / 'backend-a-live-cleanup.json').exists())

    def test_exact_55006_failure_is_retained_without_success_or_termination(self):
        fault = RuntimeError('owned SQL failed: ERROR: 55006: database is being accessed by other users')
        with self.cleanup_context([json.dumps(self.identity), json.dumps(self.identity), fault]) as patch:
            with self.assertRaisesRegex(RuntimeError, '55006'):
                live.cleanup()
            self.assertEqual(patch.sql.call_count, 3)
            self.assertNotIn('force', patch.sql.call_args.args[2].lower())
            self.assertNotIn('terminate_backend', patch.sql.call_args.args[2].lower())
        self.assertTrue(json.loads((live.SCOPE / 'scope.json').read_text())['created'])
        self.assertFalse((live.BASE / 'backend-a-live-cleanup.json').exists())

    def test_success_marks_only_original_child_removed_after_absence(self):
        with self.cleanup_context([json.dumps(self.identity), json.dumps(self.identity), '', '0']) as patch:
            live.cleanup()
        self.assertEqual(patch.sql.call_count, 4)
        self.assertFalse(json.loads((live.SCOPE / 'scope.json').read_text())['created'])
        result = json.loads((live.BASE / 'backend-a-live-cleanup.json').read_text())
        self.assertEqual(result['cleanup_version'], 2)
        self.assertEqual(result['original_database_creation'], self.proof)
        self.assertFalse(result['forced_database_removal'])


if __name__ == '__main__':
    unittest.main()
