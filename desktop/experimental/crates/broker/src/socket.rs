//! Native-only WSS: fixed destination/header, strict metadata publication,
//! renewal controls never serialized to the renderer; content remains gated.
use super::*;
use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use std::{
    collections::BTreeMap,
    time::{Duration, Instant},
};
use tokio::net::TcpStream;
use tokio_tungstenite::{
    MaybeTlsStream, WebSocketStream,
    tungstenite::{
        Message, client::IntoClientRequest, http::HeaderValue, protocol::WebSocketConfig,
    },
};

#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct VersionPayload {
    pub version: String,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PongPayload {
    pub t: u64,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Presence {
    Online,
    Away,
    Dnd,
    Focus,
    Offline,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct PresencePayload {
    pub user_id: String,
    pub status: Presence,
}
#[derive(Clone)]
pub struct MetadataUser(User);
impl Serialize for MetadataUser {
    fn serialize<S: serde::Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        super::client::user_value(self.0.clone()).serialize(s)
    }
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct DisabledPayload {
    id: String,
    disabled: bool,
}
#[derive(Clone, Serialize)]
#[serde(untagged)]
pub enum UserChange {
    Disabled(DisabledPayload),
    Profile(Box<MetadataUser>),
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct StatsPayload {
    user_id: String,
    voice_seconds: i64,
}
#[derive(Clone, Serialize)]
#[serde(tag = "type", content = "payload")]
pub enum MetadataEvent {
    #[serde(rename = "server_info")]
    ServerInfo(VersionPayload),
    #[serde(rename = "system_update")]
    SystemUpdate(VersionPayload),
    #[serde(rename = "pong")]
    Pong(PongPayload),
    #[serde(rename = "presence_update")]
    PresenceUpdate(PresencePayload),
    #[serde(rename = "presence_snapshot")]
    PresenceSnapshot(BTreeMap<String, Presence>),
    #[serde(rename = "channels_changed")]
    ChannelsChanged(()),
    #[serde(rename = "member_joined")]
    MemberJoined(Box<MetadataUser>),
    #[serde(rename = "user_update")]
    UserUpdate(UserChange),
    #[serde(rename = "user_stats")]
    UserStats(StatsPayload),
}
impl fmt::Debug for MetadataEvent {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("MetadataEvent(REDACTED)")
    }
}
#[derive(Clone, Copy, Deserialize)]
#[serde(tag = "type", content = "payload", deny_unknown_fields)]
pub enum NativeSocketAction {
    #[serde(rename = "ping")]
    Ping { t: u64 },
    #[serde(rename = "presence_idle")]
    PresenceIdle { idle: bool },
}
impl fmt::Debug for NativeSocketAction {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeSocketAction(REDACTED)")
    }
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Envelope {
    #[serde(rename = "type")]
    kind: String,
    payload: serde_json::Value,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Renewed {
    access_expires_at: chrono::DateTime<Utc>,
}
struct Pending {
    grant: Arc<Grant>,
    deadline: Instant,
}
pub struct NativeSocket {
    op: Operation,
    stream: WebSocketStream<MaybeTlsStream<TcpStream>>,
    lease: Arc<Grant>,
    pending: Option<Pending>,
    #[cfg(feature = "synthetic-media-fixture")]
    fixture_message: Option<mnema_private_synthetic_media_policy::MediaMessage>,
}
impl fmt::Debug for NativeSocket {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeSocket(REDACTED)")
    }
}
fn canonical_id(value: &str) -> bool {
    Uuid::parse_str(value).is_ok_and(|id| !id.is_nil() && id.hyphenated().to_string() == value)
}
fn metadata(envelope: Envelope) -> Result<Option<MetadataEvent>, Error> {
    fn decode<T: serde::de::DeserializeOwned>(value: serde_json::Value) -> Result<T, Error> {
        serde_json::from_value(value).map_err(|_| Error::Protocol)
    }
    Ok(match envelope.kind.as_str() {
        "server_info" | "system_update" => {
            let p: VersionPayload = decode(envelope.payload)?;
            if p.version.is_empty()
                || p.version.len() > 64
                || !p
                    .version
                    .bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b".+_-".contains(&b))
            {
                return Err(Error::Protocol);
            }
            Some(if envelope.kind == "server_info" {
                MetadataEvent::ServerInfo(p)
            } else {
                MetadataEvent::SystemUpdate(p)
            })
        }
        "pong" => {
            let p: PongPayload = decode(envelope.payload)?;
            if p.t > 9_007_199_254_740_991 {
                return Err(Error::Protocol);
            }
            Some(MetadataEvent::Pong(p))
        }
        "presence_update" => {
            let p: PresencePayload = decode(envelope.payload)?;
            if !canonical_id(&p.user_id) {
                return Err(Error::Protocol);
            }
            Some(MetadataEvent::PresenceUpdate(p))
        }
        "presence_snapshot" => {
            let p: BTreeMap<String, Presence> = match envelope.payload {
                serde_json::Value::Array(a) => {
                    if a.len() > 4096 {
                        return Err(Error::BodyLimit);
                    }
                    let mut out = BTreeMap::new();
                    for id in a {
                        let id = id.as_str().ok_or(Error::Protocol)?;
                        if !canonical_id(id) {
                            return Err(Error::Protocol);
                        }
                        if out.insert(id.to_owned(), Presence::Online).is_some() {
                            return Err(Error::Protocol);
                        }
                    }
                    out
                }
                v => decode(v)?,
            };
            if p.len() > 4096 || p.keys().any(|k| !canonical_id(k)) {
                return Err(Error::Protocol);
            }
            Some(MetadataEvent::PresenceSnapshot(p))
        }
        "channels_changed" => {
            if !envelope.payload.is_null() {
                return Err(Error::Protocol);
            }
            Some(MetadataEvent::ChannelsChanged(()))
        }
        "member_joined" => {
            let u: User = decode(envelope.payload)?;
            u.validate()?;
            Some(MetadataEvent::MemberJoined(Box::new(MetadataUser(u))))
        }
        "user_update" => {
            #[derive(Deserialize)]
            #[serde(untagged)]
            enum Update {
                Disabled(DisabledPayload),
                Profile(Box<User>),
            }
            let change = match decode::<Update>(envelope.payload)? {
                Update::Disabled(p) => {
                    if !canonical_id(&p.id) {
                        return Err(Error::Protocol);
                    }
                    UserChange::Disabled(p)
                }
                Update::Profile(u) => {
                    u.validate()?;
                    UserChange::Profile(Box::new(MetadataUser(*u)))
                }
            };
            Some(MetadataEvent::UserUpdate(change))
        }
        "user_stats" => {
            let p: StatsPayload = decode(envelope.payload)?;
            if !canonical_id(&p.user_id) || p.voice_seconds < 0 {
                return Err(Error::Protocol);
            }
            Some(MetadataEvent::UserStats(p))
        }
        // Chat, read state, voice/SFU, content and all future controls cannot cross
        // this metadata bridge without a separately reviewed CryptoCore grant.
        _ => None,
    })
}
impl Broker {
    pub async fn open_native_socket(&self, owner: WindowOwner) -> Result<NativeSocket, Error> {
        let (op, grant) = {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.main(self.inner.owner, owner)?;
            let active = s.active.as_ref().ok_or(Error::NoProfile)?;
            if active.status != Status::Authenticated {
                return Err(Error::ReauthRequired);
            }
            let grant = active
                .session
                .as_ref()
                .ok_or(Error::ReauthRequired)?
                .grant
                .clone();
            if grant.access_expires_at <= Utc::now() {
                return Err(Error::Expired);
            }
            let op = self.reserve(&mut s, owner, RequestPhase::Running)?;
            (op, grant)
        };
        let mut url = op
            .origin
            .join("/api/native/v1/ws")
            .map_err(|_| Error::InvalidInput)?;
        url.set_scheme("wss").map_err(|_| Error::InvalidInput)?;
        let mut request = url
            .as_str()
            .into_client_request()
            .map_err(|_| Error::Protocol)?;
        let mut auth = Zeroizing::new(String::from("Bearer "));
        auth.push_str(grant.access_token.wire().as_str());
        let mut header = HeaderValue::from_bytes(auth.as_bytes()).map_err(|_| Error::Protocol)?;
        header.set_sensitive(true);
        request.headers_mut().insert("Authorization", header);
        let config = WebSocketConfig::default()
            .read_buffer_size(4096)
            .write_buffer_size(0)
            .max_write_buffer_size(8192)
            .max_message_size(Some(65536))
            .max_frame_size(Some(65536));
        #[cfg(feature = "synthetic-media-fixture")]
        let config = config.max_write_buffer_size(131072);
        let connector = {
            let mut builder = native_tls::TlsConnector::builder();
            builder.min_protocol_version(Some(native_tls::Protocol::Tlsv12));
            #[cfg(any(test, feature = "synthetic-transport-fixture"))]
            if let Some(root) = &self.root {
                builder.add_root_certificate(
                    native_tls::Certificate::from_pem(root).map_err(|_| Error::Internal)?,
                );
            }
            Some(tokio_tungstenite::Connector::NativeTls(
                builder.build().map_err(|_| Error::Network)?,
            ))
        };
        let result = tokio::select! {biased;_=op.cancelled()=>Err(Error::Cancelled),r=tokio::time::timeout(Duration::from_secs(8),tokio_tungstenite::connect_async_tls_with_config(request,Some(config),false,connector))=>r.map_err(|_|Error::Network)?.map_err(|_|Error::Network)};
        let (stream, response) = result?;
        self.verify(&op)?;
        if response.headers().contains_key("Set-Cookie")
            || response.headers().contains_key("Sec-WebSocket-Protocol")
            || response.headers().contains_key("Sec-WebSocket-Extensions")
        {
            return Err(Error::Protocol);
        }
        Ok(NativeSocket {
            op,
            stream,
            lease: grant,
            pending: None,
            #[cfg(feature = "synthetic-media-fixture")]
            fixture_message: None,
        })
    }
    fn socket_grant(&self, socket: &NativeSocket) -> Result<Arc<Grant>, Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.main(self.inner.owner, socket.op.window)?;
        if socket.op.cancel.is_cancelled()
            || socket.op.window_cancel.is_cancelled()
            || !s.profiles.is_current(socket.op.profile)
            || !s.requests.contains_key(&socket.op.id)
        {
            return Err(Error::Stale);
        }
        let active = s.active.as_ref().ok_or(Error::Stale)?;
        if !matches!(active.status, Status::Authenticated | Status::Rotating) {
            return Err(Error::ReauthRequired);
        }
        let grant = active
            .session
            .as_ref()
            .ok_or(Error::ReauthRequired)?
            .grant
            .clone();
        if grant.family_id != socket.lease.family_id
            || grant.client_instance_id != socket.lease.client_instance_id
            || grant.user.id != socket.lease.user.id
            || grant.refresh_sequence < socket.lease.refresh_sequence
        {
            return Err(Error::Stale);
        }
        if socket.lease.access_expires_at <= Utc::now() || grant.family_expires_at <= Utc::now() {
            return Err(Error::Expired);
        }
        Ok(grant)
    }
    pub fn commit_socket_event(
        &self,
        owner: WindowOwner,
        socket: &NativeSocket,
        event: MetadataEvent,
    ) -> Result<MetadataEvent, Error> {
        if owner != socket.op.window {
            return Err(Error::Denied);
        }
        let grant = self.socket_grant(socket)?;
        if socket.pending.is_some() || grant.refresh_sequence != socket.lease.refresh_sequence {
            return Err(Error::Stale);
        }
        Ok(event)
    }
    pub async fn socket_send(
        &self,
        owner: WindowOwner,
        socket: &mut NativeSocket,
        action: NativeSocketAction,
    ) -> Result<(), Error> {
        if owner != socket.op.window {
            return Err(Error::Denied);
        }
        let grant = self.socket_grant(socket)?;
        if socket.pending.is_some() || grant.refresh_sequence != socket.lease.refresh_sequence {
            return Err(Error::Busy);
        }
        let bytes = match action {
            NativeSocketAction::Ping { t } => {
                if t > 9_007_199_254_740_991 {
                    return Err(Error::InvalidInput);
                }
                serde_json::json!({"type":"ping","payload":{"t":t}})
            }
            NativeSocketAction::PresenceIdle { idle } => {
                serde_json::json!({"type":"presence_idle","payload":{"idle":idle}})
            }
        };
        self.socket_write(socket, Message::Text(bytes.to_string().into()))
            .await
    }
    async fn socket_write(&self, socket: &mut NativeSocket, message: Message) -> Result<(), Error> {
        self.socket_grant(socket)?;
        tokio::select! {biased;_=socket.op.cancel.cancelled()=>Err(Error::Cancelled),_=socket.op.window_cancel.cancelled()=>Err(Error::Stale),r=tokio::time::timeout(Duration::from_secs(4),socket.stream.send(message))=>r.map_err(|_|Error::Network)?.map_err(|_|Error::Network)}?;
        self.socket_grant(socket)?;
        Ok(())
    }
    pub async fn socket_poll(
        &self,
        socket: &mut NativeSocket,
    ) -> Result<Option<MetadataEvent>, Error> {
        let grant = self.socket_grant(socket)?;
        if socket
            .pending
            .as_ref()
            .is_some_and(|p| Instant::now() >= p.deadline)
        {
            return Err(Error::Expired);
        }
        if socket.pending.is_none() && grant.refresh_sequence != socket.lease.refresh_sequence {
            if socket.lease.refresh_sequence.checked_add(1) != Some(grant.refresh_sequence) {
                return Err(Error::Stale);
            }
            let mut control = Zeroizing::new(String::from(
                "{\"type\":\"native_access_renew\",\"payload\":{\"access_token\":\"",
            ));
            control.push_str(grant.access_token.wire().as_str());
            control.push_str("\"}}");
            self.socket_write(socket, Message::Text(control.as_str().to_owned().into()))
                .await?;
            socket.pending = Some(Pending {
                grant: grant.clone(),
                deadline: Instant::now() + Duration::from_secs(4),
            });
        }
        let next = tokio::select! {biased;_=socket.op.cancel.cancelled()=>return Err(Error::Cancelled),_=socket.op.window_cancel.cancelled()=>return Err(Error::Stale),_=tokio::time::sleep(Duration::from_millis(100))=>return Ok(None),r=socket.stream.next()=>r};
        let message = next.ok_or(Error::Network)?.map_err(|_| Error::Network)?;
        let current = self.socket_grant(socket)?;
        match message {
            Message::Ping(bytes) => {
                self.socket_write(socket, Message::Pong(bytes)).await?;
            }
            Message::Pong(_) => {}
            Message::Close(_) => return Err(Error::Network),
            Message::Text(text) => {
                if text.len() > 65536 {
                    return Err(Error::BodyLimit);
                }
                let e: Envelope = serde_json::from_str(&text).map_err(|_| Error::Protocol)?;
                if e.kind == "native_access_renewed" {
                    if text.len() > 1024 {
                        return Err(Error::BodyLimit);
                    }
                    #[derive(Deserialize)]
                    #[serde(deny_unknown_fields)]
                    struct Ack {
                        #[serde(rename = "type")]
                        kind: String,
                        payload: Renewed,
                    }
                    let ack: Ack = serde_json::from_str(&text).map_err(|_| Error::Protocol)?;
                    if ack.kind != "native_access_renewed" {
                        return Err(Error::Protocol);
                    }
                    let ack = ack.payload;
                    let pending = socket.pending.take().ok_or(Error::Protocol)?;
                    if Instant::now() >= pending.deadline
                        || current.refresh_sequence != pending.grant.refresh_sequence
                        || current.family_id != pending.grant.family_id
                        || !current.access_token.same(&pending.grant.access_token)
                        || ack.access_expires_at != pending.grant.access_expires_at
                    {
                        return Err(Error::Stale);
                    }
                    socket.lease = pending.grant;
                } else if socket.pending.is_none()
                    && current.refresh_sequence == socket.lease.refresh_sequence
                {
                    #[cfg(feature = "synthetic-media-fixture")]
                    if self.fixture_scope.is_some() {
                        socket.fixture_message =
                            mnema_private_synthetic_media_policy::decode_message(text.as_bytes())
                                .map_err(|_| Error::Protocol)?;
                    }
                    if let Some(event) = metadata(e)? {
                        return self
                            .commit_socket_event(socket.op.window, socket, event)
                            .map(Some);
                    }
                }
            }
            _ => return Err(Error::Protocol),
        }
        Ok(None)
    }
    pub(crate) fn socket_needs_refresh(&self, socket: &NativeSocket) -> Result<bool, Error> {
        let current = self.socket_grant(socket)?;
        Ok(socket.pending.is_none()
            && current.refresh_sequence == socket.lease.refresh_sequence
            && (socket.lease.access_expires_at - Utc::now()).num_seconds() <= 60)
    }
    pub async fn socket_next(&self, socket: &mut NativeSocket) -> Result<MetadataEvent, Error> {
        loop {
            if let Some(event) = self.socket_poll(socket).await? {
                return Ok(event);
            }
        }
    }
}

#[cfg(feature = "synthetic-media-fixture")]
impl Broker {
    pub fn native_fixture_binding(
        &self,
        owner: WindowOwner,
        socket: &NativeSocket,
        window_context: Uuid,
        profile_context: Uuid,
        socket_generation: u64,
    ) -> Result<(mnema_private_synthetic_media_policy::NativeBinding, Instant), Error> {
        use mnema_private_synthetic_media_policy::{Id, NativeBinding};
        if self.fixture_scope.is_none()
            || owner != socket.op.window
            || window_context.is_nil()
            || profile_context.is_nil()
            || socket_generation == 0
            || socket_generation > 9_007_199_254_740_991
            || socket.pending.is_some()
        {
            return Err(Error::Denied);
        }
        let g = self.socket_grant(socket)?;
        if g.refresh_sequence != socket.lease.refresh_sequence {
            return Err(Error::Stale);
        }
        let make = |i| Id::native(i).map_err(|_| Error::Protocol);
        let remaining = (g.access_expires_at - Utc::now())
            .to_std()
            .map_err(|_| Error::Expired)?
            .min(Duration::from_secs(300));
        let binding = NativeBinding {
            window_context: make(window_context)?,
            profile_context: make(profile_context)?,
            account_id: make(g.user.account_id())?,
            family_id: make(g.family_id.0)?,
            client_instance_id: make(g.client_instance_id.0)?,
            socket_generation,
            refresh_sequence: g.refresh_sequence,
        };
        Ok((binding, Instant::now() + remaining))
    }
    pub async fn native_fixture_poll(
        &self,
        socket: &mut NativeSocket,
    ) -> Result<Option<mnema_private_synthetic_media_policy::MediaMessage>, Error> {
        if self.fixture_scope.is_none() {
            return Err(Error::Denied);
        }
        let _ = self.socket_poll(socket).await?;
        if socket.pending.is_some() {
            return Ok(None);
        }
        self.socket_grant(socket)?;
        Ok(socket.fixture_message.take())
    }
    pub async fn native_fixture_send(
        &self,
        owner: WindowOwner,
        socket: &mut NativeSocket,
        cap: &mnema_private_synthetic_media_policy::FixtureCapability,
        binding: &mnema_private_synthetic_media_policy::NativeBinding,
        action: mnema_private_synthetic_media_policy::Action,
    ) -> Result<(), Error> {
        let actual = self
            .native_fixture_binding(
                owner,
                socket,
                binding.window_context.uuid(),
                binding.profile_context.uuid(),
                binding.socket_generation,
            )?
            .0;
        let bound = cap.bound();
        cap.authorize(
            Some(&actual),
            &bound.handle,
            bound.generation,
            Instant::now(),
            &action,
        )
        .map_err(|_| Error::Denied)?;
        if actual != *binding {
            return Err(Error::Stale);
        }
        let text = serde_json::to_string(&action).map_err(|_| Error::Protocol)?;
        if text.len() > 65536 {
            return Err(Error::BodyLimit);
        }
        self.socket_write(socket, Message::Text(text.into())).await
    }
}
