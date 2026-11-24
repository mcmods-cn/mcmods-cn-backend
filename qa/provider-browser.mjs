import http from 'node:http';
import { readFile, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
import { resolve, join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
const front = resolve(process.env.FRONTEND_CHECKOUT);
const {chromium} = await import(pathToFileURL(join(front,'node_modules/playwright/index.mjs')).href);
const out = process.env.OUT;
const runtimeFile = join(out,'runtime.json');
const runtime = JSON.parse(await readFile(runtimeFile,'utf8'));
const creds = JSON.parse(await readFile(runtime.authPath,'utf8'));
const site = new URL(runtime.frontendOrigin);
const upstream = new URL(runtime.apiOrigin);
assert.equal(site.hostname,'127.0.0.1');
assert.equal(upstream.hostname,'127.0.0.1');
const app = new URL('http://127.0.0.1:3991');
const gateway = http.createServer((req, res) => {
  const target = req.url.startsWith('/api/') ? upstream : app;
  const proxied = http.request({hostname:target.hostname, port:target.port, path:req.url,
    method:req.method,headers:{...req.headers,host:target.host}}, reply => {
    res.writeHead(reply.statusCode,reply.headers);
    reply.pipe(res);
  });
  proxied.on('error',()=>{ if(!res.headersSent) res.writeHead(502); res.end(); });
  req.pipe(proxied);
});
await new Promise((done,reject)=>{gateway.on('error',reject);gateway.listen(Number(site.port),site.hostname,done);});
const names={ plugin:'plugins',map:'maps',resource_pack:'resource-packs',shader_pack:'shaders',datapack:'datapacks',addon:'addons' };
const httpFacts=[];
const summary={fixture:process.env.CASE, frontend:process.env.FPIN,backend:process.env.BPIN,accepted:[],browserScreens:[],httpFacts};
const persist=()=>writeFile(join(out,'acceptance.json'),JSON.stringify(summary,null,2)+'\n');
async function call(url, token, method='GET', body=undefined, allowed=[200]){
  const response=await fetch(upstream.origin+url,{method,headers:{...(token?{Authorization:'Bearer '+token}:{}),...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined,redirect:'error'});
  let obj={};try{obj=await response.json();}catch{}
  httpFacts.push({path:url,method,status:response.status,code:obj.code||null});
  assert(allowed.includes(response.status),'Unexpected '+method+' '+url+' HTTP '+response.status+' code '+(obj.code||obj.error||''));
  return obj.data??obj;
}
const browser=await chromium.launch();
const reader=await browser.newContext();
await reader.addCookies([{name:'mcmods-ui-locale',value:'en-US',url:site.origin}]);
try {
  let i=0;
  for(const sample of runtime.supportedCombinations){
    const {projectType:kind,provider,sampleId,sourceURL}=sample;
    const token=creds[sample.actor]??creds.creatorToken;
    const reviewer=creds.reviewer??creds.reviewerToken;
    assert(token && reviewer);
    const prefix='/api/v1/content-project-imports/'+kind;
    const route='/'+names[kind]+'/new';
    const ctx=await browser.newContext();
    await ctx.addCookies([{name:'mcmods-ui-locale',value:'en-US',url:site.origin},{name:'mcmods_session',value:token,url:site.origin,httpOnly:true,sameSite:'Lax'}]);
    const page=await ctx.newPage(); page.setDefaultTimeout(25000);
    try {
      let imported, jobId;
      if(sample.adapterSamplePath){
        const original=await readFile(sample.adapterSamplePath);
        assert.equal(createHash('sha256').update(original).digest('hex'),sample.adapterSampleSha256);
        imported=JSON.parse(original);
      } else {
        const response=page.waitForResponse(r=>new URL(r.url()).pathname===prefix && r.request().method()==='POST',{timeout:90000});
        await page.goto(site.origin+route+'?method='+provider+'&url='+encodeURIComponent(sourceURL),{waitUntil:'domcontentloaded',timeout:90000});
        const accepted=await response;
        const queued=await accepted.json();
        assert.equal(accepted.status(),202,queued.code??queued.error);
        jobId=queued.data.id;
        const deadline=Date.now()+60000;
        for(;;){
          const job=await call(prefix+'/'+jobId,token);
          if(job.status==='completed'){imported=job.result;break;}
          assert(!['cancelled','failed'].includes(job.status) && Date.now()<deadline,'task '+job.status);
          await delay(400);
        }
      }
      assert.equal(imported.projectType,kind);
      if(kind==='addon' && !(imported.parentProjects??[]).length) imported.parentProjects=[{type:'mod',publicId:runtime.parentModPublicId}];
      // Resume the actual provider result using the user's supported draft API.
      const key='simple-project:'+kind+':new';
      const draft=await call('/api/v1/users/me/drafts',token,'POST',{draftKey:key,projectKey:key,kind:'simple_project',title:'Provider fixture '+sampleId,editUrl:route,payload:{draft:imported,keywordsInput:(imported.searchKeywords??[]).join(', '),changeReason:''}},[200,201]);
      assert(draft.id);
      const restored=page.waitForResponse(r=>new URL(r.url()).pathname.endsWith('/drafts/'+draft.id),{timeout:90000});
      await page.goto(site.origin+route+'?draft='+encodeURIComponent(draft.id),{waitUntil:'domcontentloaded',timeout:90000});
      assert.equal((await restored).status(),200);
      const input=page.locator('input.field.font-mono[required]').first();
      await page.waitForFunction(slug=>document.querySelector('input.field.font-mono[required]')?.value===slug,imported.siteId,{timeout:25000});
      await input.fill('review-'+sampleId.replace(/^tp008-/,'').slice(0,49));
      if(i===0)await page.screenshot({path:join(out,'restored-editor.png')});
      const saved=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1/content-projects/'+kind&&r.request().method()==='POST');
      await page.getByRole('button',{name:'Save',exact:true}).click();
      const created=await saved;
      const result=await created.json();
      assert.equal(created.status(),201,result.code??result.error);
      const project=result.data;
      assert(project.siteId && project.submissionRevisionId);
      await call('/api/v1/content-revisions/'+project.submissionRevisionId,reviewer,'PATCH',{status:'approved',note:'Independently approved provider fixture'});
      const pub=await call('/api/v1/content-projects/'+kind+'/'+project.siteId,null);
      assert.equal(pub.siteId,project.siteId);
      const anon=await reader.newPage();
      await anon.goto(site.origin+'/'+names[kind]+'/'+project.siteId,{waitUntil:'domcontentloaded',timeout:90000});
      await anon.getByRole('heading',{name:imported.localizations[0].name,exact:true}).first().waitFor({timeout:35000});
      await anon.close();
      summary.accepted.push({kind,provider,sampleId,source:sample.adapterSamplePath?'controlled provider and draft':'HTTP job and JetStream Worker',jobId:jobId??null,siteId:project.siteId,approvedRevision:project.submissionRevisionId});
      console.log('Accepted', sampleId);
      await persist();
    } finally {await ctx.close();}
    i++;
    if(i<runtime.supportedCombinations.length)await delay(61500);
  }
  assert.equal(summary.accepted.length,11);
  summary.status='PASS';
} catch(e){summary.status='FAIL';summary.reason=String(e.message).slice(0,550);throw e;}
finally{await persist();await browser.close();gateway.close();}
