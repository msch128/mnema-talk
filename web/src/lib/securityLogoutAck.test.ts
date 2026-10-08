import { expect, it, vi } from 'vitest'
import { initializeNativeContext, installNativePort, nativeApiTransport } from './nativeTransport'
const CONTEXT='10000000-0000-4000-8000-000000000001'
const PROFILE='20000000-0000-4000-8000-000000000001'
const FIRST='50000000-0000-4000-8000-000000000001'
it('recovers only a comparison descriptor before fresh explicit login after unknown logout ACK',async()=>{
 let current:string|null=FIRST
 const invoke=vi.fn(async(command:string,args?:Record<string,unknown>):Promise<unknown>=>{
  if(command==='native_context')return{context:CONTEXT,profile_intent:PROFILE,authentication_intent:current,content_authorization:'unavailable',remembered_login:false}
  if(command==='native_auth_logout'){current=null;throw new Error('synthetic lost logout ACK')}
  if(command==='native_auth_login'){expect(args?.authenticationIntent).toBe(null);return{context:CONTEXT,status:200,body:{user:{id:CONTEXT},authentication_intent:FIRST}}}
  throw new Error('unexpected synthetic command')
 })
 installNativePort({invoke,channel:()=>({onmessage(){}})})
 await initializeNativeContext();invoke.mockClear()
 await expect(nativeApiTransport('/api/auth/logout',{method:'POST'})).rejects.toThrow('lost logout ACK')
 await nativeApiTransport('/api/auth/login',{method:'POST',json:{username:'synthetic',password:'synthetic-fixture-password'}})
 expect(invoke.mock.calls.map(c=>c[0])).toEqual(['native_auth_logout','native_context','native_auth_login'])
})
it('a late lost logout ACK cannot burn a newer accepted Login selector', async () => {
 const SECOND='50000000-0000-4000-8000-000000000002'
 let rejectLogout!:(error:Error)=>void
 const invoke=vi.fn(async(command:string,args?:Record<string,unknown>):Promise<unknown>=>{
  if(command==='native_context')return{context:CONTEXT,profile_intent:PROFILE,authentication_intent:FIRST,content_authorization:'unavailable',remembered_login:false}
  if(command==='native_auth_logout')return await new Promise((_resolve,reject)=>{rejectLogout=reject})
  if(command==='native_auth_login')return{context:CONTEXT,status:200,body:{user:{id:CONTEXT},authentication_intent:SECOND}}
  if(command==='native_auth_me'){expect(args?.authenticationIntent).toBe(SECOND);return{context:CONTEXT,status:200,body:{id:CONTEXT}}}
  throw new Error('unexpected synthetic command')
 })
 installNativePort({invoke,channel:()=>({onmessage(){}})});await initializeNativeContext()
 const old=nativeApiTransport('/api/auth/logout',{method:'POST'})
 const failed=expect(old).rejects.toThrow('lost old ACK')
 await nativeApiTransport('/api/auth/login',{method:'POST',json:{username:'synthetic',password:'synthetic-fixture-password'}})
 rejectLogout(new Error('lost old ACK'));await failed
 await nativeApiTransport('/api/auth/me',{method:'GET'})
 expect(invoke.mock.calls.filter(c=>c[0]==='native_context')).toHaveLength(1)
})
it('a rejected logout ACK burns the old selector without replay and reads descriptor only on explicit Login', async () => {
 let current:string|null=FIRST
 const invoke=vi.fn(async(command:string,args?:Record<string,unknown>):Promise<unknown>=>{
  if(command==='native_context')return{context:CONTEXT,profile_intent:PROFILE,authentication_intent:current,content_authorization:'unavailable',remembered_login:false}
  if(command==='native_auth_logout'){current=null;return{context:CONTEXT,status:503,body:null}}
  if(command==='native_auth_login'){expect(args?.authenticationIntent).toBe(null);return{context:CONTEXT,status:401,body:null}}
  throw new Error('unexpected synthetic command')
 })
 installNativePort({invoke,channel:()=>({onmessage(){}})});await initializeNativeContext();invoke.mockClear()
 expect((await nativeApiTransport('/api/auth/logout',{method:'POST'})).status).toBe(503)
 expect(invoke.mock.calls.map(c=>c[0])).toEqual(['native_auth_logout'])
 await nativeApiTransport('/api/auth/login',{method:'POST',json:{username:'synthetic',password:'synthetic-fixture-password'}})
 expect(invoke.mock.calls.map(c=>c[0])).toEqual(['native_auth_logout','native_context','native_auth_login'])
})
