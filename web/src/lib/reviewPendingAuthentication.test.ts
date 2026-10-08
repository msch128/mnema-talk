import { expect,it } from 'vitest'
import { initializeNativeContext,installNativePort,nativeApiTransport } from './nativeTransport'
it('rejects an older delivered Me reply after a newer password intent has begun',async()=>{
 const context='10000000-0000-4000-8000-000000000001', profile='20000000-0000-4000-8000-000000000001', first='50000000-0000-4000-8000-000000000001', second='50000000-0000-4000-8000-000000000002'
 let finishMe!:(v:unknown)=>void,finishPassword!:(v:unknown)=>void
 installNativePort({channel:()=>({onmessage:()=>{}}),invoke:async command=>{
  if(command==='native_context')return {context,profile_intent:profile,authentication_intent:first,content_authorization:'unavailable',remembered_login:false}
  if(command==='native_auth_me')return await new Promise(resolve=>{finishMe=resolve})
  if(command==='native_auth_password')return await new Promise(resolve=>{finishPassword=resolve})
  return {context,status:204,body:null}
 }})
 await initializeNativeContext()
 const old=nativeApiTransport('/api/auth/me',{method:'GET'})
 const changed=nativeApiTransport('/api/auth/password',{method:'PUT',json:{current_password:'synthetic-old',new_password:'synthetic-new'}})
 finishMe({context,status:401,body:{error:{code:'INVALID_CREDENTIALS'}}})
 const observed=await old.then(value=>({accepted:value.status}),error=>({rejected:error.name}))
 finishPassword({context,status:200,body:{authentication_intent:second}});await changed
 expect(observed).toEqual({rejected:'AbortError'})
})
