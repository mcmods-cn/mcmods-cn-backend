#!/usr/bin/env python3
"""Read-only seal of the task-owned synthetic three-kind recipe fixture."""
import datetime,hashlib,json,os,pathlib,re,subprocess,sys,urllib.request
runtime_path=pathlib.Path(sys.argv[1]).resolve();out=pathlib.Path(sys.argv[2]).resolve()
r=json.loads(runtime_path.read_text());schema=r['ownedSchema']
assert re.fullmatch(r'mcmods_audit_[0-9a-f]{16}',schema)
assert r['owner']=='mcmods-live-recipe-geometry' and r['state']=='READY'
assert (runtime_path.parent/'OWNER').read_text().strip()==f"mcmods-live-recipe-geometry pid={r['pid']}"
process=pathlib.Path(f"/proc/{r['pid']}");assert process.stat().st_uid==os.getuid()
assert b'TestAuditLiveRecipeGeometryFixture' in (process/'cmdline').read_bytes()
env=dict(os.environ)
for key in ['DOCKER_HOST','DOCKER_CONTEXT','DOCKER_TLS','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH']:env.pop(key,None)
docker=['docker','--host=unix:///var/run/docker.sock'];container='mcmods-audit-20261024-postgres18'
labels=json.loads(subprocess.check_output(docker+['inspect','--format','{{json .Config.Labels}}',container],text=True,env=env));assert labels.get('mcmods.audit.task')=='20261024'
def sql(query):
 return subprocess.check_output(docker+['exec','-i',container,'psql','-U','mcmods_audit','-d','mcmods_audit_test','-X','-qAt','-v','ON_ERROR_STOP=1'],input=query,text=True,env=env).strip()
owner,marker=sql(f"select pg_get_userbyid(nspowner),obj_description(oid,'pg_namespace') from pg_namespace where nspname='{schema}'").split('|',1)
assert owner=='mcmods_audit' and re.fullmatch('mcmods-owned-test:'+schema.removeprefix('mcmods_audit_')+'[0-9a-f]{16}',marker)
out.mkdir(mode=0o700,exist_ok=False)
raw=sql(f'''begin transaction isolation level repeatable read read only;
set local search_path={schema},public;
select jsonb_build_object(
 'schema',current_schema(),
 'import',(select jsonb_build_object('jobId',job.id,'jobStatus',job.status,'revisionId',revision.id,'revisionStatus',revision.status,'active',revision.is_active,'sourceSHA256',file.sha256,'creatorPreserved',file.uploader_id=job.created_by and package.uploaded_by=job.created_by,'independentReviewer',audit.actor_id<>job.created_by,'successIntents',(select count(*) from nats_outbox intent where intent.payload->'data'->>'jobId'=job.id and intent.payload->>'templateKey'='mod_import_success')) from catalog_import_jobs job join catalog_import_packages package on package.id=job.package_id join oss_files file on file.id=package.archive_file_id join catalog_import_revisions revision on revision.job_id=job.id join audit_events audit on audit.aggregate_key=revision.id and audit.action='review_approved'),
 'outputs',(select jsonb_agg(jsonb_build_object('recipePublicId',entity.public_id,'kind',candidate.ingredient_kind,'amount',candidate.amount,'rect',slot.rect,'visualRect',slot.visual_rect) order by entity.public_id) from recipe_import_snapshots snapshot join catalog_entities entity on entity.id=snapshot.recipe_id join recipe_import_bindings binding on binding.recipe_snapshot_id=snapshot.id join recipe_import_binding_candidates candidate on candidate.binding_id=binding.id join recipe_template_import_slots slot on slot.id=binding.template_slot_id));
commit;''')
data=json.loads(raw);(out/'database-snapshot.json').write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
i=data['import'];assert i['jobId']==r['jobId'] and i['revisionId']==r['revisionId'] and i['jobStatus']==i['revisionStatus']=='ready' and i['active'] and i['creatorPreserved'] and i['independentReviewer'] and i['sourceSHA256']==r['sampleSHA256'] and i['successIntents']==1
assert len(data['outputs'])==len(r['gr010']['cases'])==3
assert hashlib.sha256((runtime_path.parent/'synthetic-jei-v7-three-kinds.zip').read_bytes()).hexdigest()==r['sampleSHA256']
checks=[]
for expected in r['gr010']['cases']:
 row=next(row for row in data['outputs'] if row['recipePublicId']==expected['recipePublicId'])
 assert row['kind']==expected['kind'] and row['amount']==expected['amount'] and row['rect']==expected['expectedOutputRect'] and row['visualRect']==expected['expectedOutputVisualRect']
 path='/api/v1/catalog/recipes/'+expected['recipePublicId']+'/render'
 with urllib.request.urlopen(r['apiOrigin']+path) as response:body=response.read();value=json.loads(body)['data']
 slots=value['layout']['slots'];assert len(slots)==2
 assert any(slot['role']=='input' and slot['ingredient_present'] is False and slot['alternatives']==[] for slot in slots)
 outputs=[slot for slot in slots if slot['role']=='output'];assert len(outputs)==1
 slot=outputs[0];assert slot['rect']==expected['expectedOutputRect'] and slot['visual_rect']==expected['expectedOutputVisualRect'] and len(slot['alternatives'])==1
 alt=slot['alternatives'][0];assert alt['ingredient_kind']==expected['kind'] and alt['amount']==expected['amount']
 checks.append({'recipePublicId':expected['recipePublicId'],'path':path,'status':200,'kind':alt['ingredient_kind'],'amount':alt['amount'],'rect':slot['rect'],'visualRect':slot['visual_rect'],'bodySHA256':hashlib.sha256(body).hexdigest()})
result={'status':'PASS_REAL_HTTP_PG_SUPPORTED_SYNTHETIC_THREE_KIND_PROJECTIONS','recordedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sourceRevisions':r['repoRevisions'],'driverSHA256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),'databaseSHA256':hashlib.sha256((out/'database-snapshot.json').read_bytes()).hexdigest(),'checks':checks,'sampleSHA256':r['sampleSHA256'],'productionExecuted':False,'scope':'Read-only task-owned PostgreSQL+anonymous actual HTTP. Explicit synthetic v7 data, direct Worker and independent review; native browser/real queue/actual game acceptance are separate.'}
(out/'acceptance.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'status':result['status'],'checks':len(checks),'directory':str(out)}))
