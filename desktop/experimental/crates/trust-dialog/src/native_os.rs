use super::*;
use mnema_private_native_client_broker::{NativeClient, NativeWindowLease};
use std::sync::{
    Mutex,
    atomic::{AtomicBool, Ordering},
};
use tauri::WebviewWindow;

/// Trusted host callback must check its actual native OS window identity,
/// immutable loaded-document kind and exact current native registry lease.
/// This is Rust host wiring, never a renderer callback/label/session claim.
pub type NativeRegistryValidator =
    Arc<dyn Fn(&WebviewWindow, &NativeWindowLease) -> Result<(), DialogError> + Send + Sync>;
struct Pending {
    operation: Uuid,
    cancelled: Arc<AtomicBool>,
    finished: Arc<AtomicBool>,
}
#[derive(Clone)]
pub struct TauriNativeTrustDialog {
    window: WebviewWindow,
    client: NativeClient,
    lease: NativeWindowLease,
    validator: NativeRegistryValidator,
    active: Arc<Mutex<Option<Arc<Pending>>>>,
}
impl TauriNativeTrustDialog {
    pub fn from_native_window(
        window: WebviewWindow,
        client: NativeClient,
        lease: NativeWindowLease,
        validator: NativeRegistryValidator,
    ) -> Result<Self, DialogError> {
        validator(&window, &lease)?;
        client
            .check_window(&lease)
            .map_err(|_| DialogError::Stale)?;
        Ok(Self {
            window,
            client,
            lease,
            validator,
            active: Arc::new(Mutex::new(None)),
        })
    }
    fn check(&self, request: &NativeDialogRequest) -> Result<(), DialogError> {
        request.check_deadline()?;
        (self.validator)(&self.window, &self.lease)?;
        self.client
            .check_authenticated_scope(&self.lease, request.authenticated_scope())
            .map_err(|_| DialogError::Stale)
    }
}
struct AbortedFuture {
    cancelled: Arc<AtomicBool>,
    armed: bool,
}
impl Drop for AbortedFuture {
    fn drop(&mut self) {
        if self.armed {
            self.cancelled.store(true, Ordering::Release)
        }
    }
}
impl TauriNativeTrustDialog {
    fn confirm_request(&self, request: NativeDialogRequest) -> NativeDialogFuture {
        let service = self.clone();
        Box::pin(async move {
            service.check(&request)?;
            let pending = Arc::new(Pending {
                operation: request.operation_id(),
                cancelled: Arc::new(AtomicBool::new(false)),
                finished: Arc::new(AtomicBool::new(false)),
            });
            {
                let mut active = service
                    .active
                    .lock()
                    .map_err(|_| DialogError::Unavailable)?;
                if active.is_some() {
                    return Err(DialogError::Unavailable);
                }
                *active = Some(pending.clone());
            }
            let mut abort = AbortedFuture {
                cancelled: pending.cancelled.clone(),
                armed: true,
            };
            let (send, receive) = tokio::sync::oneshot::channel();
            let callback_service = service.clone();
            let callback_request = request.clone();
            let callback_pending = pending.clone();
            let finish: Finish = Box::new(move |result: Result<bool, DialogError>| {
                callback_pending.finished.store(true, Ordering::Release);
                let answer = if callback_pending.cancelled.load(Ordering::Acquire) {
                    Err(DialogError::Cancelled)
                } else {
                    result.and_then(|approved| {
                        callback_service.check(&callback_request)?;
                        Ok(NativeDialogDecision {
                            request: callback_request,
                            approved,
                            cancelled: callback_pending.cancelled.clone(),
                        })
                    })
                };
                if let Ok(mut active) = callback_service.active.lock()
                    && active
                        .as_ref()
                        .is_some_and(|p| Arc::ptr_eq(p, &callback_pending))
                {
                    *active = None;
                }
                let _ = send.send(answer);
            });
            let native_service = service.clone();
            let native_request = request.clone();
            let native_pending = pending.clone();
            let dispatch = service.window.run_on_main_thread(move || {
                if native_pending.cancelled.load(Ordering::Acquire) {
                    finish(Err(DialogError::Cancelled));
                    return;
                }
                if let Err(error) = native_service.check(&native_request) {
                    finish(Err(error));
                    return;
                }
                platform::show(
                    &native_service.window,
                    &native_request,
                    native_pending.cancelled.clone(),
                    native_pending.finished.clone(),
                    finish,
                );
            });
            if dispatch.is_err() {
                if let Ok(mut active) = service.active.lock()
                    && active.as_ref().is_some_and(|p| Arc::ptr_eq(p, &pending))
                {
                    *active = None;
                }
                return Err(DialogError::Unavailable);
            }
            let result = receive.await.map_err(|_| DialogError::Unavailable)?;
            // Normal completion must not masquerade as an abandoned future.
            // Explicit cancellation remains in the exact shared decision latch.
            abort.armed = false;
            drop(abort);
            result
        })
    }
    fn cancel_request(&self, operation: Uuid) -> Result<(), DialogError> {
        let active = self.active.lock().map_err(|_| DialogError::Unavailable)?;
        let pending = active
            .as_ref()
            .filter(|p| p.operation == operation)
            .ok_or(DialogError::Stale)?;
        pending.cancelled.store(true, Ordering::Release);
        Ok(())
    }
}
impl NativeTrustDialog for TauriNativeTrustDialog {
    fn confirm_first_root(&self, request: NativeDialogRequest) -> NativeDialogFuture {
        if request.is_device_removal() {
            return Box::pin(async { Err(DialogError::Denied) });
        }
        self.confirm_request(request)
    }
    fn confirm_device_removal(&self, request: NativeDialogRequest) -> NativeDialogFuture {
        if !request.is_device_removal() {
            return Box::pin(async { Err(DialogError::Denied) });
        }
        self.confirm_request(request)
    }
    fn cancel_first_root(&self, operation: Uuid) -> Result<(), DialogError> {
        self.cancel_request(operation)
    }
    fn cancel_device_removal(&self, operation: Uuid) -> Result<(), DialogError> {
        self.cancel_request(operation)
    }
}
type Finish = Box<dyn FnOnce(Result<bool, DialogError>) + Send + 'static>;
// A fresh confirmation instance, distinct even when trusted native callers
// repeat the same immutable Request. Every deferred user retains this Arc.
#[cfg(any(target_os = "macos", test))]
#[derive(Clone)]
struct DialogInstance(Arc<()>);
#[cfg(any(target_os = "macos", test))]
impl DialogInstance {
    fn fresh() -> Self {
        Self(Arc::new(()))
    }
    fn key(&self) -> usize {
        Arc::as_ptr(&self.0) as usize
    }
    fn same(&self, other: &Self) -> bool {
        Arc::ptr_eq(&self.0, &other.0)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn normal_completion_disarms_abort_without_erasing_explicit_cancellation() {
        let flag = Arc::new(AtomicBool::new(false));
        let mut guard = AbortedFuture {
            cancelled: flag.clone(),
            armed: true,
        };
        guard.armed = false;
        drop(guard);
        assert!(!flag.load(Ordering::Acquire));
        let guard = AbortedFuture {
            cancelled: flag.clone(),
            armed: true,
        };
        drop(guard);
        assert!(flag.load(Ordering::Acquire));
        let guard = AbortedFuture {
            cancelled: flag.clone(),
            armed: false,
        };
        drop(guard);
        assert!(flag.load(Ordering::Acquire));
    }
    #[test]
    fn deferred_old_timer_cannot_select_repeated_request_new_confirmation() {
        let first = DialogInstance::fresh();
        let timer = first.clone();
        let mut sheets = std::collections::HashMap::new();
        sheets.insert(first.key(), first.clone());
        // Native completion removes the first sheet before an already queued
        // timer reaches its native-main-thread lookup. Same request may reopen.
        sheets.remove(&first.key());
        drop(first);
        let second = DialogInstance::fresh();
        sheets.insert(second.key(), second.clone());
        assert!(!timer.same(&second));
        assert!(
            sheets
                .get(&timer.key())
                .filter(|s| s.same(&timer))
                .is_none()
        );
        assert!(sheets.get(&second.key()).is_some_and(|s| s.same(&second)));
    }
}
#[cfg(target_os = "macos")]
#[path = "platform_mac.rs"]
mod platform;
#[cfg(windows)]
#[path = "platform_windows.rs"]
mod platform;
#[cfg(target_os = "linux")]
#[path = "platform_linux.rs"]
mod platform;
#[cfg(not(any(target_os = "macos", windows, target_os = "linux")))]
mod platform {
    use super::*;
    pub(super) fn show(
        _: &WebviewWindow,
        _: &NativeDialogRequest,
        _: Arc<AtomicBool>,
        _: Arc<AtomicBool>,
        finish: Finish,
    ) {
        finish(Err(DialogError::Unavailable));
    }
}
