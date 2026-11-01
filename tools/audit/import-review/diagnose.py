#!/usr/bin/env python3
"""Read-only owned pending review seal, after a native browser observation."""
import datetime,hashlib,json,os,pathlib,re,subprocess,sys,urllib.request,urllib.error
rp=pathlib.Path(sys.argv[1]).resolve();out=pathlib.Path(sys.argv[2]).resolve();r=json.loads(rp.read_text());schema=r['ownedSchema']
assert re.fullmatch('mcmods_audit_[0-9a-f]{16}',schema) and r['owner']=='mcmods-live-import-review' and r['state']=='READY'
assert (rp.parent/'OWNER').read_text().strip()==f"mcmods-live-import-review pid={r['pid']}"
proc=pathlib.Path('/proc/'+str(r['pid']));assert proc.stat().st_uid==os.getuid() and b'TestAuditGR004RealConfiguredWildcardScopePreservesDenyAndSelfHTTPPostgresIntegration' in (proc/'cmdline').read_bytes()
env=dict(os.environ)
for k in ['DOCKER_HOST','DOCKER_CONTEXT','DOCKER_TLS','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH']:env.pop(k,None)
docker=['docker','--host=unix:///var/run/docker.sock'];container='mcmods-audit-20261024-postgres18'
labels=json.loads(subprocess.check_output(docker+['inspect','--format','{{json .Config.Labels}}',container],text=True,env=env));assert labels.get('mcmods.audit.task')=='20261024'
def sql(q):return subprocess.check_output(docker+['exec','-i',container,'psql','-U','mcmods_audit','-d','mcmods_audit_test','-X','-qAt','-v','ON_ERROR_STOP=1'],input=q,text=True,env=env).strip()
owner,marker=sql(f"select pg_get_userbyid(nspowner),obj_description(oid,'pg_namespace') from pg_namespace where nspname='{schema}'").split('|',1)
assert owner=='mcmods_audit' and re.fullmatch('mcmods-owned-test:'+schema.removeprefix('mcmods_audit_')+'[0-9a-f]{16}',marker)
out.mkdir(mode=0o700,exist_ok=False)
raw=sql(f'''begin transaction isolation level repeatable read read only;set local search_path={schema},public;
select jsonb_build_object('schema',current_schema(),
 'jobs',(select count(*) from catalog_import_jobs), 'files',(select count(*) from oss_files),
 'artifacts',(select count(*) from catalog_import_job_artifacts),'media',(select count(*) from catalog_import_media),
 'decisions',(select count(*) from audit_events where action in ('review_approved','review_rejected')),
 'imports',(select jsonb_agg(jsonb_build_object('revisionId',revision.id,'jobId',job.id,'projectId',mod.project_code,'jobStatus',job.status,'revisionStatus',revision.status,'active',revision.is_active,'sourceSHA256',file.sha256,'creatorPreserved',job.created_by=file.uploader_id and package.uploaded_by=job.created_by and revision.submitted_by=job.created_by) order by revision.id) from catalog_import_revisions revision join catalog_import_jobs job on job.id=revision.job_id join mods mod on mod.id=revision.mod_id join catalog_import_packages package on package.id=job.package_id join oss_files file on file.id=package.archive_file_id));commit;''')
data=json.loads(raw);(out/'database-snapshot.json').write_text(json.dumps(data,indent=2)+'\n')
assert data['jobs']==data['files']==len(data['imports'])==2 and data['artifacts']==data['media']==data['decisions']==0
assert all(i['jobStatus']==i['revisionStatus']=='ready' and not i['active'] and i['creatorPreserved'] for i in data['imports'])
i=next(i for i in data['imports'] if i['revisionId']==r['revisionId']);assert i['jobId']==r['jobId'] and i['projectId']==r['projectId'] and i['sourceSHA256']==r['sourceSHA256']
auth=pathlib.Path(r['authFile']);assert auth.stat().st_mode&0o777==0o600
secrets=json.loads(auth.read_text());token=secrets['reviewerToken']
checks=[]
def http(path,credential,want):
 req=urllib.request.Request(r['apiOrigin']+path,headers={'Authorization':'Bearer '+credential} if credential else {})
 try:
  with urllib.request.urlopen(req) as response:status=response.status;body=response.read()
 except urllib.error.HTTPError as e:status=e.code;body=e.read()
 assert status==want,(path,status,want)
 checks.append({'path':path,'status':status,'bodySHA256':hashlib.sha256(body).hexdigest(),'authenticated':bool(credential)})
 return json.loads(body)
q=http(r['queuePath'],token,200)['data'];assert q['total']==len(q['items'])==1 and q['items'][0]['id']==r['revisionId']
for key in ['categories','operations','projectTypes']:assert len(q['facets'][key])==1 and sum(v['count'] for v in q['facets'][key])==1
assert r['deniedRevisionId'] not in json.dumps(q)
d=http('/api/v1/export-revisions/'+r['revisionId'],token,200)['data'];assert d['canReview'] and d['package']['sha256']==r['sourceSHA256'] and d['targetVersionPublicId']==r['targetVersionPublicId'] and d['namespace']==r['expectedNamespace']
for path,credential in [('/api/v1/export-revisions/'+r['revisionId'],''),('/api/v1/export-revisions/'+r['deniedRevisionId'],token)]:
 denied=http(path,credential,403);assert 'data' not in denied
result={'status':'PASS_READ_ONLY_CURRENT_SCOPE_PENDING_AND_ZERO_EXTRA_UPLOADS','recordedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sourceRevisions':r['repoRevisions'],'checks':checks,'jobs':2,'files':2,'media':0,'artifacts':0,'independentReviewDecisionsBeforeStop':0,'authPrivateOpened':True,'authScope':'Only synthetic actual reviewer token consumed in memory; no credential material written','databaseSHA256':hashlib.sha256((out/'database-snapshot.json').read_bytes()).hexdigest(),'driverSHA256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),'productionExecution':False}
(out/'acceptance.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({'status':result['status'],'checks':len(checks)}))
