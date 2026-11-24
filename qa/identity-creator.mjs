import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';
import {setTimeout as sleep} from 'node:timers/promises';
const [run]=process.argv.slice(2), task=path.dirname(run), frontend=process.env.FRONTEND_CHECKOUT??path.join(task,'frontend');
const cfg=JSON.parse(await fs.readFile(path.join(run,'runtime.json'))),credentials=JSON.parse(await fs.readFile(cfg.privateAuthPath));
const {chromium}=createRequire(path.join(frontend,'package.json'))('playwright');
const pin=(await fs.readFile(path.join(task,'spki'),'utf8')).trim();
const b=await chromium.launch({args:['--ignore-certificate-errors-spki-list='+pin]});
const context=await b.newContext({locale:'en-US',serviceWorkers:'block'});
const origin=cfg.frontendOrigin;
await context.addCookies([{name:'mcmods-ui-locale',value:'en-US',url:origin}]);
await context.route(u=>/^https?:$/.test(u.protocol)&&u.origin!==origin,r=>r.abort());
const q=await context.newPage();const captures=[];
q.on('response',r=>{let p=new URL(r.url()).pathname;if(p.startsWith('/api/')||p.startsWith(cfg.ossBucketPathPrefix))captures.push([r.request().method(), p.startsWith(cfg.ossBucketPathPrefix)?'private-object':p.replace(/\b[a-z0-9]{9,}\b/g,'[id]'),r.status()])});
q.on('response',r=>{if(r.request().resourceType()==='script')captures.push(['script',new URL(r.url()).pathname,r.status()])});
q.on('requestfailed',r=>{const u=new URL(r.url());if(['http:','https:'].includes(u.protocol))captures.push(['failed '+r.resourceType(),u.origin===origin?u.pathname:'external origin',r.failure()?.errorText])});
q.on('pageerror',e=>captures.push(['page error',e.name,e.message.replace(/https?:\/\/[^ ]+/g,'[url]').slice(0,180)]));
q.setDefaultNavigationTimeout(90000);
q.setDefaultTimeout(40000);
function response(method,p){return q.waitForResponse(r=>r.request().method()===method&&new URL(r.url()).pathname===p,{timeout:45000})}
async function decoded(){await q.waitForFunction(prefix=>[...document.images].some(im=>new URL(im.src||location.href).pathname.startsWith(prefix)&&im.complete&&im.naturalWidth>0),cfg.ossBucketPathPrefix,{timeout:45000});return q.evaluate(prefix=>{const im=[...document.images].find(im=>new URL(im.src||location.href).pathname.startsWith(prefix)&&im.complete&&im.naturalWidth>0);return{src:im.src,width:im.naturalWidth,height:im.naturalHeight}},cfg.ossBucketPathPrefix)}
const journey=[];
let stage='native login';
const stages=[];
function step(value){stage=value;stages.push(value)}
const draftResponses=[];
let latestUploadWrite=0;
q.on('response',r=>{
 const pathname=new URL(r.url()).pathname;
 if(r.request().method()==='POST'&&/^\/api\/v1\/users\/me\/(?:oss\/uploads\/|files\/presign)/.test(pathname))latestUploadWrite=Date.now();
 if(r.request().method()==='POST'&&new URL(r.url()).pathname==='/api/v1/users/me/drafts')draftResponses.push(r)
});
try{
 const firstTarget=cfg.profiles[0].editPath;
 await q.goto(origin+'/login?next='+encodeURIComponent(firstTarget),{waitUntil:'load'});
 const form=q.locator('main form'), account=form.getByLabel('Account',{exact:true}),password=form.locator('input[type=password]');
 // Confirm hydration and identify the login inputs through a real form-state transition.
 await form.getByRole('button',{name:'Email code',exact:true}).click();await form.locator('input[type=password]').waitFor({state:'detached'});
 await form.getByRole('button',{name:'Password login',exact:true}).click();
 await account.fill(credentials.username);await password.fill(credentials.password);
 await form.getByText('Password strength:',{exact:false}).waitFor();
 assert.equal(await account.inputValue(),credentials.username);assert.ok(await form.evaluate(f=>f.checkValidity()));
 await form.locator('button[type=submit]').click();
 await q.waitForURL(origin+firstTarget);
 const me=await q.evaluate(async()=>{const r=await fetch('/api/v1/auth/me',{credentials:'include',cache:'no-store'});return {status:r.status,id:(await r.json()).data?.id}});
 assert.equal(me.status,200);assert.equal(me.id,credentials.publicId);
 let n=0;
 const initial=[];
 for(const prof of cfg.profiles){await q.goto(origin+prof.editPath,{waitUntil:'domcontentloaded'});const im=await decoded();assert.ok(new URL(im.src).search);initial.push(im);}
 const waitUntil=Math.max(...initial.map(({src})=>{const u=new URL(src),date=u.searchParams.get('x-oss-date'),exp=Number(u.searchParams.get('x-oss-expires'));assert.ok(date&&exp===60);const s=date.replace(/^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z$/,'$1-$2-$3T$4:$5:$6Z');return Date.parse(s)+(exp+2)*1000;}));
 await sleep(Math.max(0,waitUntil-Date.now()));
 for(const [i,prof] of cfg.profiles.entries()){const status=await q.evaluate(async url=>(await fetch(url,{cache:'no-store',credentials:'same-origin'})).status,initial[i].src);assert.equal(status,403);await q.goto(origin+prof.editPath,{waitUntil:'domcontentloaded'});const renewed=await decoded();assert.notEqual(renewed.src,initial[i].src);journey.push({action:'initial-and-expired',kind:prof.kind,wallClockExpiredStatus:status,freshSignedBytes:[renewed.width,renewed.height]})}
 for(const [action,prof] of [...cfg.profiles.map(p=>['edit',p]),...cfg.profiles.map(p=>['create',p])]){
  // The standard upload policy permits five writes per rolling minute.
  // Each independent edit exercises presign, completion and draft preview signing.
  if(latestUploadWrite)await sleep(Math.max(0,latestUploadWrite+61000-Date.now()));
  n++;const base=prof.kind==='team'?'teams':'authors';
  await q.goto(origin+(action==='edit'?prof.editPath:'/'+base+'/new'),{waitUntil:'domcontentloaded'});
  const f=q.locator('main form'),name=f.locator('input[required]').first();await name.waitFor();
  if(action==='create')await name.fill('Browser '+prof.kind+' '+n+' '+Date.now());
  if(action==='edit')await decoded();
  const image=await q.evaluate(n=>{const c=document.createElement('canvas');c.width=72;c.height=72;const ctx=c.getContext('2d');ctx.fillStyle=['red','green','orange','navy'][n-1];ctx.fillRect(0,0,72,72);ctx.fillStyle='white';ctx.fillRect(n*6,10,12,14);return c.toDataURL('image/png').split(',')[1]},n);
  const phase=action+' '+prof.kind;
  step(phase+' choose avatar file');
  const chooser=f.getByLabel('Choose image',{exact:true});
  assert.equal(await chooser.count(),1);
  captures.push(['avatar chooser',await chooser.count(),await f.locator('input[type=file]').count()]);
  await chooser.setInputFiles({name:'ordinary-avatar-'+n+'.png',mimeType:'image/png',buffer:Buffer.from(image,'base64')});
  step(phase+' confirm crop and complete upload');
  const crop=q.getByRole('dialog').getByRole('button',{name:'Crop and upload',exact:true});
  await crop.waitFor({state:'visible'});
  const [c]=await Promise.all([response('POST','/api/v1/users/me/oss/uploads/complete'),crop.click()]);
  assert.ok(c.ok());const record=(await c.json()).data;const fileId=record.id||record.publicId;assert.ok(fileId);
  if(action==='edit'){
   step(phase+' autosave stable reference');
   const matches=r=>r.request().method()==='POST'&&new URL(r.url()).pathname==='/api/v1/users/me/drafts'&&r.request().postDataJSON()?.payload?.avatarFileId===fileId;
   const draftResponse=draftResponses.find(matches)||await q.waitForResponse(matches,{timeout:45000});
   assert.ok(draftResponse.ok());const snapshot=draftResponse.request().postDataJSON().payload;assert.equal(new URL(snapshot.avatarUrl).search,'');
   const draftId=(await draftResponse.json()).data.id;assert.ok(draftId);step(phase+' resume draft');
   const [rest,preview]=await Promise.all([response('GET','/api/v1/users/me/drafts/'+draftId),q.waitForResponse(r=>r.request().method()==='POST'&&new URL(r.url()).pathname==='/api/v1/users/me/files/presign'&&r.request().postDataJSON()?.fileId===fileId,{timeout:45000}),q.goto(origin+prof.editPath+'?draft='+encodeURIComponent(draftId),{waitUntil:'domcontentloaded'})]);
   assert.equal(rest.status(),200);const restored=(await rest.json()).data?.payload;assert.equal(restored.avatarFileId,fileId);assert.equal(restored.avatarUrl,snapshot.avatarUrl);
   step(phase+' new draft image preview');
   assert.equal(preview.status(),200);const renewed=(await preview.json()).data.url;assert.ok(new URL(renewed).search);
   await q.waitForFunction(src=>Array.from(document.images).some(i=>i.currentSrc===src&&i.complete&&i.naturalWidth===256&&i.naturalHeight===256),renewed,{timeout:45000});
   journey.push({action:'restored-draft',kind:prof.kind,fileId,presignStatus:preview.status(),unsignedPersistedReference:true,decoded:[256,256]});
  }
  step(phase+' submit pending creator change');
  const [saved]=await Promise.all([response(action==='edit'?'PUT':'POST','/api/v1/creators'+(action==='edit'?'/'+prof.publicId:'')),f.locator('header button[type=submit]').click()]);assert.equal(saved.status(),action==='edit'?200:201);
  const payload=saved.request().postDataJSON(),result=(await saved.json()).data;
  assert.equal(payload.avatarFileId,fileId);assert.ok(!new URL(payload.avatarUrl).search);assert.equal(result.reviewStatus,'pending');
  const creatorId=result.publicId||prof.publicId,revisionId=result.revisionId;assert.ok(creatorId&&revisionId);
  step(phase+' independent approval');
  const label=String(n).padStart(2,'0'),pending=path.join(run,'review-request-'+label+'.tmp');
  await fs.writeFile(pending,JSON.stringify({creatorId,revisionId,fileId}),{flag:'wx',mode:0o600});await fs.rename(pending,path.join(run,'review-request-'+label+'.json'));
  const receipt=path.join(run,'review-result-'+label+'.json');for(let i=0;i<300;i++){try{await fs.access(receipt);break}catch{await sleep(175)}}
  const outcome=JSON.parse(await fs.readFile(receipt));assert.equal(outcome.status,'approved');assert.equal(outcome.publishedRevisionId,revisionId);assert.equal(outcome.temporarySignaturePersisted,false);
  step(phase+' view approved avatar');
  await q.goto(origin+'/'+base+'/'+creatorId,{waitUntil:'domcontentloaded'});const visible=await decoded();assert.deepEqual([visible.width,visible.height],[256,256]);
  journey.push({action,kind:prof.kind,reviewStatus:'approved',publicId:creatorId,fileId,publishedRevisionId:revisionId,transport:[saved.status(),c.status()],dimensions:[visible.width,visible.height],stableReference:!new URL(payload.avatarUrl).search});
 }
 await q.screenshot({path:path.join(run,'reviewed-team.png')});await fs.writeFile(path.join(run,'browser-result.json'),JSON.stringify({browser:b.version(),journey,requests:captures},null,2),{mode:0o600,flag:'wx'});
 await fs.writeFile(path.join(run,'STOP'),'',{mode:0o600,flag:'wx'});console.log('PASS ordinary native author/team snapshots and signature renewal');
}catch(err){
 const error=String(err.message).split('\n')[0].replace(/https?:\/\/[^ ]+/g,'[url]').slice(0,240);
 await fs.writeFile(path.join(run,'stage-diagnostics.json'),JSON.stringify({stage,stages,journey,error},null,2),{mode:0o600});
 await fs.writeFile(path.join(run,'request-diagnostics.json'),JSON.stringify(captures),{mode:0o600});throw err}finally{await b.close()}
