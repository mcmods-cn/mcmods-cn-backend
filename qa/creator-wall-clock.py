"""Hold only the serving fixture long enough to observe an actual one-hour policy window."""
import difflib, hashlib, json, pathlib, sys
repo, work=map(pathlib.Path,sys.argv[1:])
src=repo/'internal/httpapi/audit_live_oss001_creator_fixture_20261101_test.go'
before=src.read_text()
old='f := newAuditFixGRFixture(t, 45*time.Minute)'
assert before.count(old)==1
after=before.replace(old,'f := newAuditFixGRFixture(t, 75*time.Minute)')
alternate=work/src.name
alternate.write_text(after)
(work/'creator-overlay.json').write_text(json.dumps({'Replace':{str(src):str(alternate)}}))
(work/'identity-public'/'fixture-lifetime.json').write_text(json.dumps({
  'path':str(src.relative_to(repo)), 'appliesOnlyTo':'optional same-actor real-time recovery',
  'baseSHA256':hashlib.sha256(before.encode()).hexdigest(),'testOverlaySHA256':hashlib.sha256(after.encode()).hexdigest(),
  'diff':list(difflib.unified_diff(before.splitlines(),after.splitlines(),n=1,lineterm=''))
},indent=2))
