import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
const [run,kind]=process.argv.slice(2);
const task=path.dirname(run), front=process.env.FRONTEND_CHECKOUT??path.join(task,'frontend');
const env=JSON.parse(await fs.readFile(path.join(run,'runtime.json')));
const privateAuth=JSON.parse(await fs.readFile(env.authPath));
const {chromium}=createRequire(path.join(front,'package.json'))('playwright');
const browser=await chromium.launch({args:['--use-angle=swiftshader','--enable-unsafe-swiftshader']});
const origin=env.frontendOrigin,api=env.apiOrigin;
const evidence={actualLoopbackHTTP:true,chromium:browser.version(),frontend:'7fd79235539a048c65dc5fda42929fd166d61385',backend:env.sourceCommit};
async function user(token){
 const context=await browser.newContext({locale:'en-US',viewport:{width:1250,height:820},serviceWorkers:'block'});
 await context.route(u=> /^https?:$/.test(u.protocol)&&![origin,api].includes(u.origin), r=>r.abort());
 await context.addCookies([{name:'mcmods-ui-locale',value:'en-US',url:origin},...(token?[{name:env.authCookieName,value:token,domain:new URL(api).hostname,path:'/api/',httpOnly:true,sameSite:'Lax'}]:[])]);
 const page=await context.newPage();
 return {context,page};
}
try{
 if(kind==='skin'){
  const {context,page}=await user(privateAuth.ownerToken);
  const imageUrl=new URL(env.textureUrl,api).href, skinPage=origin+'/skins/'+env.skinPublicId;
  await page.addInitScript(({p})=>{const modes=[];Object.defineProperty(window,'mcmodsTextureModes',{value:modes});const d=Object.getOwnPropertyDescriptor(HTMLImageElement.prototype,'src'); Object.defineProperty(HTMLImageElement.prototype,'src',{...d,set(v){if(new URL(v,location.href).pathname===p)modes.push(this.crossOrigin);d.set.call(this,v)}})}, {p:new URL(imageUrl).pathname});
  const pending=page.waitForResponse(q=>q.url()===imageUrl && q.status()===200,{timeout:60000});
  await page.goto(skinPage,{waitUntil:'domcontentloaded',timeout:90000});
  await page.getByRole('heading',{name:env.skinName,exact:true}).waitFor({timeout:50000});
  const png=await pending, raw=await png.body(), digest=createHash('sha256').update(raw).digest('hex');assert.equal(digest,env.textureHash);
  assert.deepEqual([raw.readUInt32BE(16),raw.readUInt32BE(20)],[64,64]);const req=await png.request().allHeaders();assert.ok(req.cookie.includes(env.authCookieName+'='));const headers=await png.allHeaders();assert.equal(headers['access-control-allow-origin'],origin);assert.equal(headers['access-control-allow-credentials'],'true');
  await page.waitForFunction(()=>!!document.querySelector('canvas[data-engine]'),null,{timeout:60000});
  await page.getByRole('button',{name:'2D preview',exact:true}).click();
  await page.waitForFunction(n=>{const c=document.querySelector('canvas[aria-label="'+n+'"]');return c&&!c.classList.contains('invisible')&&c.getContext('2d').getImageData(0,0,c.width,c.height).data.some((v,i)=>i%4===3&&v>0)},env.skinName);
  const modes=await page.evaluate(()=>window.mcmodsTextureModes);assert.ok(modes.length>=2&&modes.every(v=>v==='use-credentials'));
  await page.screenshot({path:path.join(run,'owner-render.png')});await context.close();
  const denied=[];
  for(const token of [undefined,privateAuth.otherToken]){const{context:c,page:q}=await user(token);const r=await q.goto(imageUrl);assert.equal(r.status(),404);denied.push(r.status());await c.close()}
  Object.assign(evidence,{ownerTextureStatus:200,anonymousAndOther:denied,decodedPNG:[64,64],pngSHA256:digest,imageModes:modes,renderer:'software WebGL and 2D canvas',privateCacheControl:headers['cache-control']});
 }else if(kind==='fid'){
  const{page,context}=await user(privateAuth.senderToken);await page.goto(origin+'/user?section=economy',{waitUntil:'domcontentloaded',timeout:90000});
  const mode=page.getByLabel('Recipient identifier',{exact:true}),who=page.getByLabel('Recipient',{exact:true}),amount=page.getByLabel('Amount',{exact:true}),currency=page.getByLabel('Currency',{exact:true}),submit=page.getByRole('button',{name:'Confirm transfer',exact:true});
  await mode.waitFor({timeout:50000});assert.equal(await mode.inputValue(),'identifier');await who.fill(env.recipientPublicId);await amount.fill(String(env.amount));await currency.selectOption(env.currency);
  const writes=[];page.on('request',r=>{if(r.method()==='POST'&&new URL(r.url()).pathname==='/api/v1/users/me/economy/transfer')writes.push(r.postDataJSON())});
  async function execute(status){const p=page.waitForResponse(r=>r.request().method()==='POST'&&new URL(r.url()).pathname==='/api/v1/users/me/economy/transfer');await submit.click();const r=await p;assert.equal(r.status(),status);return r.json()}
  const first=await execute(409);assert.equal(first.code,'TRANSFER_RECIPIENT_AMBIGUOUS');await page.getByText('More than one account matches.',{exact:false}).waitFor();assert.equal(await who.inputValue(),env.recipientPublicId);
  await page.getByRole('alertdialog').getByRole('button',{name:'Close',exact:true}).click();await mode.selectOption('public_id');assert.equal(writes.length,1);
  const debit=await execute(200);assert.equal(debit.data.received,env.amount);await page.getByText('Transfer complete. The recipient received 100; tax: 0.',{exact:true}).waitFor();assert.deepEqual(writes,[{recipient:env.recipientPublicId,currency:env.currency,amount:env.amount},{recipientPublicId:env.recipientPublicId,currency:env.currency,amount:env.amount}]);await page.screenshot({path:path.join(run,'explicit-transfer.png')});await context.close();Object.assign(evidence,{writes,statuses:[409,200],received:debit.data.received,tax:debit.data.tax});
 }else throw Error('unknown');
 await fs.writeFile(path.join(run,'browser-result.json'),JSON.stringify(evidence,null,2)+'\n',{flag:'wx',mode:0o600});
 await fs.writeFile(path.join(run,'done.flag'),'',{flag:'wx',mode:0o600});console.log('PASS',kind);
}finally{await browser.close()}
