use mnema_private_native_client_broker::{
    Error, MetadataResource, NativeClient, NativeReply, NativeRequest, NativeSocketAction,
    NativeSocketNotice, NativeSocketObserver, NativeWindowLease, Password,
    PersonalMetadataOperation, PublicMetadataResource,
};
#[cfg(feature = "synthetic-media-fixture")]
use mnema_private_synthetic_media_actor::{MediaActor, NativeMediaWindow};
use raw_window_handle::{HasWindowHandle, RawWindowHandle};
use serde::Serialize;
use std::sync::{Arc, Mutex};
use tauri::ipc::Channel;
use tauri::{Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder};
use uuid::Uuid;
mod local_route;
#[cfg(all(feature = "native-crypto", feature = "synthetic-media-fixture"))]
compile_error!("Native Core host and unrelated synthetic-media entry cannot share an executable");
#[cfg(feature = "native-crypto")]
mod crypto_host;
#[cfg(feature = "native-crypto")]
use crypto_host::{
    native_chat_mutate, native_chat_publish, native_chat_receive, native_chat_snapshot,
    native_trust_begin_first_root, native_trust_cancel, native_trust_read_status,
    native_trust_request_confirmation,
};
#[cfg(feature = "synthetic-media-fixture")]
mod media_fixture;
#[cfg(feature = "synthetic-media-fixture")]
use media_fixture::{
    native_fixture_media_close, native_fixture_media_open, native_fixture_media_send,
};
struct Registration {
    identity: NativeIdentity,
    lease: NativeWindowLease,
    document: NativeDocument,
    #[cfg(feature = "synthetic-media-fixture")]
    media_window: Option<NativeMediaWindow>,
}
#[derive(Clone, Copy, PartialEq, Eq)]
enum NativeDocument {
    App,
    #[cfg(feature = "synthetic-media-fixture")]
    SyntheticMedia,
}
impl NativeDocument {
    // Called only from native setup or PageLoadStarted with the native load URL.
    // History changes do not load a new document and cannot mint this authority.
    fn at_native_load(url: &url::Url, fixture_enabled: bool) -> Result<Self, Error> {
        if !bundled(url) {
            return Err(Error::Denied);
        }
        #[cfg(feature = "synthetic-media-fixture")]
        if url.path() == "/native-media.html" {
            return if fixture_enabled {
                Ok(Self::SyntheticMedia)
            } else {
                Err(Error::QualificationRequired)
            };
        }
        let _ = fixture_enabled;
        if local_route::app_path(url.path()) {
            Ok(Self::App)
        } else {
            Err(Error::Denied)
        }
    }
}
#[derive(Clone, Copy, PartialEq, Eq)]
enum NativeIdentity {
    Win32(isize),
    AppKit(usize),
    Xlib(std::os::raw::c_ulong),
    Xcb(u32),
    Wayland(usize),
}
struct RuntimeState {
    client: NativeClient,
    registry: Mutex<Option<Registration>>,
    #[cfg(feature = "native-crypto")]
    core: crypto_host::Host,
    #[cfg(feature = "synthetic-media-fixture")]
    media: Option<MediaActor>,
}
impl RuntimeState {
    fn registration(
        &self,
        window: &WebviewWindow,
        native_load_url: &url::Url,
    ) -> Result<Registration, Error> {
        let fixture_enabled = {
            #[cfg(feature = "synthetic-media-fixture")]
            {
                self.media.is_some()
            }
            #[cfg(not(feature = "synthetic-media-fixture"))]
            {
                false
            }
        };
        let document = NativeDocument::at_native_load(native_load_url, fixture_enabled)?;
        let native = identity(window)?;
        let lease = self.client.attach_main_window()?;
        #[cfg(feature = "synthetic-media-fixture")]
        let media_window = self
            .media
            .as_ref()
            .filter(|_| document == NativeDocument::SyntheticMedia)
            .map(|m| m.native_attach_window())
            .transpose()?;
        Ok(Registration {
            identity: native,
            lease,
            document,
            #[cfg(feature = "synthetic-media-fixture")]
            media_window,
        })
    }
    fn detach(&self, entry: &Registration) {
        #[cfg(feature = "native-crypto")]
        self.core.retire_context(&entry.lease.context_nonce());
        let _ = self.client.detach_main_window(&entry.lease);
        #[cfg(feature = "synthetic-media-fixture")]
        if let (Some(actor), Some(window)) = (&self.media, &entry.media_window) {
            let _ = actor.native_detach_window(window);
        }
    }
}
fn identity(window: &WebviewWindow) -> Result<NativeIdentity, Error> {
    match window.window_handle().map_err(|_| Error::Denied)?.as_raw() {
        RawWindowHandle::Win32(h) => Ok(NativeIdentity::Win32(h.hwnd.get())),
        RawWindowHandle::AppKit(h) => Ok(NativeIdentity::AppKit(h.ns_view.as_ptr() as usize)),
        RawWindowHandle::Xlib(h) => Ok(NativeIdentity::Xlib(h.window)),
        RawWindowHandle::Xcb(h) => Ok(NativeIdentity::Xcb(h.window.get())),
        RawWindowHandle::Wayland(h) => Ok(NativeIdentity::Wayland(h.surface.as_ptr() as usize)),
        _ => Err(Error::Denied),
    }
}
fn bundled(url: &url::Url) -> bool {
    ((url.scheme() == "tauri" && url.host_str() == Some("localhost"))
        || (["http", "https"].contains(&url.scheme()) && url.host_str() == Some("tauri.localhost")))
        && (local_route::app_path(url.path()) || {
            #[cfg(feature = "synthetic-media-fixture")]
            {
                url.path() == "/native-media.html"
            }
            #[cfg(not(feature = "synthetic-media-fixture"))]
            {
                false
            }
        })
        && url.port().is_none()
        && url.query().is_none()
        && url.username().is_empty()
        && url.password().is_none()
}
fn require(
    window: &WebviewWindow,
    state: &RuntimeState,
    context: Option<&str>,
) -> Result<NativeWindowLease, Error> {
    require_document(window, state, context, NativeDocument::App)
}
fn require_document(
    window: &WebviewWindow,
    state: &RuntimeState,
    context: Option<&str>,
    document: NativeDocument,
) -> Result<NativeWindowLease, Error> {
    if window.label() != "main" || !bundled(&window.url().map_err(|_| Error::Denied)?) {
        return Err(Error::Denied);
    }
    let native = identity(window)?;
    let registry = state.registry.lock().map_err(|_| Error::Internal)?;
    let entry = registry.as_ref().ok_or(Error::Stale)?;
    if entry.document != document
        || entry.identity != native
        || context.is_some_and(|c| c != entry.lease.context_nonce())
    {
        return Err(Error::Stale);
    }
    state.client.check_window(&entry.lease)?;
    Ok(entry.lease.clone())
}
fn canonical(value: &str) -> Result<Uuid, Error> {
    let id = Uuid::parse_str(value).map_err(|_| Error::InvalidInput)?;
    if id.is_nil() || id.hyphenated().to_string() != value {
        return Err(Error::InvalidInput);
    }
    Ok(id)
}
fn rejected(context: String, error: Error) -> NativeReply {
    let (status, code, message) = match error {
        Error::Stale => (
            499,
            "NATIVE_STALE",
            "Diese Anforderung ist nicht mehr aktuell.",
        ),
        Error::Cancelled => (
            499,
            "NATIVE_CANCELLED",
            "Diese Anforderung wurde abgebrochen.",
        ),
        Error::NoProfile
        | Error::Unauthorized
        | Error::ReauthRequired
        | Error::Expired
        | Error::UncertainRotation => {
            (401, "UNAUTHORIZED", "Eine neue Anmeldung ist erforderlich.")
        }
        Error::Busy => (429, "RATE_LIMITED", "Bitte versuche es später erneut."),
        Error::Denied => (403, "FORBIDDEN", "Diese Aktion ist nicht freigegeben."),
        Error::InvalidInput => (400, "INVALID_INPUT", "Die Eingabe ist ungültig."),
        _ => (
            503,
            "UNAVAILABLE",
            "Diese Funktion ist in dieser Entwicklungsprüfung nicht verfügbar.",
        ),
    };
    NativeReply {
        context,
        status,
        body: serde_json::json!({"error":{"code":code,"message":message}}),
    }
}
#[allow(clippy::too_many_arguments)]
async fn invoke(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: Option<String>,
    authentication_intent: Option<String>,
    request_id: String,
    request: NativeRequest,
) -> NativeReply {
    let result = async {
        let lease = require(&window, &state, Some(&context))?;
        let expected = profile_intent.as_deref().map(canonical).transpose()?;
        let authentication = authentication_intent
            .as_deref()
            .map(canonical)
            .transpose()?;
        let retires = matches!(
            request,
            NativeRequest::Connect { .. }
                | NativeRequest::Login { .. }
                | NativeRequest::Password { .. }
                | NativeRequest::Register { .. }
                | NativeRequest::Logout
        );
        let id = canonical(&request_id)?;
        let publication = state
            .client
            .request_with_authentication_admission(
                &lease,
                id,
                expected,
                authentication,
                request,
                || {
                    #[cfg(feature = "native-crypto")]
                    if retires {
                        if let Some(profile) = expected {
                            state.core.retire_profile(&lease.context_nonce(), profile)
                        } else {
                            state.core.retire_context(&lease.context_nonce())
                        }
                    }
                    #[cfg(not(feature = "native-crypto"))]
                    let _ = retires;
                },
            )
            .await?;
        let current = require(&window, &state, Some(&context))?;
        let reply = state.client.commit(&current, publication)?;
        require(&window, &state, Some(&context))?;
        Ok::<_, Error>(reply)
    }
    .await;
    match result {
        Ok(r) => r,
        Err(e) => rejected(context, e),
    }
}
#[derive(Serialize)]
struct Context {
    context: String,
    profile_intent: Option<Uuid>,
    authentication_intent: Option<Uuid>,
    content_authorization: &'static str,
    remembered_login: bool,
}
#[tauri::command]
fn native_context(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
) -> Result<Context, &'static str> {
    let lease = require(&window, &state, None).map_err(|_| "native context unavailable")?;
    let (profile_intent, authentication_intent) = state
        .client
        .selected_intents(&lease)
        .map_err(|_| "native selectors unavailable")?;
    Ok(Context {
        context: lease.context_nonce(),
        profile_intent,
        authentication_intent,
        content_authorization: "unavailable",
        remembered_login: false,
    })
}
#[tauri::command]
async fn native_connect(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: Option<String>,
    authentication_intent: Option<String>,
    request_id: String,
    address: String,
) -> Result<NativeReply, ()> {
    Ok(invoke(
        window,
        state,
        context,
        profile_intent,
        authentication_intent,
        request_id,
        NativeRequest::Connect { address },
    )
    .await)
}
#[tauri::command]
#[allow(clippy::too_many_arguments)]
async fn native_auth_login(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    request_id: String,
    username: String,
    password: String,
) -> Result<NativeReply, ()> {
    let password = match Password::from_native_input(password) {
        Ok(p) => p,
        Err(e) => return Ok(rejected(context, e)),
    };
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        authentication_intent,
        request_id,
        NativeRequest::Login { username, password },
    )
    .await)
}
#[tauri::command]
#[allow(clippy::too_many_arguments)]
async fn native_auth_password(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: String,
    request_id: String,
    current_password: String,
    new_password: String,
) -> Result<NativeReply, ()> {
    // Convert both credential arguments at entry, before any await/admission.
    let (current_password, new_password) = match (
        Password::from_native_input(current_password),
        Password::from_native_input(new_password),
    ) {
        (Ok(current), Ok(new)) => (current, new),
        (Err(error), _) | (_, Err(error)) => return Ok(rejected(context, error)),
    };
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        Some(authentication_intent),
        request_id,
        NativeRequest::Password {
            current_password,
            new_password,
        },
    )
    .await)
}
#[tauri::command]
#[allow(clippy::too_many_arguments)]
async fn native_auth_register(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    request_id: String,
    username: String,
    display_name: String,
    password: String,
    invite_code: String,
) -> Result<NativeReply, ()> {
    let invite_code = zeroize::Zeroizing::new(invite_code);
    let password = match Password::from_native_input(password) {
        Ok(value) => value,
        Err(error) => return Ok(rejected(context, error)),
    };
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        authentication_intent,
        request_id,
        NativeRequest::Register {
            username,
            display_name,
            password,
            invite_code,
        },
    )
    .await)
}
#[tauri::command]
#[allow(clippy::too_many_arguments)]
async fn native_admin_request(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: String,
    request_id: String,
    input: mnema_private_native_client_broker::AdminMetadataOperation,
) -> Result<NativeReply, ()> {
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        Some(authentication_intent),
        request_id,
        NativeRequest::AdminMetadata { operation: input },
    )
    .await)
}
#[tauri::command]
async fn native_auth_me(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    request_id: String,
) -> Result<NativeReply, ()> {
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        authentication_intent,
        request_id,
        NativeRequest::Me,
    )
    .await)
}
#[tauri::command]
async fn native_auth_refresh(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    request_id: String,
) -> Result<NativeReply, ()> {
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        authentication_intent,
        request_id,
        NativeRequest::Refresh,
    )
    .await)
}
#[tauri::command]
async fn native_auth_logout(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    request_id: String,
) -> Result<NativeReply, ()> {
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        authentication_intent,
        request_id,
        NativeRequest::Logout,
    )
    .await)
}
#[tauri::command]
fn native_disconnect(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: Option<String>,
    authentication_intent: Option<String>,
) -> NativeReply {
    match require(&window, &state, Some(&context)).and_then(|lease| {
        let expected = profile_intent.as_deref().map(canonical).transpose()?;
        state.client.disconnect_with_authentication_admission(
            &lease,
            expected,
            authentication_intent
                .as_deref()
                .map(canonical)
                .transpose()?,
            || {
                #[cfg(feature = "native-crypto")]
                if let Some(profile) = expected {
                    state.core.retire_profile(&lease.context_nonce(), profile)
                } else {
                    state.core.retire_context(&lease.context_nonce())
                }
            },
        )
    }) {
        Ok(()) => NativeReply {
            context,
            status: 204,
            body: serde_json::Value::Null,
        },
        Err(e) => rejected(context, e),
    }
}
#[tauri::command]
fn native_request_cancel(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: Option<String>,
    authentication_intent: Option<String>,
    request_id: String,
) -> NativeReply {
    let r = require(&window, &state, Some(&context)).and_then(|lease| {
        state.client.cancel_request_with_intents(
            &lease,
            profile_intent.as_deref().map(canonical).transpose()?,
            authentication_intent
                .as_deref()
                .map(canonical)
                .transpose()?,
            canonical(&request_id)?,
        )
    });
    match r {
        Ok(()) => NativeReply {
            context,
            status: 204,
            body: serde_json::Value::Null,
        },
        Err(e) => rejected(context, e),
    }
}
#[tauri::command]
async fn native_metadata_request(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    request_id: String,
    resource: MetadataResource,
) -> Result<NativeReply, ()> {
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        authentication_intent,
        request_id,
        NativeRequest::Metadata { resource },
    )
    .await)
}
#[tauri::command]
async fn native_personal_metadata_request(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    request_id: String,
    input: PersonalMetadataOperation,
) -> Result<NativeReply, ()> {
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        authentication_intent,
        request_id,
        NativeRequest::PersonalMetadata { operation: input },
    )
    .await)
}
#[tauri::command]
async fn native_public_metadata_request(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    request_id: String,
    resource: PublicMetadataResource,
) -> Result<NativeReply, ()> {
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        authentication_intent,
        request_id,
        NativeRequest::PublicMetadata { resource },
    )
    .await)
}
#[tauri::command]
async fn native_socket_open(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    request_id: String,
    on_event: Channel<NativeSocketNotice>,
) -> Result<NativeReply, ()> {
    let lease = match require(&window, &state, Some(&context)) {
        Ok(l) => l,
        Err(e) => return Ok(rejected(context, e)),
    };
    if let Err(error) = canonical(&profile_intent)
        .and_then(|profile| state.client.check_profile_intent(&lease, profile))
    {
        return Ok(rejected(context, error));
    }
    let app = window.app_handle().clone();
    let callback_lease = lease.clone();
    let callback_context = context.clone();
    let observer: NativeSocketObserver = Arc::new(move |scope, notice| {
        let state = app.state::<RuntimeState>();
        // Native lifecycle hooks take this same registry mutex. Context is a
        // replay fence; physical window identity is native minted authority.
        let Ok(registry) = state.registry.lock() else {
            return false;
        };
        let Some(entry) = registry.as_ref() else {
            return false;
        };
        if entry.lease.context_nonce() != callback_context || notice.context != callback_context {
            return false;
        }
        state
            .client
            .with_socket_publication(&callback_lease, scope, || on_event.send(notice).is_ok())
            .unwrap_or(false)
    });
    Ok(invoke(
        window,
        state,
        context,
        Some(profile_intent),
        authentication_intent,
        request_id,
        NativeRequest::OpenSocket { observer },
    )
    .await)
}
#[tauri::command]
async fn native_socket_send(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    handle: String,
    action: NativeSocketAction,
) -> Result<NativeReply, ()> {
    let result = async {
        let lease = require(&window, &state, Some(&context))?;
        state
            .client
            .check_profile_intent(&lease, canonical(&profile_intent)?)?;
        state
            .client
            .socket_send_with_intents(
                &lease,
                canonical(&profile_intent)?,
                authentication_intent
                    .as_deref()
                    .map(canonical)
                    .transpose()?
                    .ok_or(Error::Stale)?,
                canonical(&handle)?,
                action,
            )
            .await?;
        require(&window, &state, Some(&context))?;
        state.client.with_selected_authentication(
            &lease,
            canonical(&profile_intent)?,
            authentication_intent
                .as_deref()
                .map(canonical)
                .transpose()?,
            || (),
        )?;
        Ok::<_, Error>(())
    }
    .await;
    Ok(match result {
        Ok(()) => NativeReply {
            context,
            status: 204,
            body: serde_json::Value::Null,
        },
        Err(e) => rejected(context, e),
    })
}
#[tauri::command]
fn native_socket_close(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: Option<String>,
    handle: String,
) -> NativeReply {
    let result = require(&window, &state, Some(&context)).and_then(|lease| {
        state
            .client
            .check_profile_intent(&lease, canonical(&profile_intent)?)?;
        state.client.socket_close_with_intents(
            &lease,
            canonical(&profile_intent)?,
            authentication_intent
                .as_deref()
                .map(canonical)
                .transpose()?
                .ok_or(Error::Stale)?,
            canonical(&handle)?,
        )
    });
    match result {
        Ok(()) => NativeReply {
            context,
            status: 204,
            body: serde_json::Value::Null,
        },
        Err(e) => rejected(context, e),
    }
}
#[cfg(feature = "synthetic-transport-fixture")]
mod transport_fixture;
fn main() {
    #[cfg(feature = "synthetic-transport-fixture")]
    let configured = transport_fixture::client();
    #[cfg(not(feature = "synthetic-transport-fixture"))]
    let configured = NativeClient::ephemeral();
    let Ok(client) = configured else { return };
    #[cfg(feature = "synthetic-media-fixture")]
    let media = match media_fixture::configured_actor() {
        Ok(m) => m,
        Err(_) => return,
    };
    let builder = tauri::Builder::default()
        .manage(RuntimeState {
            client,
            registry: Mutex::new(None),
            #[cfg(feature = "native-crypto")]
            core: crypto_host::Host::default(),
            #[cfg(feature = "synthetic-media-fixture")]
            media,
        })
        .setup(|app| {
            let handle = app.handle().clone();
            let navigation = handle.clone();
            #[cfg(feature = "native-crypto")]
            app.add_capability(
                tauri::ipc::CapabilityBuilder::new("native-core-main-only")
                    .window("main")
                    .local(true)
                    .permission("allow-native-trust-begin-first-root")
                    .permission("allow-native-trust-request-confirmation")
                    .permission("allow-native-trust-cancel")
                    .permission("allow-native-trust-read-status")
                    .permission("allow-native-chat-publish")
                    .permission("allow-native-chat-receive")
                    .permission("allow-native-chat-mutate")
                    .permission("allow-native-chat-snapshot"),
            )?;
            #[cfg(feature = "synthetic-media-fixture")]
            if handle.state::<RuntimeState>().media.is_some() {
                app.add_capability(
                    tauri::ipc::CapabilityBuilder::new("synthetic-media-main-only")
                        .window("main")
                        .local(true)
                        .permission("allow-native-fixture-media-open")
                        .permission("allow-native-fixture-media-send")
                        .permission("allow-native-fixture-media-close"),
                )?;
            }
            let entry = "index.html";
            #[cfg(feature = "synthetic-media-fixture")]
            let entry = if handle.state::<RuntimeState>().media.is_some() {
                "native-media.html"
            } else {
                entry
            };
            let window = WebviewWindowBuilder::new(app, "main", WebviewUrl::App(entry.into()))
                .title("Mnema Desktop DEV")
                .inner_size(1200.0, 820.0)
                .on_navigation(move |url| {
                    let state = navigation.state::<RuntimeState>();
                    if !bundled(url) {
                        if let Ok(registry) = state.registry.lock()
                            && let Some(entry) = registry.as_ref()
                        {
                            state.detach(entry);
                        }
                        return false;
                    }
                    true
                })
                .build()?;
            let state = handle.state::<RuntimeState>();
            *state
                .registry
                .lock()
                .map_err(|_| "native registry failed")? = Some(
                state
                    .registration(&window, &window.url()?)
                    .map_err(|_| "native registration failed")?,
            );
            let lifecycle = handle.clone();
            window.on_window_event(move |event| {
                if matches!(
                    event,
                    tauri::WindowEvent::Destroyed | tauri::WindowEvent::CloseRequested { .. }
                ) {
                    let state = lifecycle.state::<RuntimeState>();
                    if let Ok(mut registry) = state.registry.lock()
                        && let Some(entry) = registry.take()
                    {
                        state.detach(&entry);
                    }
                }
            });
            Ok(())
        })
        .on_page_load(|webview, payload| {
            if webview.label() != "main"
                || payload.event() != tauri::webview::PageLoadEvent::Started
            {
                return;
            }
            let state = webview.state::<RuntimeState>();
            if let Ok(mut registry) = state.registry.lock() {
                if let Some(entry) = registry.take() {
                    state.detach(&entry);
                }
                if bundled(payload.url())
                    && let Some(window) = webview.app_handle().get_webview_window("main")
                    && let Ok(entry) = state.registration(&window, payload.url())
                {
                    *registry = Some(entry);
                }
            }
        })
        .on_permission_request(|_, _| tauri::webview::PermissionResponse::Deny);
    #[cfg(not(any(feature = "synthetic-media-fixture", feature = "native-crypto")))]
    let builder = builder.invoke_handler(tauri::generate_handler![
        native_context,
        native_connect,
        native_metadata_request,
        native_personal_metadata_request,
        native_public_metadata_request,
        native_auth_login,
        native_auth_password,
        native_auth_register,
        native_admin_request,
        native_auth_me,
        native_auth_refresh,
        native_auth_logout,
        native_disconnect,
        native_request_cancel,
        native_socket_open,
        native_socket_send,
        native_socket_close
    ]);
    #[cfg(feature = "native-crypto")]
    let builder = builder.invoke_handler(tauri::generate_handler![
        native_context,
        native_connect,
        native_metadata_request,
        native_personal_metadata_request,
        native_public_metadata_request,
        native_auth_login,
        native_auth_password,
        native_auth_register,
        native_admin_request,
        native_auth_me,
        native_auth_refresh,
        native_auth_logout,
        native_disconnect,
        native_request_cancel,
        native_socket_open,
        native_socket_send,
        native_socket_close,
        native_trust_begin_first_root,
        native_trust_request_confirmation,
        native_trust_cancel,
        native_trust_read_status,
        native_chat_publish,
        native_chat_receive,
        native_chat_mutate,
        native_chat_snapshot
    ]);
    #[cfg(feature = "synthetic-media-fixture")]
    let builder = builder.invoke_handler(tauri::generate_handler![
        native_context,
        native_connect,
        native_metadata_request,
        native_personal_metadata_request,
        native_public_metadata_request,
        native_auth_login,
        native_auth_password,
        native_auth_register,
        native_admin_request,
        native_auth_me,
        native_auth_refresh,
        native_auth_logout,
        native_disconnect,
        native_request_cancel,
        native_socket_open,
        native_socket_send,
        native_socket_close,
        native_fixture_media_open,
        native_fixture_media_send,
        native_fixture_media_close
    ]);
    builder
        .run(tauri::generate_context!())
        .expect("native preview could not start");
}

#[cfg(test)]
mod local_origin_tests {
    use super::*;
    #[test]
    fn actual_tauri_root_without_slash_is_bundled_and_other_origins_ports_pages_are_rejected() {
        // Tauri2.12.1 manager/webview.rs deliberately leaves App(index.html)
        // at tauri://localhost: Url::path is empty for this custom scheme.
        let root = url::Url::parse("tauri://localhost").unwrap();
        assert_eq!(root.path(), "");
        assert!(bundled(&root));
        for raw in [
            "tauri://localhost/",
            "tauri://localhost/index.html",
            "http://tauri.localhost/",
        ] {
            assert!(bundled(&url::Url::parse(raw).unwrap()));
        }
        for raw in [
            "tauri://localhost:9000",
            "http://tauri.localhost:9000/",
            "https://example.invalid/",
            "tauri://localhost/other.html",
            "tauri://user@localhost/",
        ] {
            assert!(!bundled(&url::Url::parse(raw).unwrap()));
        }
    }
    #[cfg(feature = "synthetic-media-fixture")]
    #[test]
    fn native_document_authority_does_not_follow_same_document_history_addresses() {
        let media_url = url::Url::parse("tauri://localhost/native-media.html").unwrap();
        let app_url = url::Url::parse("tauri://localhost/index.html").unwrap();
        let media = NativeDocument::at_native_load(&media_url, true).unwrap();
        let app = NativeDocument::at_native_load(&app_url, true).unwrap();
        assert!(media == NativeDocument::SyntheticMedia);
        assert!(app == NativeDocument::App);
        // A history-only URL is deliberately not fed to at_native_load. The
        // actual native registry keeps the loaded document's authority; another
        // actual native load must create a new registration/opaque window lease.
        assert!(media != app);
        assert!(NativeDocument::at_native_load(&media_url, false).is_err());
        assert!(
            NativeDocument::at_native_load(
                &url::Url::parse("https://example.invalid/native-media.html").unwrap(),
                true
            )
            .is_err()
        );
    }
}
