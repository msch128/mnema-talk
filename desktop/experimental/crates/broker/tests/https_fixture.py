#!/usr/bin/env python3
"""Loopback-only ephemeral HTTPS fixture; generated CA/key never leave owned temp dir.
No real credentials, external URL access, OS trust changes, or application database.
"""
import sys,json,ssl,subprocess,threading,time,base64,datetime,socket,hashlib,struct
from pathlib import Path
from http.server import ThreadingHTTPServer,BaseHTTPRequestHandler
root=Path(sys.argv[1]);root.mkdir(parents=True,exist_ok=False);root.chmod(0o700)
def cmd(*args):subprocess.run(args,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
cmd('openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(root/'ca.key'),'-out',str(root/'ca.pem'),'-days','1','-subj','/CN=Mnema Private Loopback Fixture CA','-addext','basicConstraints=critical,CA:TRUE','-addext','keyUsage=critical,keyCertSign,cRLSign')
cmd('openssl','req','-new','-newkey','rsa:2048','-nodes','-keyout',str(root/'server.key'),'-out',str(root/'server.csr'),'-subj','/CN=127.0.0.1')
(root/'extensions.txt').write_text('subjectAltName=IP:127.0.0.1\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n')
cmd('openssl','x509','-req','-in',str(root/'server.csr'),'-CA',str(root/'ca.pem'),'-CAkey',str(root/'ca.key'),'-CAcreateserial','-out',str(root/'server.pem'),'-days','1','-extfile',str(root/'extensions.txt'))
for key in root.glob('*.key'):key.chmod(0o600)
lock=threading.Lock();calls=[];sequence=0;instance=None;family='33333333-3333-3333-3333-333333333333';user_id='11111111-1111-1111-1111-111111111111'
now=datetime.datetime.now(datetime.timezone.utc);expiry=(now+datetime.timedelta(days=29)).isoformat().replace('+00:00','Z')
def token(n):return base64.urlsafe_b64encode(bytes([n])*32).decode().rstrip('=')
def user():return {'id':user_id,'username':'fixture','display_name':'Fixture','bio':'','role':'user','status_text':'','locale':'en','created_at':now.isoformat().replace('+00:00','Z')}
relay_events=[]
issued_expiry=None
def grant():
 global issued_expiry
 issued_expiry=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(minutes=4)).isoformat().replace('+00:00','Z')
 return {'user':user(),'family_id':family,'client_instance_id':instance,'refresh_sequence':sequence,'access_token':token(41+sequence*10),'refresh_token':token(42+sequence*10),'access_expires_at':issued_expiry,'family_expires_at':expiry}
def mode_for(path):
 try:return json.loads((root/'control.json').read_text()).get(path,{})
 except (FileNotFoundError,json.JSONDecodeError):return {}
def save():
 temp=root/'state.tmp';temp.write_text(json.dumps({'calls':calls,'sequence':sequence}));temp.replace(root/'state.json')
class Handler(BaseHTTPRequestHandler):
 protocol_version='HTTP/1.1'
 def log_message(self,*args):pass
 def do_GET(self):self.run_request()
 def do_POST(self):self.run_request()
 def do_PUT(self):self.run_request()
 def run_request(self):
  global sequence,instance
  path=self.path;size=int(self.headers.get('Content-Length','0'));body=self.rfile.read(size) if size else b''
  with lock:calls.append({'path':path,'method':self.command,'headers':dict(self.headers),'body':body.decode('utf-8'),'sequence_before':sequence,'port':self.server.server_port});save()
  control=mode_for(path);mode=control.get('mode','normal');data=None;status=200
  if path=='/api/native/v1/ws':self.websocket(mode,control);return
  if self.command=='POST' and path=='/api/native/v1/auth/login':
   fields=json.loads(body);instance=fields['client_instance_id'];sequence=0;data=grant()
  elif self.command=='POST' and path=='/api/native/v1/auth/refresh':
   fields=json.loads(body)
   if fields['refresh_token']!=token(42+sequence*10):status=401;data={}
   else:
    with lock:sequence+=1;save()
    data=grant()
  elif '/ciphertext-events' in path and path.startswith('/api/native/v1/channels/'):
   channel=path.split('/')[5]
   if self.command=='POST':
    fields=json.loads(body)
    with lock:
     existing=next((x for x in relay_events if x['client_event_id']==fields['client_event_id']),None)
     if existing:data=dict(existing)
     else:
      data={'id':'55555555-5555-5555-5555-555555555555','number':len(relay_events)+1,'channel_id':channel,'user_id':user_id,'created_at':now.isoformat().replace('+00:00','Z'),**fields};relay_events.append(dict(data));status=201
   elif self.command=='GET':
    from urllib.parse import urlsplit,parse_qs
    after=int(parse_qs(urlsplit(path).query).get('after',['0'])[0])
    events=[dict(x) for x in relay_events if x['channel_id']==channel and x['number']>after][:10]
    data={'events':events,'next_after':events[-1]['number'] if events else after}
   else:status=404;data={}
  elif self.command=='GET' and path=='/.well-known/mnema':data={'protocol':'mnema-desktop-discovery-v1','community_id':'fixture-community','api_versions':[1],'e2ee_required':True,'native_api':{'protocol':'mnema-native-preview-v1','api_version':1,'prefix':'/api/native/v1','compatibility':'supported','authentication':'opaque-bearer-v1','capabilities':['authentication','profile','presence','members','channels','admin_metadata'],'content_authorization':'unavailable'}}
  elif self.command=='GET' and path=='/api/native/v1/auth/me':data=user()
  elif self.command=='GET' and path.startswith('/api/native/v1/users/'):
   data=user();data['id']=path.rsplit('/',1)[1]
  elif self.command=='PUT' and path in ['/api/native/v1/users/me/profile','/api/native/v1/users/me/locale','/api/native/v1/users/me/presence','/api/native/v1/users/me/status']:
   data=user();fields=json.loads(body)
   for key in ['display_name','bio','locale','presence','status_text']:
    if key in fields:data[key]=fields[key]

  elif self.command=='GET' and path=='/api/native/v1/channels':data={'categories':[],'uncategorized':[]}
  elif self.command=='GET' and path=='/api/native/v1/members':data=[user()]
  elif self.command=='GET' and path=='/api/native/v1/read-state':data=[]
  elif self.command=='GET' and path in ['/api/native/v1/health','/api/native/v1/public/health']:data={'status':'ok','version':'fixture'}
  elif self.command=='GET' and path in ['/api/native/v1/legal','/api/native/v1/public/legal']:data={'operator_name':'fixture','operator_email':'','operator_country':'','project_notice':'fixture','media_retention_days':0,'session_expiry_days':30,'stun_servers':[],'turn_servers':[],'update_check':False,'legal_version':'fixture'}
  elif self.command=='POST' and path=='/api/native/v1/auth/logout':status=204
  else:status=404;data={}
  if mode=='relay_wrong_author':data['user_id']='22222222-2222-2222-2222-222222222222'
  if mode=='relay_bad_group':data['group_id']='AA'
  if mode in ['relay_two_account_event','relay_duplicate_account_event']:
   item=dict(data['events'][0]);item['number']+=1;item['id']='88888888-8888-8888-8888-888888888888'
   if mode=='relay_two_account_event':item['user_id']='22222222-2222-2222-2222-222222222222'
   data['events'].append(item);data['next_after']=item['number']
  if mode=='relay_bad_cursor':data['next_after']+=1
  if mode=='relay_unknown_field':data['access_token']='dummy-public-rejected-marker'
  if mode=='hold_admin_me':data['role']='admin'
  if mode=='hold_admin_refresh':
   data['user']['role']='admin'
   deadline=time.monotonic()+10
   while not (root/'refresh-release').exists() and time.monotonic()<deadline:time.sleep(0.005)
  if mode in ('hold','hold_admin_me','hold_unauthorized'):
   deadline=time.monotonic()+10
   while not (root/'release').exists() and time.monotonic()<deadline:time.sleep(0.005)
  if mode in ['drop_after_rotate','relay_drop_ack']:
   try:self.connection.shutdown(socket.SHUT_RDWR)
   except OSError:pass
   self.connection.close();self.close_connection=True;return
  if mode=='redirect':status=307
  elif mode in ['unauthorized','hold_unauthorized']:status=401
  elif mode=='unknown_metadata_field':data={'categories':[],'uncategorized':[],'access_token':'dummy-secret-must-not-publish'}
  elif mode=='admin_login':data['user']['role']='admin'
  elif mode=='personal_unknown_field':data['access_token']='public-synthetic-rejected-marker'
  elif mode=='personal_wrong_account':data['id']='22222222-2222-2222-2222-222222222222'
  elif mode=='personal_bad_bounds':data['display_name']='x'*25
  elif mode=='bad_family':data['family_id']='44444444-4444-4444-4444-444444444444'
  elif mode=='bad_instance':data['client_instance_id']='44444444-4444-4444-4444-444444444444'
  elif mode=='bad_sequence':data['refresh_sequence']+=2
  elif mode=='legacy_grant':
   for k in ['family_id','client_instance_id','refresh_sequence']:data.pop(k,None)
  payload=b'' if status==204 else json.dumps(data).encode()
  if mode in ['oversized','chunked_oversized']:payload=b' '*20000
  if mode in ['metadata_oversized','relay_oversized']:payload=b' '*1048577
  try:
   self.send_response(status);self.send_header('Cache-Control','no-store');self.send_header('Content-Type','text/html' if mode=='html' else 'application/json')
   if mode=='redirect':self.send_header('Location',control.get('location','/api/native/v1/auth/login'))
   if mode=='cookie':self.send_header('Set-Cookie','fixture=value; Secure; HttpOnly')
   if mode=='gzip':self.send_header('Content-Encoding','gzip')
   if mode=='chunked_oversized':self.send_header('Transfer-Encoding','chunked')
   else:self.send_header('Content-Length',str(len(payload)))
   self.end_headers()
   if mode=='chunked_oversized':
    for i in range(0,len(payload),1000):part=payload[i:i+1000];self.wfile.write(f'{len(part):X}\r\n'.encode()+part+b'\r\n');self.wfile.flush()
    self.wfile.write(b'0\r\n\r\n')
   elif mode=='slow_body':
    for byte in payload:self.wfile.write(bytes([byte]));self.wfile.flush();time.sleep(0.1)
   else:self.wfile.write(payload)
   self.wfile.flush()
  except (BrokenPipeError,ConnectionResetError,ssl.SSLError):pass
 def websocket(self,mode,control):
  if mode=='redirect':
   self.send_response(307);self.send_header('Location',control.get('location','/api/native/v1/ws'));self.send_header('Content-Length','0');self.end_headers();return
  if self.headers.get('Authorization')!='Bearer '+token(41+sequence*10):
   self.send_response(401);self.send_header('Content-Length','0');self.end_headers();return
  key=self.headers.get('Sec-WebSocket-Key','');accept=base64.b64encode(hashlib.sha1((key+'258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest()).decode()
  self.send_response(101);self.send_header('Upgrade','websocket');self.send_header('Connection','Upgrade');self.send_header('Sec-WebSocket-Accept',accept)
  if mode=='cookie':self.send_header('Set-Cookie','fixture=value')
  self.end_headers();self.wfile.flush()
  def send(data,opcode=1):
   if not isinstance(data,bytes):data=json.dumps(data).encode()
   size=len(data);header=bytes([0x80|opcode,size]) if size<126 else bytes([0x80|opcode,126])+struct.pack('!H',size) if size<65536 else bytes([0x80|opcode,127])+struct.pack('!Q',size)
   self.wfile.write(header+data);self.wfile.flush()
  def exact(n):
   data=self.rfile.read(n)
   if len(data)!=n:raise EOFError()
   return data
  try:
   if mode=='oversized':send(b'x'*65537);return
   send({'type':'server_info','payload':{'version':'fixture'}})
   send({'type':'presence_snapshot','payload':{user_id:'online'}})
   if mode=='metadata_updates':
    send({'type':'channels_changed','payload':None})
    send({'type':'member_joined','payload':user()})
    send({'type':'user_update','payload':{'id':user_id,'disabled':True}})
    send({'type':'user_stats','payload':{'user_id':user_id,'voice_seconds':123}})
   send({'type':'message_create','payload':{'content':'protected fixture plaintext must not cross metadata bridge'}})
   while True:
    head=exact(2);opcode=head[0]&15;size=head[1]&127
    if size==126:size=struct.unpack('!H',exact(2))[0]
    elif size==127:size=struct.unpack('!Q',exact(8))[0]
    if not(head[1]&128) or size>4096:raise EOFError()
    mask=exact(4);data=exact(size);data=bytes(v^mask[i%4] for i,v in enumerate(data))
    if opcode==8:return
    if opcode==9:send(data,10);continue
    if opcode!=1:continue
    fields=json.loads(data)
    with lock:calls.append({'path':'WS:'+fields['type'],'method':'WSS','body':data.decode(),'headers':{},'port':self.server.server_port});save()
    if fields['type']=='native_access_renew':
     if fields['payload']['access_token']!=token(41+sequence*10):return
     if mode=='lost_ack':continue
     expiry=issued_expiry if mode!='bad_ack' else '2000-01-01T00:00:00Z'
     send({'type':'native_access_renewed','payload':{'access_expires_at':expiry}})
     send({'type':'server_info','payload':{'version':'renewed'}})
    elif fields['type']=='ping':send({'type':'pong','payload':{'t':fields['payload']['t']}})
  except (EOFError,OSError,ValueError,KeyError):pass
  finally:self.close_connection=True

server=ThreadingHTTPServer(('127.0.0.1',0),Handler);server.daemon_threads=True
context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);context.minimum_version=ssl.TLSVersion.TLSv1_2;context.load_cert_chain(root/'server.pem',root/'server.key');server.socket=context.wrap_socket(server.socket,server_side=True)
server2=ThreadingHTTPServer(('127.0.0.1',0),Handler);server2.daemon_threads=True;server2.socket=context.wrap_socket(server2.socket,server_side=True);threading.Thread(target=server2.serve_forever,daemon=True).start()
(root/'control.json').write_text('{}')
with lock:save()
print(json.dumps({'port':server.server_port,'port2':server2.server_port,'root':str(root/'ca.pem')}),flush=True)
server.serve_forever()
