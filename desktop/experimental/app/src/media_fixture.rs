//! New separate compile-time synthetic qualification adapter. No production MLS grant.
use super::*;
use mnema_private_synthetic_media_actor::{BoundMedia, EventSink, NativeMediaEvent, Role};
use mnema_private_synthetic_media_policy::{Action, load_owned_bootstrap};
use std::path::Path;
pub(super) fn configured_actor() -> Result<Option<MediaActor>, Error> {
    if std::env::var("MNEMA_SYNTHETIC_NATIVE_MEDIA_FIXTURE")
        .ok()
        .as_deref()
        != Some("1")
    {
        return Ok(None);
    }
    let path = std::env::var_os("MNEMA_SYNTHETIC_NATIVE_MEDIA_BOOTSTRAP").ok_or(Error::Denied)?;
    let owned = load_owned_bootstrap(Path::new(&path)).map_err(|_| Error::Denied)?;
    MediaActor::fixture(owned).map(Some)
}
fn require_media(
    window: &WebviewWindow,
    state: &RuntimeState,
) -> Result<(NativeWindowLease, NativeMediaWindow, MediaActor), Error> {
    let lease = require_document(window, state, None, NativeDocument::SyntheticMedia)?;
    let registry = state.registry.lock().map_err(|_| Error::Internal)?;
    let entry = registry.as_ref().ok_or(Error::Stale)?;
    if entry.document != NativeDocument::SyntheticMedia
        || entry.lease.context_nonce() != lease.context_nonce()
    {
        return Err(Error::Stale);
    }
    let actor = state
        .media
        .as_ref()
        .ok_or(Error::QualificationRequired)?
        .clone();
    let media = entry.media_window.as_ref().ok_or(Error::Stale)?.clone();
    drop(registry);
    actor.check_window(&media)?;
    Ok((lease, media, actor))
}
async fn finish_open(
    actor: &MediaActor,
    media: &NativeMediaWindow,
    bound: BoundMedia,
    final_native_fence: Result<bool, Error>,
) -> Result<BoundMedia, Error> {
    if !matches!(final_native_fence, Ok(true)) {
        // Even a failed final require (including cosmetic history outside the
        // route allowlist) must retire the already-open native session. The
        // captured opaque owner is native authority for cleanup, never renderer.
        let _ = actor.close(media, &bound.handle, bound.generation).await;
        return Err(Error::Stale);
    }
    Ok(bound)
}
#[tauri::command]
pub(super) async fn native_fixture_media_open(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    role: Role,
    on_event: Channel<NativeMediaEvent>,
) -> Result<BoundMedia, &'static str> {
    let result = async {
        let (root, media, actor) = require_media(&window, &state)?;
        let context = root.context_nonce();
        let callback_context = context.clone();
        let native = identity(&window)?;
        let app = window.app_handle().clone();
        let callback_media = media.clone();
        let callback_root = root.clone();
        let sink: EventSink = Arc::new(move |event| {
            let state = app.state::<RuntimeState>();
            // Actor holds its own publication lock. Never wait on registry or
            // call back into actor: lifecycle registry→actor order must not invert.
            let registry = state.registry.try_lock().map_err(|_| ())?;
            let entry = registry.as_ref().ok_or(())?;
            if entry.identity != native
                || entry.document != NativeDocument::SyntheticMedia
                || entry.lease.context_nonce() != callback_context
                || entry
                    .media_window
                    .as_ref()
                    .is_none_or(|w| !w.same_native_context(&callback_media))
            {
                return Err(());
            }
            state.client.check_window(&callback_root).map_err(|_| ())?;
            on_event.send(event).map_err(|_| ())
        });
        let bound = actor.open(&media, role, sink).await?;
        let final_native_fence =
            require_media(&window, &state).map(|(current, current_media, _)| {
                current.context_nonce() == context && current_media.same_native_context(&media)
            });
        finish_open(&actor, &media, bound, final_native_fence).await
    }
    .await;
    result.map_err(|_| "native fixture media unavailable")
}
#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use std::{
        fs,
        os::unix::fs::PermissionsExt,
        process::{Child, Command, Stdio},
        time::Duration,
    };
    struct Fixture {
        child: Child,
        directory: std::path::PathBuf,
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            unsafe {
                libc::kill(self.child.id() as i32, libc::SIGINT);
            }
            for _ in 0..100 {
                if self.child.try_wait().ok().flatten().is_some() {
                    break;
                }
                std::thread::sleep(Duration::from_millis(50));
            }
            let _ = self.child.kill();
            let _ = self.child.wait();
            let _ = fs::remove_dir_all(&self.directory);
        }
    }
    #[tokio::test(flavor = "current_thread")]
    async fn failed_final_document_fence_closes_actual_tls_native_session_before_response() {
        let directory =
            std::env::temp_dir().join(format!("mnema-native-document-{}", Uuid::new_v4()));
        fs::create_dir(&directory).unwrap();
        fs::set_permissions(&directory, fs::Permissions::from_mode(0o700)).unwrap();
        let bootstrap = directory.join("bootstrap.json");
        let binary = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../native-media-integration/native-media-backend-fixture");
        let child = Command::new(binary)
            .env("MNEMA_SYNTHETIC_NATIVE_MEDIA_FIXTURE", "1")
            .env("MNEMA_SYNTHETIC_NATIVE_MEDIA_BOOTSTRAP", &bootstrap)
            .args([
                "-test.run",
                "^TestNativeMediaOperationalFixture$",
                "-test.timeout",
                "2m",
            ])
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .unwrap();
        let _fixture = Fixture { child, directory };
        tokio::time::timeout(Duration::from_secs(45), async {
            while !bootstrap.exists() {
                tokio::time::sleep(Duration::from_millis(50)).await;
            }
        })
        .await
        .unwrap();
        let actor = MediaActor::fixture(load_owned_bootstrap(&bootstrap).unwrap()).unwrap();
        let window = actor.native_attach_window().unwrap();
        let sink: EventSink = Arc::new(|_| Ok(()));
        for failed_fence in [Err(Error::Denied), Ok(false)] {
            let bound = actor
                .open(&window, Role::Publisher, sink.clone())
                .await
                .unwrap();
            let old_handle = bound.handle.clone();
            let generation = bound.generation;
            assert!(matches!(
                finish_open(&actor, &window, bound, failed_fence).await,
                Err(Error::Stale)
            ));
            assert_eq!(
                actor.close(&window, &old_handle, generation).await,
                Err(Error::Denied)
            );
            // Another real login/socket can acquire this role only after the
            // rejected open has awaited native destruction + logout cleanup.
        }
        let bound = actor.open(&window, Role::Publisher, sink).await.unwrap();
        let bound = finish_open(&actor, &window, bound, Ok(true)).await.unwrap();
        actor
            .close(&window, &bound.handle, bound.generation)
            .await
            .unwrap();
        actor.native_detach_window(&window).unwrap();
    }
}
#[tauri::command]
pub(super) async fn native_fixture_media_send(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    handle: String,
    generation: u64,
    action: Action,
) -> Result<(), &'static str> {
    let result = async {
        let (root, media, actor) = require_media(&window, &state)?;
        actor.send(&media, &handle, generation, action).await?;
        let (current, current_media, _) = require_media(&window, &state)?;
        if root.context_nonce() != current.context_nonce()
            || !media.same_native_context(&current_media)
        {
            return Err(Error::Stale);
        }
        Ok::<_, Error>(())
    }
    .await;
    result.map_err(|_| "native fixture media action unavailable")
}
#[tauri::command]
pub(super) async fn native_fixture_media_close(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    handle: String,
    generation: u64,
) -> Result<(), &'static str> {
    let result = async {
        let (root, media, actor) = require_media(&window, &state)?;
        actor.close(&media, &handle, generation).await?;
        let (current, current_media, _) = require_media(&window, &state)?;
        if root.context_nonce() != current.context_nonce()
            || !media.same_native_context(&current_media)
        {
            return Err(Error::Stale);
        }
        Ok::<_, Error>(())
    }
    .await;
    result.map_err(|_| "native fixture media close unavailable")
}
