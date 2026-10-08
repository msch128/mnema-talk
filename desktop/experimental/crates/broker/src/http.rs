use crate::{Error, secret::Secret};
use reqwest::{
    Client, Method, Url,
    header::{self, HeaderValue},
};
use std::{fmt, time::Duration};
use zeroize::Zeroizing;
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Endpoint {
    Login,
    Refresh,
    Me,
    Logout,
    Password,
    Register,
    Discovery,
    Channels,
    Members,
    ReadState,
    Health,
    Legal,
    PublicHealth,
    PublicLegal,
    User(uuid::Uuid),
    Profile,
    Locale,
    Presence,
    UserStatus,
    Admin(crate::broker::admin_metadata::AdminEndpoint),
    OpaquePost(uuid::Uuid),
    OpaquePage(uuid::Uuid, i64),
}
impl Endpoint {
    fn route(self) -> (Method, String) {
        let (method, path) = match self {
            Self::Admin(endpoint) => return endpoint.route(),
            Self::Login => (Method::POST, "/api/native/v1/auth/login"),
            Self::Refresh => (Method::POST, "/api/native/v1/auth/refresh"),
            Self::Me => (Method::GET, "/api/native/v1/auth/me"),
            Self::Logout => (Method::POST, "/api/native/v1/auth/logout"),
            Self::Password => (Method::PUT, "/api/native/v1/auth/password"),
            Self::Register => (Method::POST, "/api/native/v1/auth/register"),
            Self::Channels => (Method::GET, "/api/native/v1/channels"),
            Self::Members => (Method::GET, "/api/native/v1/members"),
            Self::ReadState => (Method::GET, "/api/native/v1/read-state"),
            Self::Health => (Method::GET, "/api/native/v1/health"),
            Self::Legal => (Method::GET, "/api/native/v1/legal"),
            Self::PublicHealth => (Method::GET, "/api/native/v1/public/health"),
            Self::PublicLegal => (Method::GET, "/api/native/v1/public/legal"),
            Self::Discovery => (Method::GET, "/.well-known/mnema"),
            Self::User(id) => return (Method::GET, format!("/api/native/v1/users/{id}")),
            Self::Profile => (Method::PUT, "/api/native/v1/users/me/profile"),
            Self::Locale => (Method::PUT, "/api/native/v1/users/me/locale"),
            Self::Presence => (Method::PUT, "/api/native/v1/users/me/presence"),
            Self::UserStatus => (Method::PUT, "/api/native/v1/users/me/status"),
            Self::OpaquePost(id) => {
                return (
                    Method::POST,
                    format!("/api/native/v1/channels/{id}/ciphertext-events"),
                );
            }
            Self::OpaquePage(id, _) => {
                return (
                    Method::GET,
                    format!("/api/native/v1/channels/{id}/ciphertext-events"),
                );
            }
        };
        (method, path.to_owned())
    }
    fn needs_access(self) -> bool {
        matches!(
            self,
            Self::Me
                | Self::Logout
                | Self::Password
                | Self::Channels
                | Self::Members
                | Self::ReadState
                | Self::Health
                | Self::Legal
                | Self::User(_)
                | Self::Profile
                | Self::Locale
                | Self::Presence
                | Self::UserStatus
                | Self::Admin(_)
                | Self::OpaquePost(_)
                | Self::OpaquePage(_, _)
        )
    }
}
#[derive(Clone)]
pub(crate) struct Http(Client);
impl fmt::Debug for Http {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Http(REDACTED)")
    }
}
pub(crate) struct Body(Zeroizing<Vec<u8>>, u16);
impl fmt::Debug for Body {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Body(REDACTED)")
    }
}
impl Body {
    pub(crate) fn status(&self) -> u16 {
        self.1
    }
    pub(crate) fn bytes(&self) -> &[u8] {
        &self.0
    }

    pub(crate) fn decode<'de, T: serde::Deserialize<'de>>(&'de self) -> Result<T, Error> {
        serde_json::from_slice(&self.0).map_err(|_| Error::Protocol)
    }
}
const AUTH_BODY_MAX: usize = 16 * 1024;
impl Http {
    fn builder() -> reqwest::ClientBuilder {
        Client::builder()
            .use_native_tls()
            .https_only(true)
            .redirect(reqwest::redirect::Policy::none())
            .retry(reqwest::retry::never())
            .no_proxy()
            .referer(false)
            .no_gzip()
            .no_brotli()
            .no_deflate()
            .no_zstd()
            .timeout(Duration::from_secs(8))
            .connect_timeout(Duration::from_secs(4))
            .pool_max_idle_per_host(2)
            .connection_verbose(false)
            .user_agent("Mnema-Native-Broker-Private-Research")
    }
    pub(crate) fn new() -> Result<Self, Error> {
        Self::builder()
            .build()
            .map(Self)
            .map_err(|_| Error::Network)
    }
    #[cfg(any(test, feature = "synthetic-transport-fixture"))]
    pub(crate) fn local_fixture(root: &[u8]) -> Result<Self, Error> {
        let cert = reqwest::Certificate::from_pem(root).map_err(|_| Error::Internal)?;
        Self::builder()
            .add_root_certificate(cert)
            .build()
            .map(Self)
            .map_err(|_| Error::Network)
    }
    pub(crate) async fn request(
        &self,
        endpoint: Endpoint,
        origin: &Url,
        access: Option<&Secret>,
        body: Option<Zeroizing<Vec<u8>>>,
    ) -> Result<Body, Error> {
        if origin.scheme() != "https"
            || origin.host_str().is_none()
            || origin.path() != "/"
            || !origin.username().is_empty()
            || origin.password().is_some()
            || origin.query().is_some()
            || origin.fragment().is_some()
            || endpoint.needs_access() != access.is_some()
            || matches!(endpoint, Endpoint::User(id) if id.is_nil())
            || matches!(endpoint, Endpoint::OpaquePost(id) | Endpoint::OpaquePage(id, _) if id.is_nil())
            || matches!(endpoint, Endpoint::OpaquePage(_, after) if after < 0)
        {
            return Err(Error::Denied);
        }
        let max_body = if matches!(
            endpoint,
            Endpoint::Channels
                | Endpoint::Members
                | Endpoint::ReadState
                | Endpoint::Health
                | Endpoint::Legal
                | Endpoint::PublicHealth
                | Endpoint::PublicLegal
                | Endpoint::OpaquePage(_, _)
                | Endpoint::Admin(_)
                | Endpoint::OpaquePost(_)
        ) {
            1024 * 1024
        } else {
            AUTH_BODY_MAX
        };
        let (method, path) = endpoint.route();
        let mut url = origin.join(&path).map_err(|_| Error::InvalidInput)?;
        let query = match endpoint {
            Endpoint::OpaquePage(_, after) => Some(format!("after={after}&limit=10")),
            _ => None,
        };
        url.set_query(query.as_deref());
        if url.origin() != origin.origin()
            || url.path() != path
            || url.query() != query.as_deref()
            || url.fragment().is_some()
        {
            return Err(Error::Denied);
        }
        let mut request = self
            .0
            .request(method, url.clone())
            .header(header::ACCEPT, "application/json")
            .header(header::ACCEPT_ENCODING, "identity");
        if let Some(secret) = access {
            let mut value = Zeroizing::new(String::from("Bearer "));
            value.push_str(secret.wire().as_str());
            let mut header =
                HeaderValue::from_bytes(value.as_bytes()).map_err(|_| Error::Protocol)?;
            header.set_sensitive(true);
            request = request.header(header::AUTHORIZATION, header)
        }
        if let Some(bytes) = body {
            let request_max = if matches!(endpoint, Endpoint::OpaquePost(_)) {
                96 * 1024
            } else if matches!(endpoint, Endpoint::Admin(_)) {
                65536
            } else {
                AUTH_BODY_MAX
            };
            if bytes.len() > request_max {
                return Err(Error::BodyLimit);
            }
            request = request
                .header(header::CONTENT_TYPE, "application/json")
                .body(bytes.to_vec());
        }
        let mut response = request.send().await.map_err(|_| Error::Network)?;
        if response.status().is_redirection() {
            return Err(Error::Redirect);
        }
        if response.url() != &url
            || response.headers().contains_key(header::SET_COOKIE)
            || response
                .headers()
                .get(header::CONTENT_ENCODING)
                .is_some_and(|h| h.as_bytes() != b"identity")
        {
            return Err(Error::Protocol);
        }
        if response.status() == reqwest::StatusCode::UNAUTHORIZED {
            return Err(Error::Unauthorized);
        }
        if response
            .headers()
            .get(header::CACHE_CONTROL)
            .and_then(|h| h.to_str().ok())
            != Some("no-store")
        {
            return Err(Error::Protocol);
        }
        if let Endpoint::Admin(admin) = endpoint {
            let status = response.status().as_u16();
            if [400, 403, 404, 409, 429, 500, 503].contains(&status) {
                // Do not expose arbitrary server error bodies or auto-retry writes.
                return Ok(Body(Zeroizing::new(Vec::new()), status));
            }
            if status != admin.status() {
                return Err(Error::Protocol);
            }
            if status == 204 {
                if response
                    .headers()
                    .get(header::CONTENT_LENGTH)
                    .is_some_and(|h| h.as_bytes() != b"0")
                    || response.headers().contains_key(header::TRANSFER_ENCODING)
                {
                    return Err(Error::Protocol);
                }
                return Ok(Body(Zeroizing::new(Vec::new()), 204));
            }
        }
        if endpoint == Endpoint::Logout {
            if response.status() != reqwest::StatusCode::NO_CONTENT {
                return Err(Error::Protocol);
            }
            return Ok(Body(Zeroizing::new(Vec::new()), 204));
        }
        if endpoint == Endpoint::Register && response.status() != reqwest::StatusCode::CREATED {
            return Err(Error::Protocol);
        }
        if !matches!(endpoint, Endpoint::Admin(_))
            && response.status() != reqwest::StatusCode::OK
            && !(matches!(endpoint, Endpoint::OpaquePost(_) | Endpoint::Register)
                && response.status() == reqwest::StatusCode::CREATED)
        {
            return Err(Error::Protocol);
        }
        let content_type = response
            .headers()
            .get(header::CONTENT_TYPE)
            .and_then(|h| h.to_str().ok())
            .unwrap_or("");
        if content_type.split(';').next().unwrap_or("").trim() != "application/json"
            || response
                .headers()
                .get(header::CACHE_CONTROL)
                .and_then(|h| h.to_str().ok())
                != Some("no-store")
        {
            return Err(Error::Protocol);
        }
        if response
            .content_length()
            .is_some_and(|length| length > max_body as u64)
        {
            return Err(Error::BodyLimit);
        }
        let mut bytes = Zeroizing::new(Vec::new());
        while let Some(chunk) = response.chunk().await.map_err(|_| Error::Network)? {
            if chunk.len() > max_body.saturating_sub(bytes.len()) {
                return Err(Error::BodyLimit);
            }
            bytes.extend_from_slice(&chunk)
        }
        Ok(Body(bytes, response.status().as_u16()))
    }
}
