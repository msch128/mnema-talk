//! Closed administration commands. The server's current native principal is
//! authoritative; cached login roles never authorize an admin request.
use super::*;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

#[derive(Deserialize)]
#[serde(tag = "operation", rename_all = "snake_case", deny_unknown_fields)]
pub enum AdminMetadataOperation {
    Invites {},
    CreateInvite {
        code: String,
        max_uses: Option<u16>,
        expires_in_hours: Option<u16>,
    },
    DeleteInvite {
        id: String,
    },
    Users {},
    DisableUser {
        id: String,
    },
    EnableUser {
        id: String,
    },
    RevokeUserSessions {
        id: String,
    },
    KickUser {
        id: String,
    },
    UserStatus {
        id: String,
        status_text: String,
    },
    CreateCategory {
        name: String,
        sort_order: i32,
    },
    DeleteCategory {
        id: String,
    },
    RenameCategory {
        id: String,
        name: String,
    },
    CreateChannel {
        category_id: Option<String>,
        name: String,
        kind: AdminChannelKind,
        topic: String,
        sort_order: i32,
    },
    DeleteChannel {
        id: String,
    },
    UpdateChannel {
        id: String,
        name: Option<String>,
        topic: Option<String>,
        user_limit: Option<u16>,
    },
    DuplicateChannel {
        id: String,
    },
    Layout {
        categories: Vec<AdminCategoryOrder>,
        channels: Vec<AdminChannelPlacement>,
    },
    System {},
    SystemUpdate {},
}
impl fmt::Debug for AdminMetadataOperation {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("AdminMetadataOperation(REDACTED)")
    }
}
#[derive(Clone, Copy, Deserialize, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum AdminChannelKind {
    Text,
    Voice,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct AdminCategoryOrder {
    pub id: String,
    pub sort_order: i32,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct AdminChannelPlacement {
    pub id: String,
    pub category_id: Option<String>,
    pub sort_order: i32,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum AdminEndpoint {
    Invites,
    CreateInvite,
    DeleteInvite(Uuid),
    Users,
    DisableUser(Uuid),
    EnableUser(Uuid),
    RevokeUserSessions(Uuid),
    KickUser(Uuid),
    UserStatus(Uuid),
    CreateCategory,
    DeleteCategory(Uuid),
    RenameCategory(Uuid),
    CreateChannel,
    DeleteChannel(Uuid),
    UpdateChannel(Uuid),
    DuplicateChannel(Uuid),
    Layout,
    System,
    SystemUpdate,
}
impl AdminEndpoint {
    pub(crate) fn route(self) -> (reqwest::Method, String) {
        use reqwest::Method;
        let (method, tail) = match self {
            Self::Invites => (Method::GET, "invites".into()),
            Self::CreateInvite => (Method::POST, "invites".into()),
            Self::DeleteInvite(id) => (Method::DELETE, format!("invites/{id}")),
            Self::Users => (Method::GET, "users".into()),
            Self::DisableUser(id) => (Method::POST, format!("users/{id}/disable")),
            Self::EnableUser(id) => (Method::POST, format!("users/{id}/enable")),
            Self::RevokeUserSessions(id) => (Method::POST, format!("users/{id}/sessions/revoke")),
            Self::KickUser(id) => (Method::POST, format!("users/{id}/kick")),
            Self::UserStatus(id) => (Method::PUT, format!("users/{id}/status")),
            Self::CreateCategory => (Method::POST, "categories".into()),
            Self::DeleteCategory(id) => (Method::DELETE, format!("categories/{id}")),
            Self::RenameCategory(id) => (Method::PATCH, format!("categories/{id}")),
            Self::CreateChannel => (Method::POST, "channels".into()),
            Self::DeleteChannel(id) => (Method::DELETE, format!("channels/{id}")),
            Self::UpdateChannel(id) => (Method::PATCH, format!("channels/{id}")),
            Self::DuplicateChannel(id) => (Method::POST, format!("channels/{id}/duplicate")),
            Self::Layout => (Method::PUT, "layout".into()),
            Self::System => (Method::GET, "system".into()),
            Self::SystemUpdate => (Method::GET, "system/update".into()),
        };
        (method, format!("/api/native/v1/admin/{tail}"))
    }
    pub(crate) fn status(self) -> u16 {
        match self {
            Self::CreateInvite
            | Self::CreateCategory
            | Self::CreateChannel
            | Self::DuplicateChannel(_) => 201,
            Self::DeleteInvite(_)
            | Self::DisableUser(_)
            | Self::EnableUser(_)
            | Self::RevokeUserSessions(_)
            | Self::KickUser(_)
            | Self::DeleteCategory(_)
            | Self::DeleteChannel(_)
            | Self::Layout => 204,
            _ => 200,
        }
    }
}
fn id(s: &str) -> Result<Uuid, Error> {
    Uuid::parse_str(s)
        .ok()
        .filter(|i| !i.is_nil() && i.hyphenated().to_string() == s)
        .ok_or(Error::InvalidInput)
}
fn text(s: &str, max: usize, required: bool) -> bool {
    !s.contains('\0') && s.trim().chars().count() <= max && (!required || !s.trim().is_empty())
}
fn json_body(v: Value) -> Result<Option<Zeroizing<Vec<u8>>>, Error> {
    let bytes = serde_json::to_vec(&v).map_err(|_| Error::InvalidInput)?;
    if bytes.len() > 65536 {
        return Err(Error::BodyLimit);
    }
    Ok(Some(Zeroizing::new(bytes)))
}
struct Prepared {
    endpoint: AdminEndpoint,
    body: Option<Zeroizing<Vec<u8>>>,
}
impl AdminMetadataOperation {
    fn prepare(self) -> Result<Prepared, Error> {
        use AdminEndpoint as E;
        let (endpoint, body) = match self {
            Self::Invites {} => (E::Invites, None),
            Self::CreateInvite {
                code,
                max_uses,
                expires_in_hours,
            } => {
                let code = code.trim();
                if (!code.is_empty()
                    && (!(6..=64).contains(&code.len())
                        || !code
                            .bytes()
                            .all(|b| b.is_ascii_alphanumeric() || b"_-".contains(&b))))
                    || max_uses.is_some_and(|v| !(1..=1000).contains(&v))
                    || expires_in_hours.is_some_and(|v| !(1..=8760).contains(&v))
                {
                    return Err(Error::InvalidInput);
                }
                (
                    E::CreateInvite,
                    json_body(
                        json!({"code":code,"max_uses":max_uses,"expires_in_hours":expires_in_hours}),
                    )?,
                )
            }
            Self::DeleteInvite { id: s } => (E::DeleteInvite(id(&s)?), None),
            Self::Users {} => (E::Users, None),
            Self::DisableUser { id: s } => (E::DisableUser(id(&s)?), None),
            Self::EnableUser { id: s } => (E::EnableUser(id(&s)?), None),
            Self::RevokeUserSessions { id: s } => (E::RevokeUserSessions(id(&s)?), None),
            Self::KickUser { id: s } => (E::KickUser(id(&s)?), None),
            Self::UserStatus { id: s, status_text } => {
                if !text(&status_text, 32, false) {
                    return Err(Error::InvalidInput);
                }
                (
                    E::UserStatus(id(&s)?),
                    json_body(json!({"status_text":status_text}))?,
                )
            }
            Self::CreateCategory { name, sort_order } => {
                if !text(&name, 64, true) {
                    return Err(Error::InvalidInput);
                }
                (
                    E::CreateCategory,
                    json_body(json!({"name":name,"sort_order":sort_order}))?,
                )
            }
            Self::DeleteCategory { id: s } => (E::DeleteCategory(id(&s)?), None),
            Self::RenameCategory { id: s, name } => {
                if !text(&name, 64, true) {
                    return Err(Error::InvalidInput);
                }
                (E::RenameCategory(id(&s)?), json_body(json!({"name":name}))?)
            }
            Self::CreateChannel {
                category_id,
                name,
                kind,
                topic,
                sort_order,
            } => {
                if let Some(s) = &category_id {
                    id(s)?;
                }
                if !text(&name, 64, true) || !text(&topic, 255, false) {
                    return Err(Error::InvalidInput);
                }
                (
                    E::CreateChannel,
                    json_body(
                        json!({"category_id":category_id,"name":name,"type":kind,"topic":topic,"sort_order":sort_order}),
                    )?,
                )
            }
            Self::DeleteChannel { id: s } => (E::DeleteChannel(id(&s)?), None),
            Self::UpdateChannel {
                id: s,
                name,
                topic,
                user_limit,
            } => {
                if name.as_ref().is_some_and(|s| !text(s, 64, true))
                    || topic.as_ref().is_some_and(|s| !text(s, 255, false))
                    || user_limit.is_some_and(|v| v > 999)
                {
                    return Err(Error::InvalidInput);
                }
                (
                    E::UpdateChannel(id(&s)?),
                    json_body(json!({"name":name,"topic":topic,"user_limit":user_limit}))?,
                )
            }
            Self::DuplicateChannel { id: s } => (E::DuplicateChannel(id(&s)?), None),
            Self::Layout {
                categories,
                channels,
            } => {
                if categories.len().saturating_add(channels.len()) > 500 {
                    return Err(Error::InvalidInput);
                }
                let mut seen = std::collections::HashSet::new();
                for c in &categories {
                    if !seen.insert(id(&c.id)?) {
                        return Err(Error::InvalidInput);
                    }
                }
                seen.clear();
                for c in &channels {
                    if !seen.insert(id(&c.id)?) {
                        return Err(Error::InvalidInput);
                    }
                    if let Some(s) = &c.category_id {
                        id(s)?;
                    }
                }
                (
                    E::Layout,
                    json_body(json!({"categories":categories,"channels":channels}))?,
                )
            }
            Self::System {} => (E::System, None),
            Self::SystemUpdate {} => (E::SystemUpdate, None),
        };
        Ok(Prepared { endpoint, body })
    }
}

pub struct AdminDelivery {
    operation: Operation,
    status: u16,
    value: Value,
}
impl fmt::Debug for AdminDelivery {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("AdminDelivery(REDACTED)")
    }
}
pub struct AdminReply {
    pub status: u16,
    pub body: Value,
}
impl fmt::Debug for AdminReply {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("AdminReply(REDACTED)")
    }
}
impl Broker {
    pub async fn admin_metadata(
        &self,
        owner: WindowOwner,
        input: AdminMetadataOperation,
    ) -> Result<AdminDelivery, Error> {
        let Prepared { endpoint, body } = input.prepare()?;
        let op = self.prepare_me(owner)?;
        {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.current(self.inner.owner, &op)?;
            if op.auth_revision
                != s.active
                    .as_ref()
                    .and_then(|active| active.session.as_ref())
                    .map(|session| session.native_identity)
            {
                return Err(Error::Stale);
            }
            s.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Running;
        }
        let grant = op.grant.as_ref().ok_or(Error::Unauthorized)?;
        if grant.access_expires_at <= Utc::now() {
            return Err(Error::Expired);
        }
        let body = match self
            .send(
                &op,
                Endpoint::Admin(endpoint),
                Some(&grant.access_token),
                body,
            )
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
            result => result?,
        };
        if grant.access_expires_at <= Utc::now() {
            return Err(Error::Expired);
        }
        let status = body.status();
        let value = if status >= 400 {
            let code = match status {
                400 => "INVALID_INPUT",
                403 => "FORBIDDEN",
                404 => "NOT_FOUND",
                409 => "CONFLICT",
                429 => "RATE_LIMITED",
                500 => "INTERNAL_ERROR",
                503 => "UNAVAILABLE",
                _ => return Err(Error::Protocol),
            };
            json!({"error":{"code":code,"message":"Native administration request rejected"}})
        } else {
            super::admin_wire::decode(endpoint, &body)?
        };
        {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.current(self.inner.owner, &op)?;
            if op.auth_revision
                != s.active
                    .as_ref()
                    .and_then(|active| active.session.as_ref())
                    .map(|session| session.native_identity)
            {
                return Err(Error::Stale);
            }
            s.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Delivered;
        }
        Ok(AdminDelivery {
            operation: op,
            status,
            value,
        })
    }
    pub fn commit_admin(
        &self,
        owner: WindowOwner,
        delivery: AdminDelivery,
    ) -> Result<AdminReply, Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        if owner != delivery.operation.window {
            return Err(Error::Denied);
        }
        s.current(self.inner.owner, &delivery.operation)?;
        if delivery.operation.auth_revision
            != s.active
                .as_ref()
                .and_then(|active| active.session.as_ref())
                .map(|session| session.native_identity)
        {
            return Err(Error::Stale);
        }
        if delivery
            .operation
            .grant
            .as_ref()
            .is_none_or(|g| g.access_expires_at <= Utc::now())
        {
            return Err(Error::Expired);
        }
        Ok(AdminReply {
            status: delivery.status,
            body: delivery.value,
        })
    }
}
