import {createHash} from 'node:crypto';
export async function run({runtime,folder,front,auth,openActor,expectWrite,save,sleep,evidence,assert,join,readFileSync,writeFileSync,existsSync}){
 for(const row of runtime.localeCases){
   const {page,close}=await openActor(auth.localizations[row.caseId]);
   try{
     evidence.currentCase=row.caseId;save();
     const field=page.getByRole('combobox',{name:'Default content language'});
     for(let nav=0;nav<2;nav++){
       try{
         await page.goto(front.origin+row.newURL,{waitUntil:'commit'});
         await field.waitFor({state:'visible'});
         break;
       } catch(error) {
         if(nav===1)throw error;
         evidence.readRetries??=[];evidence.readRetries.push({case:row.caseId,stage:'opening empty editor before any write'});save();
       }
     }
     await field.selectOption(row.defaultLocale);
     const v=row.formValues;
     await page.getByRole('textbox',{name:/^Mod site ID/}).fill(row.siteId);
     await page.getByRole('textbox',{name:/^Primary name/}).fill(v.primaryName);
     await page.getByRole('textbox',{name:'Mod ID',exact:true}).fill(v.modIds[0].identifier);
     const current=v.localizations?.find(x=>x.locale===row.defaultLocale);
     if(current?.name)await page.getByRole('textbox',{name:/name in the default content language/}).fill(current.name);
     if(current?.summary)await page.getByRole('textbox',{name:/^(Short summary|Summary)/}).fill(current.summary);
     const secondary=v.localizations?.find(x=>x.locale===row.otherLocale);
     if(secondary?.contentMarkdown){
       const label=await field.locator('option[value="'+row.otherLocale+'"]').textContent();
       await page.getByRole('tab',{name:label,exact:true}).click();
       await page.locator('textarea[aria-label]').filter({visible:true}).last().fill(secondary.contentMarkdown);
     }
     if(row.mode==='summary-only'){
       const localized=page.getByRole('textbox',{name:/name in the default content language/});
       await localized.fill('');
       const before=evidence.requests.filter(r=>r.method==='POST' && r.path==='/api/v1/mods').length;
       await page.locator('main form button[type=submit]').first().click();
       await page.getByText('Enter a name in the default content language before submitting localized content.',{exact:true}).waitFor();
       assert.equal(evidence.requests.filter(r=>r.method==='POST' && r.path==='/api/v1/mods').length,before);
       await localized.fill(current.name);
     }
     let response;
     for(;;){
       response=await expectWrite(page,/^\/api\/v1\/mods$/,'POST',()=>page.locator('main form button[type=submit]').first().click());
       if(response.status()!==429)break;
       const secs=Number(response.headers()['retry-after']);assert(Number.isFinite(secs)&&secs>0);
       await sleep(secs*1000);
     }
     assert.equal(response.status(),201, 'Ordinary form '+row.caseId+' status '+response.status());
     const req=response.request();
     const hash=createHash('sha256').update(req.postDataBuffer()).digest('hex');
     writeFileSync(join(folder,'review-request.'+row.caseId+'.json'),JSON.stringify({caseId:row.caseId,siteId:row.siteId,bodySHA256:hash}));
     const receipt=join(folder,'review-result.'+row.caseId+'.json');
     for(let i=0;i<7200&&!existsSync(receipt);i++)await sleep(500);
     assert(existsSync(receipt),'missing independent reviewer receipt');
     const record=JSON.parse(readFileSync(receipt,'utf8'));
     assert.equal(record.status,'APPROVED_ACTUAL_INDEPENDENT_HTTP');assert.equal(record.bodySHA256,hash);
     evidence.steps.push({case:row.caseId,locale:row.defaultLocale,bodySHA256:hash,defaultLanguageChecked:row.mode==='summary-only',status:response.status(),independentReview:record.status});save();
   } finally {await close();}
 }
 assert.equal(evidence.steps.length,32);
}
