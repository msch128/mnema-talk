//! Closed server administration replies. Parsing never forwards raw server JSON.
use super::admin_metadata::AdminEndpoint as E;
use super::*;
use chrono::DateTime;
use serde::{Deserialize, Serialize};
use serde_json::Value;

fn id(s: &str) -> bool {
    Uuid::parse_str(s).is_ok_and(|i| !i.is_nil() && i.hyphenated().to_string() == s)
}
fn text(s: &str, max: usize) -> bool {
    !s.contains('\0') && s.chars().count() <= max
}
fn safe<T: Serialize>(v: T) -> Result<Value, Error> {
    serde_json::to_value(v).map_err(|_| Error::Internal)
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Invite {
    id: String,
    code: String,
    max_uses: Option<u16>,
    uses_count: u64,
    expires_at: Option<DateTime<Utc>>,
    created_at: DateTime<Utc>,
}
fn invite(v: &Invite) -> bool {
    id(&v.id)
        && (6..=64).contains(&v.code.len())
        && v.code
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"_-".contains(&b))
        && v.max_uses.is_none_or(|n| (1..=1000).contains(&n))
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct AdminUser {
    id: String,
    username: String,
    display_name: String,
    bio: String,
    role: String,
    #[serde(default)]
    avatar_url: String,
    status_text: String,
    #[serde(default)]
    presence: String,
    #[serde(default)]
    voice_seconds: i64,
    #[serde(default)]
    message_count: i64,
    locale: String,
    created_at: DateTime<Utc>,
    disabled: bool,
    last_seen_at: Option<DateTime<Utc>>,
}
fn admin_user(v: AdminUser) -> Result<Value, Error> {
    let disabled = v.disabled;
    let seen = v.last_seen_at;
    let mut raw = safe(v)?;
    let object = raw.as_object_mut().ok_or(Error::Protocol)?;
    object.remove("disabled");
    object.remove("last_seen_at");
    let user: User = serde_json::from_value(raw).map_err(|_| Error::Protocol)?;
    user.validate()?;
    let mut out = super::client::user_value(user);
    out["disabled"] = Value::Bool(disabled);
    out["last_seen_at"] = safe(seen)?;
    Ok(out)
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
    created_at: DateTime<Utc>,
    user_limit: u16,
}
fn channel(v: &Channel) -> bool {
    id(&v.id)
        && v.number > 0
        && v.category_id.as_ref().is_none_or(|s| id(s))
        && !v.name.trim().is_empty()
        && text(&v.name, 64)
        && text(&v.topic, 255)
        && ["text", "voice"].contains(&v.kind.as_str())
        && v.user_limit <= 999
        && (v.kind == "voice" || v.user_limit == 0)
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Category {
    id: String,
    name: String,
    sort_order: i32,
    channels: Vec<Channel>,
    created_at: DateTime<Utc>,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct RenamedCategory {
    id: String,
    name: String,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Update {
    check_enabled: bool,
    current_version: String,
    latest_version: String,
    update_available: bool,
    release_url: String,
    release_notes: String,
    published_at: Option<DateTime<Utc>>,
    checked_at: Option<DateTime<Utc>>,
    check_error: String,
    retry_at: Option<DateTime<Utc>>,
}
fn update(v: &Update) -> bool {
    text(&v.current_version, 128)
        && text(&v.latest_version, 128)
        && text(&v.release_url, 2048)
        && text(&v.release_notes, 131072)
        && text(&v.check_error, 4096)
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Version {
    current: String,
    revision: String,
    go_version: String,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Database {
    reachable: bool,
    latest_migration: String,
    applied_migrations: u32,
    pending_migrations: u32,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Storage {
    configured: bool,
    reachable: bool,
    files: u64,
    total_bytes: u64,
    attachment_bytes: u64,
    avatar_bytes: u64,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Voice {
    enabled: bool,
    rooms: u32,
    participants: u32,
    media_connections: u32,
    screen_shares: u32,
    cameras: u32,
    websocket_connections: u32,
    online_users: u32,
    turn_configured: bool,
    stun_configured: bool,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Runtime {
    started_at: DateTime<Utc>,
    uptime_seconds: u64,
    go_version: String,
    goroutines: u32,
    mem_alloc_bytes: u64,
    mem_sys_bytes: u64,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Health {
    database: Database,
    storage: Storage,
    voice: Voice,
    runtime: Runtime,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct SelfUpdate {
    configured: bool,
    image: String,
    reach: String,
    reach_reason: String,
    available: bool,
    next_allowed_at: Option<DateTime<Utc>>,
}
#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct System {
    version: Version,
    health: Health,
    update: Update,
    self_update: SelfUpdate,
}

pub(super) fn decode(endpoint: E, body: &crate::http::Body) -> Result<Value, Error> {
    if endpoint.status() == 204 {
        return Ok(Value::Null);
    }
    match endpoint {
        E::Invites => {
            let v: Vec<Invite> = body.decode()?;
            if v.len() > 4096 || !v.iter().all(invite) {
                return Err(Error::Protocol);
            }
            safe(v)
        }
        E::CreateInvite => {
            let v: Invite = body.decode()?;
            if !invite(&v) {
                return Err(Error::Protocol);
            }
            safe(v)
        }
        E::Users => {
            let v: Vec<AdminUser> = body.decode()?;
            if v.len() > 4096 {
                return Err(Error::BodyLimit);
            }
            let out = v
                .into_iter()
                .map(admin_user)
                .collect::<Result<Vec<_>, _>>()?;
            safe(out)
        }
        E::UserStatus(target) => {
            let v: User = body.decode()?;
            v.validate()?;
            if v.account_id() != target {
                return Err(Error::Protocol);
            }
            Ok(super::client::user_value(v))
        }
        E::CreateCategory => {
            let v: Category = body.decode()?;
            if !id(&v.id)
                || !text(&v.name, 64)
                || v.name.trim().is_empty()
                || v.channels.len() > 4096
                || !v.channels.iter().all(channel)
            {
                return Err(Error::Protocol);
            }
            safe(v)
        }
        E::RenameCategory(target) => {
            let v: RenamedCategory = body.decode()?;
            if v.id != target.hyphenated().to_string()
                || !text(&v.name, 64)
                || v.name.trim().is_empty()
            {
                return Err(Error::Protocol);
            }
            safe(v)
        }
        E::CreateChannel | E::DuplicateChannel(_) | E::UpdateChannel(_) => {
            let v: Channel = body.decode()?;
            if !channel(&v)
                || matches!(endpoint,E::UpdateChannel(target) if v.id!=target.hyphenated().to_string())
                || matches!(endpoint,E::DuplicateChannel(source) if v.id==source.hyphenated().to_string())
            {
                return Err(Error::Protocol);
            }
            safe(v)
        }
        E::SystemUpdate => {
            let v: Update = body.decode()?;
            if !update(&v) {
                return Err(Error::Protocol);
            }
            safe(v)
        }
        E::System => {
            let v: System = body.decode()?;
            if !text(&v.version.current, 128)
                || !text(&v.version.revision, 128)
                || !text(&v.version.go_version, 128)
                || !text(&v.health.database.latest_migration, 256)
                || !text(&v.health.runtime.go_version, 128)
                || !update(&v.update)
                || !text(&v.self_update.image, 2048)
                || !["yes", "no", "unknown"].contains(&v.self_update.reach.as_str())
                || ![
                    "follows",
                    "unset",
                    "local_build",
                    "digest_pinned",
                    "version_pinned",
                    "track_mismatch",
                    "custom_tag",
                ]
                .contains(&v.self_update.reach_reason.as_str())
            {
                return Err(Error::Protocol);
            }
            safe(v)
        }
        _ => Err(Error::Protocol),
    }
}
