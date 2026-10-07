//! Public, bounded discovery only. This probe has no login or session transport.
use reqwest::blocking::Client;
use reqwest::redirect::Policy;
use serde::{Deserialize, Serialize};
use std::io::Read;
use std::time::Duration;
use url::Url;

const MAX_BYTES: u64 = 16 * 1024;
const REQUEST_TIMEOUT: Duration = Duration::from_secs(8);

#[derive(Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum DiscoveryError {
    InvalidAddress,
    HttpsRequired,
    Network,
    Redirect,
    Unavailable,
    InvalidDocument,
    Incompatible,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct Document {
    protocol: String,
    community_id: String,
    api_versions: Vec<u16>,
    e2ee_required: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct ServerCandidate {
    pub origin: String,
    pub community_id: String,
    pub api_version: u16,
    // Protocol compatibility is NOT authentication or peer/device verification.
    pub login_available: bool,
}

pub fn normalize_address(input: &str) -> Result<Url, DiscoveryError> {
    if input.chars().any(char::is_control) {
        return Err(DiscoveryError::InvalidAddress);
    }
    let input = input.trim();
    if input.is_empty() || input.len() > 2048 || input.chars().any(char::is_control) {
        return Err(DiscoveryError::InvalidAddress);
    }
    let raw = if input.contains("://") {
        input.to_owned()
    } else {
        format!("https://{input}")
    };
    // Reject syntax the URL parser would silently repair or normalize away.
    if raw.contains('\\') || raw.chars().any(char::is_whitespace) {
        return Err(DiscoveryError::InvalidAddress);
    }
    if raw
        .split_once("://")
        .and_then(|(_, rest)| rest.split_once('/'))
        .is_some_and(|(_, path)| !path.is_empty())
    {
        return Err(DiscoveryError::InvalidAddress);
    }
    let url = Url::parse(&raw).map_err(|_| DiscoveryError::InvalidAddress)?;
    if url.scheme() != "https" {
        return Err(DiscoveryError::HttpsRequired);
    }
    if url.host_str().is_none()
        || !url.username().is_empty()
        || url.password().is_some()
        || url.query().is_some()
        || url.fragment().is_some()
        || url.path() != "/"
        || raw
            .split_once("://")
            .is_some_and(|(_, authority)| authority.contains('@'))
    {
        return Err(DiscoveryError::InvalidAddress);
    }
    // Self-hosted LAN origins remain valid, subject to normal system TLS trust.
    Ok(url)
}

pub fn parse_document(origin: &Url, bytes: &[u8]) -> Result<ServerCandidate, DiscoveryError> {
    if bytes.len() as u64 > MAX_BYTES {
        return Err(DiscoveryError::InvalidDocument);
    }
    let doc: Document =
        serde_json::from_slice(bytes).map_err(|_| DiscoveryError::InvalidDocument)?;
    if doc.protocol != "mnema-desktop-discovery-v1"
        || doc.community_id.is_empty()
        || doc.community_id.len() > 128
        || !doc
            .community_id
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || b"-_".contains(&c))
        || doc.api_versions.len() > 8
    {
        return Err(DiscoveryError::InvalidDocument);
    }
    if !doc.api_versions.contains(&1) || !doc.e2ee_required {
        return Err(DiscoveryError::Incompatible);
    }
    Ok(ServerCandidate {
        origin: origin.origin().ascii_serialization(),
        community_id: doc.community_id,
        api_version: 1,
        // This is deliberately false until the native-session/E2EE gate passes.
        login_available: false,
    })
}

pub fn discover(input: &str) -> Result<ServerCandidate, DiscoveryError> {
    let origin = normalize_address(input)?;
    let endpoint = origin
        .join("/.well-known/mnema")
        .map_err(|_| DiscoveryError::InvalidAddress)?;
    let client = Client::builder()
        .redirect(Policy::none())
        .timeout(REQUEST_TIMEOUT)
        .connect_timeout(Duration::from_secs(4))
        .no_proxy()
        .https_only(true)
        .user_agent("Mnema-Desktop-Feasibility-Probe")
        .build()
        .map_err(|_| DiscoveryError::Network)?;
    request_document(&client, &origin, endpoint, REQUEST_TIMEOUT)
}

fn request_document(
    client: &Client,
    origin: &Url,
    endpoint: Url,
    timeout: Duration,
) -> Result<ServerCandidate, DiscoveryError> {
    let response = client
        .get(endpoint)
        // A blocking-client timeout alone resets on individual body reads.
        // The request timeout also sets the async transport's total deadline,
        // so a server cannot monopolize the probe by trickling response bytes.
        .timeout(timeout)
        .header("Accept", "application/json")
        .send()
        .map_err(|_| DiscoveryError::Network)?;
    let content_type = response
        .headers()
        .get(reqwest::header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    read_response(
        origin,
        response.status().as_u16(),
        content_type.to_owned(),
        response.content_length(),
        response,
    )
}

fn read_response(
    origin: &Url,
    status: u16,
    content_type: String,
    length: Option<u64>,
    reader: impl Read,
) -> Result<ServerCandidate, DiscoveryError> {
    if (300..400).contains(&status) {
        return Err(DiscoveryError::Redirect);
    }
    if !(200..300).contains(&status) {
        return Err(DiscoveryError::Unavailable);
    }
    if !content_type
        .split(';')
        .next()
        .map(str::trim)
        .is_some_and(|v| v.eq_ignore_ascii_case("application/json"))
        || length.is_some_and(|size| size > MAX_BYTES)
    {
        return Err(DiscoveryError::InvalidDocument);
    }
    let mut bytes = Vec::new();
    reader
        .take(MAX_BYTES + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| DiscoveryError::Network)?;
    parse_document(origin, &bytes)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn trickling_body_cannot_reset_the_total_request_deadline() {
        use std::io::Write;
        use std::net::TcpListener;
        use std::time::Instant;

        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let address = listener.local_addr().unwrap();
        let server = std::thread::spawn(move || {
            let (mut socket, _) = listener.accept().unwrap();
            socket
                .set_read_timeout(Some(Duration::from_secs(2)))
                .unwrap();
            let mut request = [0; 4096];
            assert!(socket.read(&mut request).unwrap() > 0);
            socket
                .write_all(b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 100\r\nConnection: close\r\n\r\n")
                .unwrap();
            for _ in 0..100 {
                if socket.write_all(b" ").is_err() {
                    break;
                }
                std::thread::sleep(Duration::from_millis(20));
            }
        });
        // HTTP is confined to this loopback fixture. Production discover()
        // still requires HTTPS and always constructs an HTTPS-only client.
        let origin = Url::parse(&format!("http://{address}/")).unwrap();
        let client = Client::builder()
            .no_proxy()
            .timeout(Duration::from_millis(100))
            .build()
            .unwrap();
        let started = Instant::now();
        assert_eq!(
            request_document(
                &client,
                &origin,
                origin.join("/.well-known/mnema").unwrap(),
                Duration::from_millis(180),
            ),
            Err(DiscoveryError::Network)
        );
        // Generous scheduling allowance; the complete 2-second trickle would
        // exceed it if only the per-read timeout were enforced.
        assert!(started.elapsed() < Duration::from_secs(1));
        server.join().unwrap();
    }

    fn valid() -> Vec<u8> {
        br#"{"protocol":"mnema-desktop-discovery-v1","community_id":"test-community","api_versions":[1],"e2ee_required":true}"#.to_vec()
    }

    #[test]
    fn canonicalizes_https_hosts_ports_and_idn() {
        for (input, expected) in [
            (" example.com ", "https://example.com/"),
            ("https://EXAMPLE.com:443", "https://example.com/"),
            ("example.com:8443", "https://example.com:8443/"),
            ("https://bücher.example", "https://xn--bcher-kva.example/"),
        ] {
            assert_eq!(normalize_address(input).unwrap().as_str(), expected);
        }
    }

    #[test]
    fn rejects_unsafe_or_ambiguous_addresses() {
        for input in [
            "",
            "http://example.com",
            "file:///etc/passwd",
            "https://user:pass@example.com",
            "https://@example.com",
            "https://example.com/?token=secret",
            "https://example.com/#x",
            "https://example.com/api",
            "https://example.com\\@evil.example",
            "https://exa mple.com",
            "https://example.com/\n",
            "https://example.com/../api",
        ] {
            assert!(normalize_address(input).is_err(), "accepted {input:?}");
        }
    }

    #[test]
    fn accepts_local_tls_origins_without_assuming_trust() {
        assert!(normalize_address("https://localhost:8443").is_ok());
    }

    #[test]
    fn discovery_never_enables_login_even_on_compatible_document() {
        let origin = normalize_address("example.com").unwrap();
        let result = parse_document(&origin, &valid()).unwrap();
        assert_eq!(result.origin, "https://example.com");
        assert!(!result.login_available);
    }

    #[test]
    fn rejects_spa_fallback_oversize_and_invalid_documents() {
        let origin = normalize_address("example.com").unwrap();
        for bytes in [
            b"<html>SPA</html>".to_vec(),
            vec![b' '; MAX_BYTES as usize + 1],
            b"null".to_vec(),
            b"[]".to_vec(),
            b"{}".to_vec(),
        ] {
            assert_eq!(
                parse_document(&origin, &bytes),
                Err(DiscoveryError::InvalidDocument)
            );
        }
        for (from, to) in [
            ("test-community", "bad/id"),
            ("test-community", ""),
            ("mnema-desktop-discovery-v1", "other-protocol"),
        ] {
            let bytes = String::from_utf8(valid()).unwrap().replace(from, to);
            assert_eq!(
                parse_document(&origin, bytes.as_bytes()),
                Err(DiscoveryError::InvalidDocument)
            );
        }
    }

    #[test]
    fn rejects_downgrade_and_protocol_incompatibility() {
        let origin = normalize_address("example.com").unwrap();
        for (from, to) in [("true", "false"), ("[1]", "[2]")] {
            let bytes = String::from_utf8(valid()).unwrap().replace(from, to);
            assert_eq!(
                parse_document(&origin, bytes.as_bytes()),
                Err(DiscoveryError::Incompatible)
            );
        }
    }

    #[test]
    fn http_policy_rejects_redirects_errors_and_spa_fallbacks() {
        let origin = normalize_address("example.com").unwrap();
        for status in [301, 302, 307, 308] {
            assert_eq!(
                read_response(
                    &origin,
                    status,
                    "application/json".into(),
                    None,
                    valid().as_slice()
                ),
                Err(DiscoveryError::Redirect)
            );
        }
        for status in [401, 403, 404, 429, 500, 503] {
            assert_eq!(
                read_response(
                    &origin,
                    status,
                    "application/json".into(),
                    None,
                    valid().as_slice()
                ),
                Err(DiscoveryError::Unavailable)
            );
        }
        for mime in ["text/html", "text/plain", "", "application/json-malicious"] {
            assert_eq!(
                read_response(&origin, 200, mime.into(), None, valid().as_slice()),
                Err(DiscoveryError::InvalidDocument)
            );
        }
        assert!(
            read_response(
                &origin,
                200,
                "Application/JSON; charset=utf-8".into(),
                None,
                valid().as_slice()
            )
            .is_ok()
        );
    }

    #[test]
    fn bounded_reader_rejects_oversize_and_io_failures() {
        let origin = normalize_address("example.com").unwrap();
        assert_eq!(
            read_response(
                &origin,
                200,
                "application/json".into(),
                Some(MAX_BYTES + 1),
                valid().as_slice()
            ),
            Err(DiscoveryError::InvalidDocument)
        );
        assert_eq!(
            read_response(
                &origin,
                200,
                "application/json".into(),
                None,
                std::io::repeat(b' ')
            ),
            Err(DiscoveryError::InvalidDocument)
        );
        struct Broken;
        impl Read for Broken {
            fn read(&mut self, _: &mut [u8]) -> std::io::Result<usize> {
                Err(std::io::Error::other("fixture"))
            }
        }
        assert_eq!(
            read_response(&origin, 200, "application/json".into(), None, Broken),
            Err(DiscoveryError::Network)
        );
    }
}
