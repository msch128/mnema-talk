# Native operational preview IPC contract (candidate, not yet frozen)

All commands receive an actual injected Tauri WebviewWindow. Native registry validates its OS window identity, local bundled main URL and current incarnation; renderer `context` is a nonsecret replay fence, never identity authority. Only the native-created main window has these ACLs. No remote webview, remote script, arbitrary native HTTP URL/header/cookie, renderer WebSocket or global fetch patch.

Native context command: `native_context()` → `{context:canonicalUUID,profile_intent:canonicalUUID|null,content_authorization:"unavailable",remembered_login:false}`. Bootstrap obtains once per local main-page incarnation; never persist it. Context changes on native page reload/recreation. Reject every queued reply/event whose context differs.

Ordinary reply: `{context:string,status:number,body:unknown}`. API errors use `{error:{code:string,message:constant}}` within body; no raw errors/origins/secrets. Current unauthorized/reauth →401 UNAUTHORIZED; unavailable →503 UNAVAILABLE; busy→429 RATE_LIMITED. Cancelled/stale publication →499 with code NATIVE_CANCELLED/NATIVE_STALE; nativeTransport throws DOMException AbortError for499 so old unauthorized listeners cannot clear a new session. Four auth commands return only user DTOs or null; no access/refresh/family/instance/sequence fields cross IPC.

Each request command accepts `requestId:canonicalUUID` for an owned cancellation label. Native registry rejects duplicate/outstanding excess; renderer cannot cancel another context/window. AbortSignal calls `native_request_cancel({context,profileIntent,requestId})`; cancellation is native and fail closed if a rotating response is uncertain. The public profile comparator below is mandatory on renderer-originated commands; it is comparison data, never native identity or an authorization grant.

- `native_connect({context,profileIntent:canonicalUUID|null,requestId,address})` →200 body `{profile_intent:canonicalUUID,origin,community_id,compatibility:"supported"|"deprecated",transport_preview:true,content_authorization:"unavailable"}`. Fixed HTTPS /.well-known/mnema extended descriptor, then actual profile select. Existing parser login_available=false is not changed into an E2EE-ready flag. Connect UI clearly says this is an auth/metadata development preview; no protected content readiness.
- `native_auth_login({context,profileIntent,requestId,username,password})` →200 body `{user:<existing User DTO>}`. This matches existing decodeUserEnvelope.
- `native_auth_me({context,profileIntent,requestId})` →200 body `<existing User DTO>`; first selected unauthenticated session returns401.
- `native_auth_refresh({context,profileIntent,requestId})` →204 body null (native-only token rotation; no caller payload).
- `native_auth_logout({context,profileIntent,requestId})` →204 body null; native state seals even if server reply fails.
- `native_disconnect({context,profileIntent:canonicalUUID|null})` →204 body null; seals profile/native session synchronously, retains main-page context, requires fresh profile/login.

Construct a native createApiClient transport in derivative `ui/src/lib/api.ts`; replace its final browserClient construction with nativeClient while retaining shared decoding/listener logic. Map exact `/api/auth/login` POST, `/api/auth/me` GET and `/api/auth/logout` POST to commands above. All unimplemented route/method/form combos return503/QualificationRequired; never fall back to browser fetch. Preserve account-only envelope/body shapes. Resource bridge pending: native user DTO currently avatar_url empty; no renderer authenticated external resource URL.

Native metadata socket:

- `native_socket_open({context,profileIntent,requestId,onEvent:Channel<NativeSocketNotice>})` →200 body `{handle:canonicalUUID}`.
- `native_socket_send({context,profileIntent,handle,action})` →204; action exact `{type:"ping",payload:{t:finiteSafeNonnegativeInteger}}` or `{type:"presence_idle",payload:{idle:boolean}}`. Other voice/content/SFU actions remain QualificationRequired until qualified native grants; no direct URL/headers/token.
- `native_socket_close({context,profileIntent,handle})` →204; handle native-owned and bound to context/profile/session.
- Channel notice shape `{context,handle,sequence:number,kind:"opened"|"message"|"closed",payload:unknown}`. Sequence positive monotone per native connection, bounded to JS-safe integer. Open payload null; message payload is the existing `{type,payload}` server-event envelope; closed payload `{code:constant}`. Recheck current context/connection/generation/sequence at delivery, never log opaque frames. Native renew controls consumed internally. Current allowlist server_info/system_update/pong/presence_snapshot/presence_update/channels_changed/member_joined/user_update/user_stats; other server events suppressed pending grant SDK.

Derivative actual `stores/chat.ts` uses `new NativeSocket()` via a factory import rather than its existing `new WebSocket(url)` line. Interface readyState/OPEN, onopen/onmessage/onclose, send(string),close() preserves current handlers/ping/reconnect generation. This is an explicit store bridge, no global WebSocket patch. Initial Channel events may precede open response: buffer bounded until validated handle, discard mismatched events. Close/cancel on teardown before retrying; do not swallow denied voice sends as if joined.

Root owns ui/** derivative. Distribution owns all Rust/Tauri files. Separate synthetic-media fixture commands are feature-gated research only, follow UIquality NativeMediaBridge contract and are not product permissions.

## Fixed metadata snapshots

`native_metadata_request({ context, profileIntent, requestId, resource })` admits ONLY resource strings `channels`, `members`, `read_state`, `health`, `legal`. Each maps to authenticated fixed GET `/api/native/v1/{channels,members,read-state,health,legal}` without query or caller headers/options. Reply status 200/body is the existing channel hierarchy, members array, five-field read-state array, health object or legal object. Typed native DTOs reject unknown response fields; final profile/window/session lease fencing applies. No protected content or marker write is admitted.

Pre-login public metadata: `native_public_metadata_request({context,profileIntent,requestId,resource:"legal"|"health"})` requires current verified selected profile, no authenticated session. Fixed GET `/api/native/v1/public/legal` or `/public/health`, no Authorization/Cookie/Origin/query/body. Reply carries only same closed Legal/Health DTOs and actual profile/window/generation fences. Existing authenticated enum unchanged. This is NEW backend increment after V2; old servers fail503 safely.

## Personal metadata actor increment

`native_personal_metadata_request({context,profileIntent,requestId,input})` uses the injected
actual App-document main window and existing cancellation/final reply fencing.
The closed input is `{operation:'user',user_id:canonicalUUID}` or
`{operation:'profile',display_name:<=24chars,bio:<=250chars}` or
`{operation:'locale',locale:'de'|'en'}` or
`{operation:'presence',presence:'online'|'away'|'dnd'|'focus'}` or
`{operation:'status',status_text:<=32chars}`. Unknown fields, NUL/oversize and
noncanonical UUIDs fail before dispatch. Success returns existing
`{context,status:200,body:User}` only. No arbitrary paths/headers/credentials.
Generic foreign-user/profile metadata replies never update native accepted
admin-role evidence; only the actual validated own `/auth/me` does.

Native auth scope is available to native SDK integrations only, never via IPC.
It binds actual grant family/instance/account/sequence, selected profile and
native window/session identities, with admission-time monotonic access cap.
Late older `/me` or refresh role snapshots cannot undo accepted downgrades.
Remembered login and protected content/media remain unavailable.


## Native profile admission comparator successor

The renderer captures `profileIntent` BEFORE invoking any profile-dependent command. Its value is the actual selected native profile UUID returned by successful Connect or the nullable current descriptor in `native_context`. It remains public comparison data. Every auth, metadata, socket, trust and chat IPC now requires this captured canonical UUID. Connect and disconnect accept the captured previous UUID, or null only while native has no ready selected profile.

Native validates the injected actual window plus expected UUID under the SAME lifecycle admission lock as request reservation, custody retirement and Connect invalidation. A delayed A login first admitted after B Connect is rejected before credential HTTP and before any Core retirement. Connect burns the old selector at admission and exposes its successor only after verified discovery and profile selection. A failed probe leaves no ready selector. A lost successful Connect acknowledgement is recovered only by obtaining the actual current native_context descriptor, then retrying with that captured UUID; Connect(null) cannot overwrite an already-ready native profile. Native context itself does not change merely because profile changes.

The four trust commands use `{context,profileIntent,channelId,onStatus}` for Begin, `{context,profileIntent,operationId}` for confirmation/cancel, and `{context,profileIntent}` for status. Chat uses `{context,profileIntent,channelId,clientEventId,body,onMessages}` for publish and the same without event/body for receive. Original native Control, operation and authenticated scope retain exact profile identity independently of this public comparator. Native first-root reservation runs inside the actual authenticated publication closure; no getter, native vault or other I/O runs under that closure.

`native_request_cancel` takes `{context,profileIntent:canonicalUUID|null,requestId}`. Null is needed for cancellation of first/unselected Connect and still selects only its originally owned opaque request UUID. Cancellation never chooses an arbitrary current operation. Replies and Channel events remain fenced by actual native incarnation, original operation scope, and renderer captured profile generation.
