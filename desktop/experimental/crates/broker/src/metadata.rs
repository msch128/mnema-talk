//! Closed metadata DTOs. No arbitrary server JSON can cross the native boundary.
use super::*;
use serde::{Deserialize, Serialize};
#[derive(Clone, Copy, Deserialize, Debug)]
#[serde(rename_all = "snake_case")]
pub enum MetadataResource {
    Channels,
    Members,
    ReadState,
    Health,
    Legal,
}
#[derive(Clone, Copy, Deserialize, Debug)]
#[serde(rename_all = "snake_case")]
pub enum PublicMetadataResource {
    Health,
    Legal,
}
pub struct MetadataDelivery {
    operation: Operation,
    value: serde_json::Value,
    authenticated: bool,
}
impl fmt::Debug for MetadataDelivery {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("MetadataDelivery(REDACTED)")
    }
}
impl MetadataResource {
    pub(crate) fn endpoint(self) -> Endpoint {
        match self {
            Self::Channels => Endpoint::Channels,
            Self::Members => Endpoint::Members,
            Self::ReadState => Endpoint::ReadState,
            Self::Health => Endpoint::Health,
            Self::Legal => Endpoint::Legal,
        }
    }
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Channel {
    id: String,
    number: i64,
    category_id: Option<String>,
    name: String,
    #[serde(rename = "type")]
    kind: String,
    topic: String,
    sort_order: i32,
    created_at: chrono::DateTime<Utc>,
    user_limit: i32,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Category {
    id: String,
    name: String,
    sort_order: i32,
    created_at: chrono::DateTime<Utc>,
    channels: Vec<Channel>,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Hierarchy {
    categories: Vec<Category>,
    uncategorized: Vec<Channel>,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct ReadState {
    channel_id: String,
    last_read_at: Option<chrono::DateTime<Utc>>,
    notify_level: String,
    unread_count: i64,
    mention_count: i64,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Health {
    status: String,
    version: String,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Legal {
    operator_name: String,
    operator_email: String,
    operator_country: String,
    project_notice: String,
    media_retention_days: i64,
    session_expiry_days: i64,
    stun_servers: Vec<String>,
    turn_servers: Vec<String>,
    update_check: bool,
    legal_version: String,
}
fn id(s: &str) -> bool {
    Uuid::parse_str(s).is_ok_and(|i| !i.is_nil() && i.hyphenated().to_string() == s)
}
fn text(s: &str, max: usize) -> bool {
    s.len() <= max && !s.chars().any(|c| c == '\0')
}
fn channel(c: &Channel) -> bool {
    id(&c.id)
        && c.number > 0
        && c.category_id.as_ref().is_none_or(|s| id(s))
        && !c.name.is_empty()
        && text(&c.name, 256)
        && text(&c.topic, 1024)
        && ["text", "voice"].contains(&c.kind.as_str())
        && (0..=999).contains(&c.user_limit)
}
fn safe<T: Serialize>(value: T) -> Result<serde_json::Value, Error> {
    serde_json::to_value(value).map_err(|_| Error::Internal)
}
fn decode(
    resource: MetadataResource,
    body: &crate::http::Body,
) -> Result<serde_json::Value, Error> {
    match resource {
        MetadataResource::Channels => {
            let v: Hierarchy = body.decode()?;
            if v.categories.len() > 4096
                || v.uncategorized.len() > 4096
                || !v.uncategorized.iter().all(channel)
                || v.categories.iter().any(|c| {
                    !id(&c.id)
                        || c.name.is_empty()
                        || !text(&c.name, 256)
                        || c.channels.len() > 4096
                        || !c.channels.iter().all(channel)
                })
            {
                return Err(Error::Protocol);
            }
            safe(v)
        }
        MetadataResource::Members => {
            let v: Vec<User> = body.decode()?;
            if v.len() > 4096 {
                return Err(Error::BodyLimit);
            }
            let mut out = Vec::with_capacity(v.len());
            for u in v {
                u.validate()?;
                out.push(super::client::user_value(u));
            }
            safe(out)
        }
        MetadataResource::ReadState => {
            let v: Vec<ReadState> = body.decode()?;
            if v.len() > 4096
                || v.iter().any(|r| {
                    !id(&r.channel_id)
                        || !["all", "mentions", "mute"].contains(&r.notify_level.as_str())
                        || r.unread_count < 0
                        || r.mention_count < 0
                })
            {
                return Err(Error::Protocol);
            }
            safe(v)
        }
        MetadataResource::Health => {
            let v: Health = body.decode()?;
            if v.status != "ok"
                || v.version.len() > 64
                || v.version.is_empty()
                || !v
                    .version
                    .bytes()
                    .all(|b| b.is_ascii_alphanumeric() || b".+_-".contains(&b))
            {
                return Err(Error::Protocol);
            }
            safe(v)
        }
        MetadataResource::Legal => {
            let v: Legal = body.decode()?;
            if !text(&v.operator_name, 1024)
                || !text(&v.operator_email, 1024)
                || !text(&v.operator_country, 256)
                || !text(&v.project_notice, 16384)
                || !text(&v.legal_version, 256)
                || v.media_retention_days < 0
                || v.session_expiry_days < 0
                || v.stun_servers.len() > 32
                || v.turn_servers.len() > 32
                || v.stun_servers
                    .iter()
                    .chain(&v.turn_servers)
                    .any(|s| !text(s, 2048))
            {
                return Err(Error::Protocol);
            }
            safe(v)
        }
    }
}
impl Broker {
    pub async fn public_metadata(
        &self,
        owner: WindowOwner,
        resource: PublicMetadataResource,
    ) -> Result<MetadataDelivery, Error> {
        let op = {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.main(self.inner.owner, owner)?;
            if s.active.is_none() {
                return Err(Error::NoProfile);
            }
            self.reserve(&mut s, owner, RequestPhase::Running)?
        };
        let (endpoint, shape) = match resource {
            PublicMetadataResource::Health => (Endpoint::PublicHealth, MetadataResource::Health),
            PublicMetadataResource::Legal => (Endpoint::PublicLegal, MetadataResource::Legal),
        };
        let body = self.send(&op, endpoint, None, None).await.map_err(|e| {
            if e == Error::Unauthorized {
                Error::QualificationRequired
            } else {
                e
            }
        })?;
        let value = decode(shape, &body)?;
        {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.current(self.inner.owner, &op)?;
            s.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Delivered;
        }
        Ok(MetadataDelivery {
            operation: op,
            value,
            authenticated: false,
        })
    }
    pub async fn metadata(
        &self,
        owner: WindowOwner,
        resource: MetadataResource,
    ) -> Result<MetadataDelivery, Error> {
        let op = self.prepare_me(owner)?;
        {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.current(self.inner.owner, &op)?;
            s.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Running;
        }
        let grant = op.grant.as_ref().ok_or(Error::Unauthorized)?;
        let body = match self
            .send(&op, resource.endpoint(), Some(&grant.access_token), None)
            .await
        {
            Err(Error::Unauthorized) => {
                let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
                s.current(self.inner.owner, &op)?;
                s.cancel_requests(None);
                if let Some(a) = s.active.as_mut() {
                    a.cancel.cancel();
                    a.status = Status::ReauthRequired;
                    a.session = None;
                    if let Some(f) = a.flight.take() {
                        f.finish(Err(Error::Unauthorized));
                    }
                }
                return Err(Error::Unauthorized);
            }
            r => r?,
        };
        let value = decode(resource, &body)?;
        {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.current(self.inner.owner, &op)?;
            s.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Delivered;
        }
        Ok(MetadataDelivery {
            operation: op,
            value,
            authenticated: true,
        })
    }
    pub fn commit_metadata(
        &self,
        owner: WindowOwner,
        delivery: MetadataDelivery,
    ) -> Result<serde_json::Value, Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        if owner != delivery.operation.window {
            return Err(Error::Denied);
        }
        s.current(self.inner.owner, &delivery.operation)?;
        if delivery.authenticated
            && delivery
                .operation
                .grant
                .as_ref()
                .is_none_or(|g| g.access_expires_at <= Utc::now())
        {
            return Err(Error::Expired);
        }
        Ok(delivery.value)
    }
}
