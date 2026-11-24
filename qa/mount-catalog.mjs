import assert from 'node:assert/strict';
import http from 'node:http';
import net from 'node:net';
import { readFile } from 'node:fs/promises';
const data=JSON.parse(await readFile(process.argv[2],'utf8'));
const api=new URL(data.apiOrigin),site=new URL(data.frontendOrigin);
for(const u of [site,api]) assert(u.protocol==='http:'&&u.hostname==='127.0.0.1'&&u.port&&!u.password&&!u.username);
const next='http://127.0.0.1:3991';
const server=http.createServer((req,res)=>{
  const upstream = req.url.startsWith('/api/') ? api : new URL(next);
  const proxy = http.request(new URL(req.url,upstream),{method:req.method,headers:req.headers},incoming=>{
    res.writeHead(incoming.statusCode,incoming.headers);
    incoming.pipe(res);
  });
  proxy.on('error',()=>{if(!res.headersSent)res.writeHead(502);res.end();});
  res.on('close',()=>proxy.destroy());
  req.pipe(proxy);
});
server.on('upgrade',(req,down,head)=>{
  const up=net.connect(3991,'127.0.0.1',()=>{
    const headers=req.rawHeaders.reduce((out,v,i)=>out+(i%2?v+'\r\n':v+': '),'');
    up.write(req.method+' '+req.url+' HTTP/'+req.httpVersion+'\r\n'+headers+'\r\n');if(head.length) up.write(head);down.pipe(up).pipe(down);
  });
  up.on('error',()=>down.destroy());down.on('error',()=>up.destroy());
});
server.listen(Number(site.port),site.hostname);
