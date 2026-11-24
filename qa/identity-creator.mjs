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
q.setDefaultNavigationTimeout(90000);
q.setDefaultTimeout(40000);
function response(method,p){return q.waitForResponse(r=>r.request().method()===method&&new URL(r.url()).pathname===p,{timeout:45000})}
async function decoded(){await q.waitForFunction(prefix=>[...document.images].some(im=>new URL(im.src||location.href).pathname.startsWith(prefix)&&im.complete&&im.naturalWidth>0),cfg.ossBucketPathPrefix,{timeout:45000});return q.evaluate(prefix=>{const im=[...document.images].find(im=>new URL(im.src||location.href).pathname.startsWith(prefix)&&im.complete&&im.naturalWidth>0);return{src:im.src,width:im.naturalWidth,height:im.naturalHeight}},cfg.ossBucketPathPrefix)}
const journey=[];
try{
 await q.goto(origin+'/login',{waitUntil:'domcontentloaded'});await q.locator('form input:not([type=password])').first().fill(credentials.username);
 await q.locator('input[type=password]').fill(credentials.password);
 const login=response('POST','/api/v1/auth/login');await q.locator('form button[type=submit]').click();assert.equal((await login).status(),200);
 let n=0;
 const initial=[];
 for(const prof of cfg.profiles){await q.goto(origin+prof.editPath,{waitUntil:'domcontentloaded'});const im=await decoded();assert.ok(new URL(im.src).search);initial.push(im);}
 const waitUntil=Math.max(...initial.map(({src})=>{const u=new URL(src),date=u.searchParams.get('x-oss-date'),exp=Number(u.searchParams.get('x-oss-expires'));assert.ok(date&&exp===60);const s=date.replace(/^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z$/,'$1-$2-$3T$4:$5:$6Z');return Date.parse(s)+(exp+2)*1000;}));
 await sleep(Math.max(0,waitUntil-Date.now()));
 for(const [i,prof] of cfg.profiles.entries()){const status=await q.evaluate(async url=>(await fetch(url,{cache:'no-store',credentials:'same-origin'})).status,initial[i].src);assert.equal(status,403);await q.goto(origin+prof.editPath,{waitUntil:'domcontentloaded'});const renewed=await decoded();assert.notEqual(renewed.src,initial[i].src);journey.push({action:'initial-and-expired',kind:prof.kind,wallClockExpiredStatus:status,freshSignedBytes:[renewed.width,renewed.height]})}
 for(const [action,prof] of [...cfg.profiles.map(p=>['edit',p]),...cfg.profiles.map(p=>['create',p])]){
  n++;const base=prof.kind==='team'?'teams':'authors';
  await q.goto(origin+(action==='edit'?prof.editPath:'/'+base+'/new'),{waitUntil:'domcontentloaded'});
  const f=q.locator('main form'),name=f.locator('input[required]').first();await name.waitFor();
  if(action==='create')await name.fill('Browser '+prof.kind+' '+n+' '+Date.now());
  if(action==='edit')await decoded();
  const image=await q.evaluate(n=>{const c=document.createElement('canvas');c.width=72;c.height=72;const ctx=c.getContext('2d');ctx.fillStyle=['red','green','orange','navy'][n-1];ctx.fillRect(0,0,72,72);ctx.fillStyle='white';ctx.fillRect(n*6,10,12,14);return c.toDataURL('image/png').split(',')[1]},n);
  const complete=response('POST','/api/v1/users/me/oss/uploads/complete');
  await f.locator('input[type=file]').setInputFiles({name:'ordinary-avatar-'+n+'.png',mimeType:'image/png',buffer:Buffer.from(image,'base64')});
  await q.getByRole('button',{name:'Crop and upload',exact:true}).click();
  const c=await complete;assert.ok(c.ok());const record=(await c.json()).data;const fileId=record.id||record.publicId;assert.ok(fileId);
  if(action==='edit'){
   const draftResponse=await q.waitForResponse(r=>{if(r.request().method()!=='POST'||new URL(r.url()).pathname!=='/api/v1/users/me/drafts')return false;return r.request().postDataJSON()?.payload?.avatarFileId===fileId},{timeout:45000});
   assert.ok(draftResponse.ok());const snapshot=draftResponse.request().postDataJSON().payload;assert.equal(new URL(snapshot.avatarUrl).search,'');
   const draftId=(await draftResponse.json()).data.id;assert.ok(draftId);const reloading=response('GET','/api/v1/users/me/drafts/'+draftId);
   await q.goto(origin+prof.editPath+'?draft='+encodeURIComponent(draftId),{waitUntil:'domcontentloaded'});const rest=await reloading;
   assert.equal(rest.status(),200);const restored=(await rest.json()).data?.payload;assert.equal(restored.avatarFileId,fileId);assert.equal(restored.avatarUrl,snapshot.avatarUrl);
   const retry=q.getByRole('button',{name:'Retry',exact:true});
   await retry.waitFor({state:'visible'});await retry.click();await decoded();
  }
  const mutation=response(action==='edit'?'PUT':'POST','/api/v1/creators'+(action==='edit'?'/'+prof.publicId:''));
  await f.locator('button[type=submit]').click();const saved=await mutation;assert.equal(saved.status(),action==='edit'?200:201);
  const payload=saved.request().postDataJSON(),result=(await saved.json()).data;
  assert.equal(payload.avatarFileId,fileId);assert.ok(!new URL(payload.avatarUrl).search);assert.equal(result.reviewStatus,'pending');
  const creatorId=result.publicId||prof.publicId,revisionId=result.revisionId;assert.ok(creatorId&&revisionId);
  const label=String(n).padStart(2,'0'),pending=path.join(run,'review-request-'+label+'.tmp');
  await fs.writeFile(pending,JSON.stringify({creatorId,revisionId,fileId}),{flag:'wx',mode:0o600});await fs.rename(pending,path.join(run,'review-request-'+label+'.json'));
  const receipt=path.join(run,'review-result-'+label+'.json');for(let i=0;i<300;i++){try{await fs.access(receipt);break}catch{await sleep(175)}}
  const outcome=JSON.parse(await fs.readFile(receipt));assert.equal(outcome.status,'approved');assert.equal(outcome.publishedRevisionId,revisionId);assert.equal(outcome.temporarySignaturePersisted,false);
  await q.goto(origin+'/'+base+'/'+creatorId,{waitUntil:'domcontentloaded'});const visible=await decoded();
  journey.push({action,kind:prof.kind,reviewStatus:'approved',publicId:creatorId,fileId,publishedRevisionId:revisionId,transport:[saved.status(),c.status()],dimensions:[visible.width,visible.height],stableReference:!new URL(payload.avatarUrl).search});
 }
 await q.screenshot({path:path.join(run,'reviewed-team.png')});await fs.writeFile(path.join(run,'browser-result.json'),JSON.stringify({browser:b.version(),journey,requests:captures},null,2),{mode:0o600,flag:'wx'});
 await fs.writeFile(path.join(run,'STOP'),'',{mode:0o600,flag:'wx'});console.log('PASS ordinary native author/team snapshots and signature renewal');
}catch(err){await fs.writeFile(path.join(run,'request-diagnostics.json'),JSON.stringify(captures),{mode:0o600});throw err}finally{await b.close()}
