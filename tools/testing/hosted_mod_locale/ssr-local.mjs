// Route only this test build's declared API hostname to its owned HTTP server.
const currentFetch=globalThis.fetch;
const destination=new URL(process.env.MCMODS_LIVE_DESTINATION);
if (destination.hostname!=='127.0.0.1') throw new Error('only a task-owned loopback API is valid');
globalThis.fetch=function(source, init){
  let url;
  try {url=new URL(typeof source==='string'?source:source instanceof URL?source.href:source.url);} catch {return currentFetch(source,init);}
  const embedded=new URL(process.env.MCMODS_LIVE_EMBEDDED);
  if(url.origin===embedded.origin){
    const path=embedded.pathname==='/service'?url.pathname.replace(/^\/service(?=\/)/,''):url.pathname;
    return currentFetch(new URL(path+url.search,destination),init);
  }
  return currentFetch(source,init);
};
