import { request } from 'node:http';
import { createServer } from 'node:https';
import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
const state=process.argv[2], work=path.dirname(state), r=JSON.parse(await readFile(path.join(state,'runtime.json')));
assert.equal(r.frontendOrigin,'https://127.0.0.1:3431');
for(const a of [r.apiOrigin,r.internalOSSOrigin])assert.equal(new URL(a).hostname,'127.0.0.1');
const observedObjectPaths=new Set();
const server=createServer({cert:await readFile(path.join(work,'leaf.pem')),key:await readFile(path.join(work,'leaf.key'))},(req,res)=>{
 const pathname=new URL(req.url,r.frontendOrigin).pathname;
 const bucketPath=pathname.startsWith(r.ossBucketPathPrefix);
 if(bucketPath)observedObjectPaths.add('/'+pathname.slice(r.ossBucketPathPrefix.length));
 // The public endpoint also addresses stable keys without the path-style bucket prefix.
 const origin=(bucketPath||observedObjectPaths.has(pathname))?r.internalOSSOrigin:req.url.startsWith('/api/')?r.apiOrigin:'http://127.0.0.1:3465';
 // Preserve the exact query, HTTP method, Host, signed headers and HEAD length.
 const headers={...req.headers};delete headers.connection;
 const upstream=request(new URL(origin),{path:req.url,headers,method:req.method},remote=>{
   const h={...remote.headers};delete h.connection;
   res.writeHead(remote.statusCode,h);remote.pipe(res);
   if(origin===r.apiOrigin && ['/api/v1/auth/login','/api/v1/auth/me'].includes(pathname))console.log('auth request',req.method,pathname,remote.statusCode);
   if(origin===r.internalOSSOrigin)console.log('object request',req.method,remote.statusCode,req.headers['x-oss-copy-source']?'copy':'data');
 });upstream.on('error',()=>{if(!res.headersSent)res.writeHead(502);res.end()});req.pipe(upstream);
});
server.listen(3431,'127.0.0.1',async()=>{await writeFile(r.tlsReadyPath,'',{flag:'wx',mode:0o600})});
