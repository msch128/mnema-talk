//! Five fixed personal metadata operations. No renderer paths/headers/query or
//! content/voice authorization; every result uses existing native publication.
use super::*;
use serde::{Deserialize, Serialize};

#[derive(Deserialize)]
#[serde(tag = "operation", rename_all = "snake_case", deny_unknown_fields)]
pub enum PersonalMetadataOperation {
    User { user_id: String },
    Profile { display_name: String, bio: String },
    Locale { locale: MetadataLocale },
    Presence { presence: MetadataPresence },
    Status { status_text: String },
}
impl fmt::Debug for PersonalMetadataOperation {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("PersonalMetadataOperation(REDACTED)")
    }
}
#[derive(Clone, Copy, Deserialize, Serialize, Debug)]
#[serde(rename_all = "lowercase")]
pub enum MetadataLocale {
    De,
    En,
}
#[derive(Clone, Copy, Deserialize, Serialize, Debug)]
#[serde(rename_all = "lowercase")]
pub enum MetadataPresence {
    Online,
    Away,
    Dnd,
    Focus,
}

struct Prepared {
    endpoint: Endpoint,
    body: Option<Zeroizing<Vec<u8>>>,
    target: Option<Uuid>,
}
fn bounded(text: &str, max: usize) -> bool {
    text.chars().count() <= max && !text.contains('\0')
}
fn json<T: Serialize>(value: &T) -> Result<Option<Zeroizing<Vec<u8>>>, Error> {
    serde_json::to_vec(value)
        .map(|v| Some(Zeroizing::new(v)))
        .map_err(|_| Error::InvalidInput)
}
impl PersonalMetadataOperation {
    fn prepare(self) -> Result<Prepared, Error> {
        let (endpoint, body, target) = match self {
            Self::User { user_id } => {
                let id = Uuid::parse_str(&user_id).map_err(|_| Error::InvalidInput)?;
                if id.is_nil() || id.hyphenated().to_string() != user_id {
                    return Err(Error::InvalidInput);
                }
                (Endpoint::User(id), None, Some(id))
            }
            Self::Profile { display_name, bio } => {
                if !bounded(&display_name, 24) || !bounded(&bio, 250) {
                    return Err(Error::InvalidInput);
                }
                (
                    Endpoint::Profile,
                    json(&serde_json::json!({"display_name":display_name,"bio":bio}))?,
                    None,
                )
            }
            Self::Locale { locale } => (
                Endpoint::Locale,
                json(&serde_json::json!({"locale":locale}))?,
                None,
            ),
            Self::Presence { presence } => (
                Endpoint::Presence,
                json(&serde_json::json!({"presence":presence}))?,
                None,
            ),
            Self::Status { status_text } => {
                if !bounded(&status_text, 32) {
                    return Err(Error::InvalidInput);
                }
                (
                    Endpoint::UserStatus,
                    json(&serde_json::json!({"status_text":status_text}))?,
                    None,
                )
            }
        };
        Ok(Prepared {
            endpoint,
            body,
            target,
        })
    }
}
impl Broker {
    pub async fn personal_metadata(
        &self,
        owner: WindowOwner,
        input: PersonalMetadataOperation,
    ) -> Result<MeDelivery, Error> {
        let prepared = input.prepare()?;
        let op = self.prepare_me(owner)?;
        {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.current(self.inner.owner, &op)?;
            s.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Running;
        }
        let grant = op.grant.as_ref().ok_or(Error::Unauthorized)?;
        if grant.access_expires_at <= Utc::now() {
            return Err(Error::Expired);
        }
        let body = match self
            .send(
                &op,
                prepared.endpoint,
                Some(&grant.access_token),
                prepared.body,
            )
            .await
        {
            Err(Error::Unauthorized) => {
                let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
                // A delayed old401 cannot clear a newly selected/login session.
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
        let user: User = body.decode()?;
        user.validate()?;
        if user.account_id() != prepared.target.unwrap_or(grant.user.account_id()) {
            return Err(Error::Protocol);
        }
        {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.current(self.inner.owner, &op)?;
            s.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Delivered;
        }
        Ok(MeDelivery {
            operation: op,
            value: user,
        })
    }
}
