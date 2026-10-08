use crate::{Error, secret::Secret};
use chrono::{DateTime, Utc};
use serde::{
    Deserialize,
    de::{Deserializer, Visitor},
};
use std::fmt;
use uuid::Uuid;
#[derive(Clone, Copy, PartialEq, Eq)]
pub(crate) struct Id(pub(crate) Uuid);
impl fmt::Debug for Id {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeId(REDACTED)")
    }
}
impl<'de> Deserialize<'de> for Id {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        struct IdVisitor;
        impl<'de> Visitor<'de> for IdVisitor {
            type Value = Id;
            fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
                f.write_str("canonical nonnil UUID")
            }
            fn visit_str<E: serde::de::Error>(self, value: &str) -> Result<Id, E> {
                let id =
                    Uuid::parse_str(value).map_err(|_| E::custom("invalid native identity"))?;
                if id.is_nil() || value.len() != 36 || id.hyphenated().to_string() != value {
                    return Err(E::custom("invalid native identity"));
                }
                Ok(Id(id))
            }
        }
        d.deserialize_str(IdVisitor)
    }
}
#[derive(Clone, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct User {
    pub(crate) id: Id,
    pub username: String,
    pub display_name: String,
    pub bio: String,
    pub role: String,
    #[serde(default)]
    pub avatar_url: String,
    pub status_text: String,
    #[serde(default)]
    pub presence: String,
    #[serde(default)]
    pub voice_seconds: i64,
    #[serde(default)]
    pub message_count: i64,
    pub locale: String,
    pub created_at: DateTime<Utc>,
}
impl fmt::Debug for User {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("User(REDACTED)")
    }
}
impl User {
    pub fn account_id(&self) -> Uuid {
        self.id.0
    }
    pub(crate) fn validate(&self) -> Result<(), Error> {
        if self.username.len() < 3
            || self.username.len() > 32
            || !self
                .username
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_.-".contains(&b))
            || self.display_name.chars().count() > 24
            || self.bio.chars().count() > 250
            || self.status_text.chars().count() > 32
            || !["admin", "user"].contains(&self.role.as_str())
            || !["", "de", "en"].contains(&self.locale.as_str())
            || !["", "online", "away", "dnd", "focus"].contains(&self.presence.as_str())
            || self.avatar_url.len() > 2048
            || self.voice_seconds < 0
            || self.message_count < 0
        {
            return Err(Error::Protocol);
        }
        Ok(())
    }
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct Grant {
    pub(crate) user: User,
    pub(crate) family_id: Id,
    pub(crate) client_instance_id: Id,
    pub(crate) refresh_sequence: u64,
    pub(crate) access_token: Secret,
    pub(crate) refresh_token: Secret,
    pub(crate) access_expires_at: DateTime<Utc>,
    pub(crate) family_expires_at: DateTime<Utc>,
}
impl fmt::Debug for Grant {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Grant(REDACTED)")
    }
}
impl Grant {
    pub(crate) fn validate(&self, instance: Uuid) -> Result<(), Error> {
        let now = Utc::now();
        self.user.validate()?;
        if self.client_instance_id.0 != instance
            || self.refresh_sequence > 65535
            || self.access_token.same(&self.refresh_token)
            || self.access_expires_at <= now
            || self.access_expires_at > now + chrono::Duration::minutes(5)
            || self.access_expires_at > self.family_expires_at
            || self.family_expires_at <= now
            || self.family_expires_at > now + chrono::Duration::hours(720)
        {
            return Err(Error::Protocol);
        }
        Ok(())
    }
    pub(crate) fn successor_of(&self, old: &Grant) -> Result<(), Error> {
        self.validate(old.client_instance_id.0)?;
        if self.user.id != old.user.id
            || self.family_id != old.family_id
            || old.refresh_sequence.checked_add(1) != Some(self.refresh_sequence)
            || self.family_expires_at != old.family_expires_at
            || self.refresh_token.same(&old.refresh_token)
            || self.access_token.same(&old.access_token)
        {
            return Err(Error::Protocol);
        }
        Ok(())
    }
}
