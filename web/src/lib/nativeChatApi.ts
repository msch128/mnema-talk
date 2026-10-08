import { t } from '../i18n'
import type { ApiReply, ApiRequest } from './api'
import { NativeChatView, nativeReceiptId } from './nativeChatView'
import type { StatusChannel } from './trustPort'

interface Reply extends ApiReply { context: string }
export interface NativeTypedChatBridge {
  readonly context: string
  isCurrent(): boolean
  call(command: string, args: Record<string, unknown>, signal?: AbortSignal): Promise<Reply>
  channel(): StatusChannel
}
const object = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const denied = (): ApiReply => ({status:503,body:{error:{code:'UNAVAILABLE',message:t('nativeDesktop.chatAuthorizationRequired')}}})
const cancelled = () => new DOMException('Native chat operation retired','AbortError')
const uuid = '([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})'
const timeline = new RegExp(`^/api/channels/${uuid}/messages(?:\\?([^#]*))?$`)
const mutation = new RegExp(`^/api/channels/${uuid}/messages/${uuid}$`)
const thread = new RegExp(`^/api/messages/${uuid}/thread$`)
const reaction = new RegExp(`^/api/messages/${uuid}/reactions$`)
export function isNativeTypedChatPath(path:string) { return timeline.test(path)||mutation.test(path)||thread.test(path)||reaction.test(path) }

/** Existing shared store paths map only to these fixed native typed commands.
 * No fetch, plaintext route, credential, source grant, or MLS wire is exposed.
 */
export class NativeTypedChatApi {
  readonly view = new NativeChatView()
  private generation = 0
  private busy = false
  private attempts = new Map<string,string>()
  clear() { this.generation++; this.busy=false; this.attempts.clear(); this.view.clear() }
  private async request(bridge: NativeTypedChatBridge, channelId: string, input: Record<string,unknown> | null, signal?: AbortSignal): Promise<{reply:Reply; affected:string|null}> {
    if (!bridge.isCurrent() || signal?.aborted) throw cancelled()
    if (this.busy) throw new Error('Native chat operation already pending')
    const generation=this.generation
    const current=()=>{if(generation!==this.generation || !bridge.isCurrent() || signal?.aborted)throw cancelled()}
    const channel=bridge.channel()
    let delivered=false; let invalid=false; let batch:unknown
    let wake!:()=>void
    const arrival=new Promise<void>(resolve=>{wake=resolve})
    let timer:ReturnType<typeof setTimeout>|undefined
    const fingerprint=input ? JSON.stringify([channelId,input]) : null
    const event=fingerprint ? this.attempts.get(fingerprint) ?? crypto.randomUUID() : null
    if(fingerprint && event) {
      if(this.attempts.size>=64 && !this.attempts.has(fingerprint)) throw new Error('Native chat unresolved operations limit')
      // An explicit retry may reuse this same public intent. No request or
      // uncertain mutation is ever replayed automatically by the bridge.
      this.attempts.set(fingerprint,event)
    }
    this.busy=true
    channel.onmessage=value=>{
      if(generation!==this.generation || !bridge.isCurrent() || signal?.aborted)return
      if(delivered) invalid=true
      else {delivered=true;batch=value}
      wake()
    }
    try {
      const reply=await bridge.call(input ? 'native_chat_mutate':'native_chat_snapshot', {channelId,...(input ? {input,clientEventId:event}:{}),onMessages:channel},signal)
      current()
      if(reply.status!==200)return{reply,affected:null}
      if(!delivered) {await Promise.race([arrival,new Promise<void>(resolve=>{timer=setTimeout(resolve,5000)})]);current()}
      if(!object(reply.body) || reply.body['state']!=='completed' || Object.keys(reply.body).length!==(input ? 2:1) || !delivered || invalid)throw new Error('Native chat completion unavailable')
      const affected=input ? reply.body['message_id']:null
      if(input && !nativeReceiptId(affected))throw new Error('Invalid native chat receipt acknowledgement')
      // Validate on a temporary view, so bad acknowledgements cannot mutate the
      // current display cache. Publication follows both the final fence and ACK.
      const validated=new NativeChatView();validated.accept(batch,channelId)
      if(input) {
        const result=validated.lookup(String(affected))?.row
        if(!result || (input['target_receipt_id'] && affected!==input['target_receipt_id']) || (['create','reply'].includes(String(input['kind'])) && (result.client_event_id!==event || result.body!==input['body'] || result.parent_id!==(input['parent_receipt_id']??null) || result.reply_to_id!==(input['quoted_receipt_id']??null))) || (input['kind']==='edit' && (result.deleted || result.body!==input['body'])) || (input['kind']==='delete' && !result.deleted))throw new Error('Wrong native chat mutation receipt')
      }
      current();this.view.accept(batch,channelId)
      if(fingerprint)this.attempts.delete(fingerprint)
      return{reply,affected:typeof affected==='string'?affected:null}
    } finally {
      if(timer!==undefined)clearTimeout(timer)
      channel.onmessage=()=>{}
      if(generation===this.generation)this.busy=false
    }
  }
  async handle(path: string, request: ApiRequest, bridge: NativeTypedChatBridge): Promise<ApiReply|null> {
    const listing=timeline.exec(path);const edit=mutation.exec(path);const replies=thread.exec(path);const reactions=reaction.exec(path)
    if(!listing && !edit && !replies && !reactions)return null
    if(request.form)return denied()
    if(listing?.[1] && nativeReceiptId(listing[1])) {
      const channelId=listing[1]
      if(request.method==='GET' && request.json===undefined) {
        const params=new URLSearchParams(listing[2]??'')
        if([...params.keys()].some(k=>!['limit','before','after','around'].includes(k)) || [...params.keys()].some(k=>params.getAll(k).length!==1) || (params.has('limit') && params.get('limit')!=='50') || ['before','after','around'].filter(k=>params.has(k)).length>1)return denied()
        for(const k of ['before','after','around'])if(params.has(k)&&!nativeReceiptId(params.get(k)))return denied()
        const {reply}=await this.request(bridge,channelId,null,request.signal)
        if(reply.status!==200)return reply
        let rows=this.view.messages(channelId).filter(row=>!row.parent_id)
        for(const k of ['before','after','around']) {
          const id=params.get(k);if(!id)continue
          const anchor=this.view.lookup(id)?.row
          if(!anchor || anchor.deleted || anchor.parent_id)return{status:404,body:{error:{code:'NOT_FOUND'}}}
          if(k==='before')rows=rows.filter(row=>row.number<anchor.number).slice(-50)
          if(k==='after')rows=rows.filter(row=>row.number>anchor.number).slice(0,50)
          if(k==='around'){const index=rows.findIndex(row=>row.id===id);rows=rows.slice(Math.max(0,index-25),index+26)}
        }
        if(!params.has('before')&&!params.has('after')&&!params.has('around'))rows=rows.slice(-50)
        return{status:200,body:rows}
      }
      if(request.method==='POST' && !listing[2] && object(request.json)) {
        const input=request.json;const keys=Object.keys(input)
        if(!keys.every(k=>['content','parent_id','reply_to_id'].includes(k)) || typeof input['content']!=='string' || !input['content'].trim() || input['content'].length>4000 || (input['parent_id']!==undefined&&!nativeReceiptId(input['parent_id'])) || (input['reply_to_id']!==undefined&&!nativeReceiptId(input['reply_to_id'])))return denied()
        const typed={kind:input['parent_id'] || input['reply_to_id'] ? 'reply':'create',body:input['content'],...(input['parent_id']?{parent_receipt_id:input['parent_id']}:{}),...(input['reply_to_id']?{quoted_receipt_id:input['reply_to_id']}:{})}
        const {reply,affected}=await this.request(bridge,channelId,typed,request.signal)
        if(reply.status!==200)return reply
        const result=this.view.messages(channelId).find(row=>row.id===affected)
        if(!result)throw new Error('Native created message unavailable')
        return{status:201,body:result}
      }
      return denied()
    }
    const target=edit?.[2]??replies?.[1]??reactions?.[1]
    const selected=target ? this.view.lookup(target):null
    if(!selected || selected.row.deleted || (edit?.[1] && edit[1]!==selected.channel))return denied()
    if(replies && request.method==='GET' && request.json===undefined) {
      const {reply}=await this.request(bridge,selected.channel,null,request.signal)
      if(reply.status!==200)return reply
      const rows=this.view.messages(selected.channel);const root=rows.find(row=>row.id===target)
      if(!root || root.parent_id)return denied()
      return{status:200,body:{root,replies:rows.filter(row=>row.parent_id===target)}}
    }
    let input:Record<string,unknown>|null=null
    if(edit && request.method==='PUT' && object(request.json) && Object.keys(request.json).length===1 && typeof request.json['content']==='string' && request.json['content'].trim() && request.json['content'].length<=4000)input={kind:'edit',target_receipt_id:target,expected_revision:selected.row.revision_id,body:request.json['content']}
    if(edit && request.method==='DELETE' && request.json===undefined)input={kind:'delete',target_receipt_id:target,expected_revision:selected.row.revision_id}
    // Native toggling derives the actual current account and admitted ledger.
    // Renderer provides only an emoji claim, never an author or add/remove grant.
    if(reactions && request.method==='POST' && object(request.json) && Object.keys(request.json).length===1 && typeof request.json['emoji']==='string')input={kind:'reaction',target_receipt_id:target,emoji:request.json['emoji'],action:'toggle'}
    if(!input)return denied()
    const {reply}=await this.request(bridge,selected.channel,input,request.signal)
    if(reply.status!==200)return reply
    if(input['kind']==='delete')return{status:204,body:null}
    const changed=this.view.messages(selected.channel).find(row=>row.id===target)
    if(!changed)throw new Error('Native changed message unavailable')
    return{status:200,body:input['kind']==='reaction'?{message_id:target,reactions:changed.reactions}:changed}
  }
}
