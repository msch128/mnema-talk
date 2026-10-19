import { t } from '../i18n'
import type { ApiReply, ApiRequest } from './api'
import { NativeChatView, nativeReceiptId, type NativeTextRow } from './nativeChatView'
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

type ChatRows = ReturnType<NativeChatView['messages']>
type Selected = NonNullable<ReturnType<NativeChatView['lookup']>>
const ANCHORS = ['before', 'after', 'around']

/** Only a closed set of query parameters, each once, with at most one anchor. */
function listingParamsAllowed(params: URLSearchParams) {
  return !([...params.keys()].some(k=>!['limit','before','after','around'].includes(k)) || [...params.keys()].some(k=>params.getAll(k).length!==1) || (params.has('limit') && params.get('limit')!=='50') || ANCHORS.filter(k=>params.has(k)).length>1)
}

/** The page a listing asks for, or null when its anchor is not a visible top-level message. */
function pageOf(view: NativeChatView, all: ChatRows, params: URLSearchParams): ChatRows | null {
  let rows=all
  for(const k of ANCHORS) {
    const id=params.get(k);if(!id)continue
    const anchor=view.lookup(id)?.row
    if(!anchor || anchor.deleted || anchor.parent_id)return null
    if(k==='before')rows=rows.filter(row=>row.number<anchor.number).slice(-50)
    if(k==='after')rows=rows.filter(row=>row.number>anchor.number).slice(0,50)
    if(k==='around'){const index=rows.findIndex(row=>row.id===id);rows=rows.slice(Math.max(0,index-25),index+26)}
  }
  if(!params.has('before')&&!params.has('after')&&!params.has('around'))rows=rows.slice(-50)
  return rows
}

/** The typed create or reply a new-message body stands for, or null when it asks for anything else. */
function newMessageInput(input: Record<string,unknown>): Record<string,unknown> | null {
  const keys=Object.keys(input)
  if(!keys.every(k=>['content','parent_id','reply_to_id'].includes(k)) || typeof input['content']!=='string' || !input['content'].trim() || input['content'].length>4000 || (input['parent_id']!==undefined&&!nativeReceiptId(input['parent_id'])) || (input['reply_to_id']!==undefined&&!nativeReceiptId(input['reply_to_id'])))return null
  return {kind:input['parent_id'] || input['reply_to_id'] ? 'reply':'create',body:input['content'],...(input['parent_id']?{parent_receipt_id:input['parent_id']}:{}),...(input['reply_to_id']?{quoted_receipt_id:input['reply_to_id']}:{})}
}

/** The typed edit, delete or reaction a request on an existing message stands for, or null. */
function messageActionInput(request: ApiRequest, isEdit: boolean, isReaction: boolean, target: string | undefined, selected: Selected): Record<string,unknown> | null {
  let input:Record<string,unknown>|null=null
  if(isEdit && request.method==='PUT' && object(request.json) && Object.keys(request.json).length===1 && typeof request.json['content']==='string' && request.json['content'].trim() && request.json['content'].length<=4000)input={kind:'edit',target_receipt_id:target,expected_revision:selected.row.revision_id,body:request.json['content']}
  if(isEdit && request.method==='DELETE' && request.json===undefined)input={kind:'delete',target_receipt_id:target,expected_revision:selected.row.revision_id}
  // Native toggling derives the actual current account and admitted ledger.
  // Renderer provides only an emoji claim, never an author or add/remove grant.
  if(isReaction && request.method==='POST' && object(request.json) && Object.keys(request.json).length===1 && typeof request.json['emoji']==='string')input={kind:'reaction',target_receipt_id:target,emoji:request.json['emoji'],action:'toggle'}
  return input
}

/** The one batch of messages a native call delivers on its channel; a second delivery makes it invalid. */
function createIntake(stale: () => boolean) {
  let wake!:()=>void
  const arrival=new Promise<void>(resolve=>{wake=resolve})
  const intake={
    delivered:false, invalid:false, batch:undefined as unknown, arrival,
    receive:(value:unknown)=>{
      if(stale())return
      if(intake.delivered) intake.invalid=true
      else {intake.delivered=true;intake.batch=value}
      wake()
    }
  }
  return intake
}

/** Waits for the batch, but at most five seconds; cancel() clears the timer. */
function waitForBatch(arrival: Promise<void>) {
  let timer:ReturnType<typeof setTimeout>|undefined
  const done=Promise.race([arrival,new Promise<void>(resolve=>{timer=setTimeout(resolve,5000)})])
  return{done,cancel:()=>{if(timer!==undefined)clearTimeout(timer)}}
}

/** The message a completed acknowledgement names (null for a snapshot); throws on anything else. */
function completedMessageId(body: unknown, input: Record<string,unknown> | null, intact: boolean) {
  if(!object(body) || body['state']!=='completed' || Object.keys(body).length!==(input ? 2:1) || !intact)throw new Error('Native chat completion unavailable')
  const affected=input ? body['message_id']:null
  if(input && !nativeReceiptId(affected))throw new Error('Invalid native chat receipt acknowledgement')
  return affected
}

/** The mutation's receipt must show exactly what was asked for. */
function receiptMatches(result: NativeTextRow | undefined, affected: unknown, input: Record<string,unknown>, event: string | null) {
  return !(!result || (input['target_receipt_id'] && affected!==input['target_receipt_id']) || (['create','reply'].includes(String(input['kind'])) && (result.client_event_id!==event || result.body!==input['body'] || result.parent_id!==(input['parent_receipt_id']??null) || result.reply_to_id!==(input['quoted_receipt_id']??null))) || (input['kind']==='edit' && (result.deleted || result.body!==input['body'])) || (input['kind']==='delete' && !result.deleted))
}

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
    const stale=()=>generation!==this.generation || !bridge.isCurrent() || !!signal?.aborted
    const current=()=>{if(stale())throw cancelled()}
    const channel=bridge.channel()
    const intake=createIntake(stale)
    let wait:ReturnType<typeof waitForBatch>|undefined
    const {fingerprint,event}=this.intentFor(channelId,input)
    this.busy=true
    channel.onmessage=intake.receive
    try {
      const reply=await bridge.call(input ? 'native_chat_mutate':'native_chat_snapshot', {channelId,...(input ? {input,clientEventId:event}:{}),onMessages:channel},signal)
      current()
      if(reply.status!==200)return{reply,affected:null}
      if(!intake.delivered) {wait=waitForBatch(intake.arrival);await wait.done;current()}
      const affected=completedMessageId(reply.body,input,intake.delivered&&!intake.invalid)
      // Validate on a temporary view, so bad acknowledgements cannot mutate the
      // current display cache. Publication follows both the final fence and ACK.
      const validated=new NativeChatView();validated.accept(intake.batch,channelId)
      if(input && !receiptMatches(validated.lookup(String(affected))?.row,affected,input,event))throw new Error('Wrong native chat mutation receipt')
      current();this.view.accept(intake.batch,channelId)
      if(fingerprint)this.attempts.delete(fingerprint)
      return{reply,affected:typeof affected==='string'?affected:null}
    } finally {
      wait?.cancel()
      channel.onmessage=()=>{}
      if(generation===this.generation)this.busy=false
    }
  }
  private intentFor(channelId: string, input: Record<string,unknown> | null) {
    const fingerprint=input ? JSON.stringify([channelId,input]) : null
    const event=fingerprint ? this.attempts.get(fingerprint) ?? crypto.randomUUID() : null
    if(fingerprint && event) this.rememberAttempt(fingerprint,event)
    return{fingerprint,event}
  }
  private rememberAttempt(fingerprint: string, event: string) {
    if(this.attempts.size>=64 && !this.attempts.has(fingerprint)) throw new Error('Native chat unresolved operations limit')
    // An explicit retry may reuse this same public intent. No request or
    // uncertain mutation is ever replayed automatically by the bridge.
    this.attempts.set(fingerprint,event)
  }
  private async listMessages(bridge: NativeTypedChatBridge, channelId: string, params: URLSearchParams, signal?: AbortSignal): Promise<ApiReply> {
    const {reply}=await this.request(bridge,channelId,null,signal)
    if(reply.status!==200)return reply
    const rows=pageOf(this.view,this.view.messages(channelId).filter(row=>!row.parent_id),params)
    return rows ? {status:200,body:rows} : {status:404,body:{error:{code:'NOT_FOUND'}}}
  }
  private async postMessage(bridge: NativeTypedChatBridge, channelId: string, typed: Record<string,unknown>, signal?: AbortSignal): Promise<ApiReply> {
    const {reply,affected}=await this.request(bridge,channelId,typed,signal)
    if(reply.status!==200)return reply
    const result=this.view.messages(channelId).find(row=>row.id===affected)
    if(!result)throw new Error('Native created message unavailable')
    return{status:201,body:result}
  }
  private async handleChannelMessages(request: ApiRequest, bridge: NativeTypedChatBridge, channelId: string, query: string | undefined): Promise<ApiReply> {
    if(request.method==='GET' && request.json===undefined) {
      const params=new URLSearchParams(query??'')
      if(!listingParamsAllowed(params))return denied()
      for(const k of ANCHORS)if(params.has(k)&&!nativeReceiptId(params.get(k)))return denied()
      return this.listMessages(bridge,channelId,params,request.signal)
    }
    if(request.method==='POST' && !query && object(request.json)) {
      const typed=newMessageInput(request.json)
      return typed ? this.postMessage(bridge,channelId,typed,request.signal) : denied()
    }
    return denied()
  }
  private async readThread(bridge: NativeTypedChatBridge, channel: string, target: string | undefined, signal?: AbortSignal): Promise<ApiReply> {
    const {reply}=await this.request(bridge,channel,null,signal)
    if(reply.status!==200)return reply
    const rows=this.view.messages(channel);const root=rows.find(row=>row.id===target)
    if(!root || root.parent_id)return denied()
    return{status:200,body:{root,replies:rows.filter(row=>row.parent_id===target)}}
  }
  async handle(path: string, request: ApiRequest, bridge: NativeTypedChatBridge): Promise<ApiReply|null> {
    const listing=timeline.exec(path);const edit=mutation.exec(path);const replies=thread.exec(path);const reactions=reaction.exec(path)
    if(!listing && !edit && !replies && !reactions)return null
    if(request.form)return denied()
    if(listing?.[1] && nativeReceiptId(listing[1])) return this.handleChannelMessages(request,bridge,listing[1],listing[2])
    const target=edit?.[2]??replies?.[1]??reactions?.[1]
    const selected=target ? this.view.lookup(target):null
    if(!selected || selected.row.deleted || (edit?.[1] && edit[1]!==selected.channel))return denied()
    if(replies && request.method==='GET' && request.json===undefined) return this.readThread(bridge,selected.channel,target,request.signal)
    const input=messageActionInput(request,!!edit,!!reactions,target,selected)
    if(!input)return denied()
    const {reply}=await this.request(bridge,selected.channel,input,request.signal)
    if(reply.status!==200)return reply
    if(input['kind']==='delete')return{status:204,body:null}
    const changed=this.view.messages(selected.channel).find(row=>row.id===target)
    if(!changed)throw new Error('Native changed message unavailable')
    return{status:200,body:input['kind']==='reaction'?{message_id:target,reactions:changed.reactions}:changed}
  }
}
