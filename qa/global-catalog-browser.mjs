import assert from 'node:assert/strict';
import {readFile,writeFile} from 'node:fs/promises';
import {createRequire} from 'node:module';
import path from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';

const base=process.env.OUT, front=process.env.FRONTEND_CHECKOUT;
const {chromium}=createRequire(path.join(front,'package.json'))('playwright');
const runtime=JSON.parse(await readFile(path.join(base,'runtime.json'),'utf8'));
assert.equal(runtime.state,'READY');
const origin=new URL(runtime.frontendOrigin);
assert.equal(origin.origin,'http://127.0.0.1:3311');
assert.equal(runtime.authPath,path.join(base,'auth.private.json'));
const auth=JSON.parse(await readFile(runtime.authPath,'utf8'));
const expected=runtime.repoRevisions;
assert.equal(expected['mcmods-cn-backend'].commit,process.env.BPIN);
assert.equal(expected['mcmods-cn-frontend'].commit,process.env.FPIN);
assert.notEqual(runtime.actors.creator,runtime.actors.reviewer);
const report={status:'RUNNING',browser:'Chromium',steps:[],actorsAreIndependent:true,sourceBackend:process.env.BPIN,sourceFrontend:process.env.FPIN};
const persist=()=>writeFile(path.join(base,'ui-operations.json'),JSON.stringify(report,null,2)+'\n');
const browser=await chromium.launch();
const pages={};
const dep=runtime.dependencies;
const processStart=Date.now();
const itemName='Public catalogue fixture input';
let location='opening accounts';
async function tab(role){
  const context=await browser.newContext({locale:'en-US',viewport:{width:1360,height:950}});
  const cookies=[{name:'mcmods-ui-locale',value:'en-US',url:origin.origin}];
  if(role!=='anonymous')cookies.push({name:'mcmods_session',value:auth[role+'Token'],domain:origin.hostname,path:'/api/',httpOnly:true,sameSite:'Lax'});
  await context.addCookies(cookies);
  const page=await context.newPage();page.setDefaultTimeout(20000);
  await page.goto(origin.origin+'/recipe-types');
  if(role!=='anonymous'){
    const me=await apiRead(page,'/api/v1/auth/me');
    assert.equal(me.http,200,'fixture session is active for '+role);
  }
  return page;
}
async function apiRead(page,url){
 return await page.evaluate(async url=>{
   const res=await fetch(url,{cache:'no-store',credentials:'include'}); const result=await res.json();
   return {http:res.status,data:result.data,code:result.code};
 },url);
}
async function populate(page,kind,reason,name,code){
 const loc=page.getByLabel('Localized name',{exact:true});
 await loc.fill(name);
 await page.getByLabel('Change reason',{exact:true}).fill(reason);
 if(['resource','tag','type'].includes(kind)){
  await page.getByRole('textbox',{name:/^Canonical ID/}).fill('audit_catalog:'+code);
  return;
 }
 if(kind==='template'){
  await page.getByLabel('Template key',{exact:true}).fill('ordinary_layout');
  await page.getByRole('button',{name:'Output',exact:true}).first().click();
  await page.getByRole('button',{name:/^Add · Output$/}).click();
  return;
 }
 await page.locator('select').filter({has:page.locator('option[value="'+dep.recipeTemplatePublicId+'"]')}).selectOption(dep.recipeTemplatePublicId);
 await page.getByLabel('Canonical source ID',{exact:true}).fill('audit_catalog:ordinary_recipe');
 await page.locator('label').filter({hasText:'Applicable Minecraft versions'}).locator('button[aria-haspopup=dialog]').click();
 let dialog=page.getByRole('dialog');await dialog.getByRole('textbox').fill('1.20.1');
 await dialog.getByRole('button',{name:/^1\.20\.1\s/}).click();
 await dialog.getByRole('button',{name:'Confirm',exact:true}).click();
 await page.locator('button[title="output"]').click();
 await page.getByRole('button',{name:'Choose resources',exact:true}).click();
 dialog=page.getByRole('dialog');
 await dialog.locator('button[aria-pressed]').filter({hasText:itemName}).click();
 await dialog.getByRole('button',{name:'Choose resources',exact:true}).click();
 await page.getByLabel('Amount',{exact:true}).fill('3');
}
const parentType=dep.recipeTypePublicId;
const cases=[
 ['resource','/admin/global-resources?editor=create','/api/v1/catalog/resources','/api/v1/catalog/resources/','reader','Create'],
 ['tag','/mods-tag?editor=create','/api/v1/tags','/api/v1/tags/','anonymous','Create'],
 ['type','/recipe-types?editor=create','/api/v1/recipe-types','/api/v1/recipe-types/','anonymous','Create'],
 ['template','/recipe-types?editor=template-create&publicId='+parentType,'/api/v1/recipe-types/'+parentType+'/templates','/api/v1/recipe-templates/','anonymous','Save'],
 ['recipe','/recipe-types?editor=recipe-create&publicId='+parentType,'/api/v1/recipe-types/'+parentType+'/recipes','/api/v1/catalog/recipes/','anonymous','Save']
];
try{
 for(const role of ['creator','reviewer','reader','anonymous'])pages[role]=await tab(role);
 let firstWrite=0;
 for(const [kind,editor,post,read,reader,button] of cases){
  if(kind==='template' && Date.now()<firstWrite+61000)await delay(firstWrite+61000-Date.now());
  const page=pages.creator, reviewer=pages.reviewer, publicPage=pages[reader], name='Manual catalogue '+kind, reason='Document the standalone '+kind+' with independent editorial approval.';
  report.steps.push({kind,events:[]});const events=report.steps.at(-1).events;
  location=kind+' form fields';await persist();
  await page.goto(origin.origin+editor);
  await populate(page,kind,reason,name,'reviewed_'+kind);
  location=kind+' submit';await persist();
  const trigger=page.getByRole('button',{name:button,exact:true}).first();
  const [response]=await Promise.all([page.waitForResponse(r=>new URL(r.url()).pathname===post && r.request().method()==='POST'),trigger.click()]);
  if(firstWrite===0)firstWrite=Date.now();
  const reply=await response.json();
  assert.equal(response.status(),202,'manual creation returned '+reply.code);
  const receipt=reply.data;assert.equal(receipt.reviewStatus,'pending');
  for(const field of ['revisionId','changeRequestId','objectPublicId'])assert.match(receipt[field],/^[A-Za-z0-9_-]+$/);
  events.push({action:'editor submitted',regularHttpStatus:response.status(),revisionId:receipt.revisionId,objectId:receipt.objectPublicId,reviewStatus:receipt.reviewStatus});await persist();
  location=kind+' public preapproval';
  const prior=await apiRead(publicPage,read+receipt.objectPublicId);
  assert.equal(prior.http,404,'unpublished subject cannot be read by designated audience');
  events.push({action:'preapproval read',scope:reader,http:prior.http});
  location=kind+' independent queue';
  await reviewer.goto(origin.origin+'/reviews');
  const row=reviewer.locator('main article').filter({hasText:reason});
  await row.waitFor();
  await row.locator('textarea').fill('Independently verified '+kind+' catalogue contents.');
  const decisionPath='/api/v1/content-revisions/'+receipt.revisionId;
  const [reviewResponse]=await Promise.all([reviewer.waitForResponse(r=>new URL(r.url()).pathname===decisionPath && r.request().method()==='PATCH'),row.getByRole('button',{name:'Approve',exact:true}).click()]);
  assert.equal(reviewResponse.status(),200);
  events.push({action:'independent reviewer clicked approve',http:200,revisionId:receipt.revisionId});await persist();
  location=kind+' published read';
  const approved=await apiRead(publicPage,read+receipt.objectPublicId+'?locale=en-US');
  assert.equal(approved.http,200);
  assert.equal(approved.data.publicId,receipt.objectPublicId);
  assert(JSON.stringify(approved.data).includes(name),'published localization matches original author input');
  events.push({action:'published contents visible',http:200,scope:reader,localizedName:name});await persist();
  if(kind==='recipe'){
    await publicPage.goto(origin.origin+'/recipe-types?recipePublicId='+receipt.objectPublicId);
    const card=publicPage.locator('#recipe-'+receipt.objectPublicId);await card.waitFor();
    await card.getByText('3',{exact:true}).waitFor();
    await publicPage.screenshot({path:path.join(base,'published-manual-recipe.png')});
    events.push({action:'anonymous recipe rendered',outputAmount:3});
  }
 }
 const stopAt=Date.now()+5000;
 do{
  const fresh=JSON.parse(await readFile(path.join(base,'runtime.json'),'utf8'));
  report.databasePublicationCount=report.steps.filter(s=>fresh.entities.some(e=>e.publicId===s.events[0].objectId && e.publishedRevisionId===s.events[0].revisionId)).length;
  if(report.databasePublicationCount===5)break;
  await delay(350);
 }while(Date.now()<stopAt);
 assert.equal(report.databasePublicationCount,5);
 report.status='PASS_FIVE_BROWSER_CREATIONS_INDEPENDENT_REVIEWS_AND_PUBLISHED_READS';
} catch(error){
 report.status='FAIL';report.failedOperation=location;
 report.error=String(error.message).replace(/https?:\/\/\S+/g,'[task endpoint]').slice(0,700);
 console.error(location,report.error);process.exitCode=1;
} finally {
 report.durationMs=Date.now()-processStart;await persist();await browser.close();
}
