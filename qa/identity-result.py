"""Exclude signed URLs and credentials from the persistent receipt."""
import hashlib,json,pathlib,re,subprocess,sys
work=pathlib.Path(sys.argv[1]); name=sys.argv[2]; paths=sys.argv[3:5]
def rev(p):return subprocess.check_output(['git','-C',p,'rev-parse','HEAD'],text=True).strip()
def code(n):
 p=work/(n+'.exit');return int(p.read_text()) if p.is_file() else None
rows=[];diagnostics=[]
raw=(work/'go.private').read_bytes()
for l in raw.splitlines():
 try:o=json.loads(l)
 except ValueError:continue
 if o['Action']!='output':rows.append({k:o[k] for k in ('Time','Action','Package','Test','Elapsed') if k in o})
 elif 'expected=' in o.get('Output','') or '.go:' in o.get('Output','') and any(w in o['Output'] for w in ['cannot','unavailable','timeout','rejected','required','failure']):
  diagnostics.append(re.sub(r'https?://\S+','[local]',o['Output']).strip()[:210])
r={'fixture':name,'backend':rev(paths[0]),'frontend':rev(paths[1]),'binarySHA256':hashlib.sha256((work/'identity.test').read_bytes()).hexdigest(),'fullGoLogSHA256':hashlib.sha256(raw).hexdigest(),'goExit':code('go'),'browserExit':code('browser'),'events':rows,'diagnostics':diagnostics}
p=work/'browser.private'
if p.is_file() and code('browser')!=0:
 lines=p.read_text().splitlines(); r['browserError']=next((re.sub(r'https?://\S+','[local]',l.strip())[:150] for l in lines if any(x in l for x in ['Timeout','Error:','AssertionError'])),'assertion failed')
 r['browserSites']=[l.strip() for l in lines if re.search(r'(identity-[\w-]+|ai_configuration_browser)\.mjs:\d+',l)][:3]
p=work/'gateway.log'
if p.is_file():r['authHTTP']=re.findall(r'^auth request (POST|GET) (/api/v1/auth/(?:me|login)) ([0-9]{3})$',p.read_text(),re.M)
if p.is_file():r['objectProtocolRequests']=re.findall(r'^object request (GET|HEAD|PUT|POST|DELETE) ([0-9]{3}) (copy|data)$',p.read_text(),re.M)
print('Named case',name,'Go exit',r['goExit'],'browser exit',r['browserExit'])
(work/'identity-public'/'named-receipt.json').write_text(json.dumps(r,indent=2)+'\n')

for filename in ('request-diagnostics.json','stage-diagnostics.json'):
 p=work/'provider-state'/filename
 if p.exists(): (work/'identity-public'/filename).write_bytes(p.read_bytes())
