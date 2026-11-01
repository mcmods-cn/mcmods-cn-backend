#!/usr/bin/env python3
"""Read only an owned live recipe fixture; preserve exact API/DB evidence."""
import datetime,hashlib,json,os,pathlib,re,subprocess,sys,urllib.request
runtime_path=pathlib.Path(sys.argv[1]).resolve();out=pathlib.Path(sys.argv[2]).resolve()
r=json.loads(runtime_path.read_text());schema=r['ownedSchema'];assert re.fullmatch(r'mcmods_audit_[0-9a-f]{16}',schema)
assert r['owner']=='mcmods-live-recipe-catalog' and r.get('state') not in ('EXPIRED','STOPPED')
assert (runtime_path.parent/'OWNER').read_text().strip()==f"mcmods-live-recipe-catalog pid={r['pid']}"
process=pathlib.Path(f"/proc/{r['pid']}");assert process.stat().st_uid==os.getuid()
assert b'TestAuditLiveRecipeCatalogFixture' in (process/'cmdline').read_bytes()
env=dict(os.environ)
for key in ['DOCKER_HOST','DOCKER_CONTEXT','DOCKER_TLS','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH']:env.pop(key,None)
docker=['docker','--host=unix:///var/run/docker.sock'];container='mcmods-audit-20261024-postgres18'
labels=json.loads(subprocess.check_output(docker+['inspect','--format','{{json .Config.Labels}}',container],text=True,env=env));assert labels.get('mcmods.audit.task')=='20261024'
def sql(query):
 return subprocess.check_output(docker+['exec','-i',container,'psql','-U','mcmods_audit','-d','mcmods_audit_test','-X','-qAt','-v','ON_ERROR_STOP=1'],input=query,text=True,env=env).strip()
owner,marker=sql(f"select pg_get_userbyid(nspowner),obj_description(oid,'pg_namespace') from pg_namespace where nspname='{schema}'").split('|',1)
assert owner=='mcmods_audit' and re.fullmatch('mcmods-owned-test:'+schema.removeprefix('mcmods_audit_')+'[0-9a-f]{16}',marker)
assert out.is_absolute();out.mkdir(mode=0o700,exist_ok=False)
query=f'''begin transaction isolation level repeatable read read only;
set local search_path={schema},public;
select jsonb_build_object(
 'schema',current_schema(),
 'entities',(select jsonb_agg(jsonb_build_object('publicId',entity.public_id,'kind',entity.entity_type,'status',entity.status,'revisionPublicId',revision.public_id) order by entity.public_id) from catalog_entities entity left join content_revisions revision on revision.id=entity.published_revision_id),
 'decisions',(select jsonb_agg(jsonb_build_object('subjectPublicId',entity.public_id,'revisionPublicId',revision.public_id,'requestPublicId',request.public_id,'status',request.status,'eventType',event.event_type,'independent',event.actor_id<>request.submitted_by) order by request.id,event.id) from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id join catalog_entities entity on entity.id=revision.entity_id join review_events event on event.change_request_id=request.id where revision.aggregate_type in ('catalog_editor_resource','catalog_editor_recipe_type','catalog_editor_recipe_template','catalog_editor_recipe') and event.event_type='approved'),
 'candidates',(select jsonb_agg(jsonb_build_object('recipePublicId',entity.public_id,'amount',candidate.amount,'probability',candidate.probability) order by entity.public_id) from recipe_binding_candidates candidate join recipe_bindings binding on binding.id=candidate.binding_id join catalog_entities entity on entity.id=binding.recipe_id),
 'groups',(select jsonb_agg(jsonb_build_object('typePublicId',entity.public_id,'canonicalId',kind.canonical_id,'manualCount',(select count(*) from recipes recipe join recipe_definitions definition on definition.recipe_id=recipe.entity_id where recipe.recipe_type_id=kind.entity_id),'importedCount',(select count(*) from recipes recipe join recipe_import_snapshots snapshot on snapshot.recipe_id=recipe.entity_id join catalog_import_revisions revision on revision.id=snapshot.revision_id where recipe.recipe_type_id=kind.entity_id and revision.is_active and revision.status in ('ready','partial'))) order by entity.public_id) from recipe_types kind join catalog_entities entity on entity.id=kind.entity_id),
 'imports',(select jsonb_agg(jsonb_build_object('jobId',job.id,'revisionId',revision.id,'jobStatus',job.status,'revisionStatus',revision.status,'isActive',revision.is_active,'sourceSHA256',file.sha256,'sourceOwnerPreserved',file.uploader_id=job.created_by and package.uploaded_by=job.created_by,'recipeCount',(select count(*) from recipe_import_snapshots snapshot where snapshot.revision_id=revision.id)) order by revision.id) from catalog_import_jobs job join catalog_import_packages package on package.id=job.package_id join oss_files file on file.id=package.archive_file_id join catalog_import_revisions revision on revision.job_id=job.id),
 'outbox',(select jsonb_object_agg(status,n) from (select status,count(*) n from nats_outbox group by status) counted));
commit;'''
raw=sql(query);data=json.loads(raw)
(out/'database-snapshot.json').write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
checks=[]
for name,manual,imported in [('manualOnly',1,0),('importedOnly',0,2),('mixed',1,1)]:
 case=r['cont002'][name];kind=next(x for x in data['groups'] if x['typePublicId']==case['typePublicId']);assert kind['manualCount']==manual and kind['importedCount']==imported
 page_ids=[]
 for offset in range(manual+imported+1):
  path=f"/api/v1/recipe-types/{case['typePublicId']}/catalog?limit=1&offset={offset}"
  with urllib.request.urlopen(r['apiOrigin']+path) as response:body=response.read();payload=json.loads(body)['data']
  assert payload['total']==manual+imported and len(payload['recipes'])==(1 if offset<manual+imported else 0)
  ids=[x.get('publicId',x.get('id')) for x in payload['recipes']];page_ids+=ids
  checks.append({'name':name,'path':path,'status':200,'bodySHA256':hashlib.sha256(body).hexdigest(),'total':payload['total'],'recipePublicIds':ids})
 assert len(page_ids)==len(set(page_ids))==manual+imported
 recipes=[case['recipePublicId']] if name!='mixed' else [case['manualRecipePublicId'],case['importedRecipePublicId']]
 assert set(recipes)<=set(page_ids)
for recipe in [r['cont002']['manualOnly']['recipePublicId'],r['cont002']['mixed']['manualRecipePublicId']]:
 candidate=next(c for c in data['candidates'] if c['recipePublicId']==recipe);assert candidate['amount']==3 and candidate['probability']==.5
 path=f'/api/v1/catalog/recipes/{recipe}/render'
 with urllib.request.urlopen(r['apiOrigin']+path) as response:body=response.read();layout=json.loads(body)['data']['layout']
 assert any(x['role']=='input' and x['ingredient_present'] is False and x['alternatives']==[] for x in layout['slots'])
 assert any(a['amount']==3 and a['probability']==.5 for x in layout['slots'] for a in x['alternatives'])
 checks.append({'name':'manual-real-render','path':path,'status':200,'bodySHA256':hashlib.sha256(body).hexdigest(),'emptyInputPresent':True,'amount':3,'probability':.5})
assert len(data['imports'])==2 and all(i['jobStatus']=='ready' and i['revisionStatus']=='ready' and i['isActive'] and i['sourceOwnerPreserved'] for i in data['imports'])
assert {i['sourceSHA256'] for i in data['imports']}=={r['sampleSHA256'],r['syntheticContractSHA256']}
assert len(data['decisions'])==6 and all(i['status']=='approved' and i['eventType']=='approved' and i['independent'] for i in data['decisions'])
result={'status':'PASS_REAL_HTTP_PG_THREE_SOURCE_GROUPS_PAGING_QUANTITIES_INDEPENDENT_DECISIONS','recordedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sourceRevisions':r['repoRevisions'],'scope':'Read-only task-owned HTTP/PostgreSQL snapshot. No browser/real queue or production inference.','databaseSHA256':hashlib.sha256((out/'database-snapshot.json').read_bytes()).hexdigest(),'checks':checks,'manualAuthoringCount':6,'imports':data['imports'],'productionExecuted':False}
(out/'acceptance.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'status':result['status'],'directory':str(out),'checks':len(checks)}))
