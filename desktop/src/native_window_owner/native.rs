//! Own-window lifecycle fencing only. No foreign window subclassing, hooks into
//! game processes, input synthesis, privilege changes, or render integration.
use super::{
    CheckedGame, Error, FocusDriver,
    policy::{GameObservation, Generation, ProcessIdentity, WindowIdentity},
};
use std::{
    cell::Cell,
    marker::PhantomData,
    mem::size_of,
    rc::Rc,
    sync::atomic::{AtomicU64, Ordering},
    time::Instant,
};
use windows_sys::Win32::{
    Foundation::{CloseHandle, FILETIME, HANDLE, HWND, LPARAM, LRESULT, WAIT_TIMEOUT, WPARAM},
    Graphics::Dwm::{DWMWA_CLOAKED, DwmGetWindowAttribute},
    System::Threading::{
        GetCurrentProcessId, GetCurrentThreadId, GetProcessId, GetProcessTimes, OpenProcess,
        PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_SYNCHRONIZE, WaitForSingleObject,
    },
    UI::{
        Shell::{DefSubclassProc, GetWindowSubclass, RemoveWindowSubclass, SetWindowSubclass},
        WindowsAndMessaging::{
            GetForegroundWindow, GetWindowThreadProcessId, IsIconic, IsWindow, IsWindowVisible,
            SetForegroundWindow, WM_NCDESTROY,
        },
    },
};

static NEXT_INSTANCE: AtomicU64 = AtomicU64::new(1);
fn instance() -> Result<Generation, Error> {
    let value = NEXT_INSTANCE
        .try_update(Ordering::Relaxed, Ordering::Relaxed, |current| {
            current.checked_add(1)
        })
        .map_err(|_| Error::IdentityExhausted)?;
    Generation::new(value).ok_or(Error::IdentityExhausted)
}

struct ProcessLease {
    handle: HANDLE,
    pid: u32,
    creation: u64,
}
impl Drop for ProcessLease {
    fn drop(&mut self) {
        // SAFETY: one uniquely owned non-null OpenProcess handle, never exposed.
        unsafe {
            CloseHandle(self.handle);
        }
    }
}
impl ProcessLease {
    fn open(pid: u32) -> Result<Self, Error> {
        // SAFETY: scalar PID, bounded minimal rights and no handle inheritance.
        let handle = unsafe {
            OpenProcess(
                PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SYNCHRONIZE,
                0,
                pid,
            )
        };
        if handle.is_null() {
            return Err(Error::ProcessUnavailable);
        }
        let mut lease = Self {
            handle,
            pid,
            creation: 0,
        };
        lease.creation = lease.creation_time()?;
        lease.validate()?;
        Ok(lease)
    }
    fn creation_time(&self) -> Result<u64, Error> {
        let mut created = FILETIME {
            dwLowDateTime: 0,
            dwHighDateTime: 0,
        };
        let mut exited = created;
        let mut kernel = created;
        let mut user = created;
        // SAFETY: the retained handle stays open; four initialized FILETIMEs
        // remain live writable output buffers for the duration of the call.
        if unsafe {
            GetProcessTimes(
                self.handle,
                &mut created,
                &mut exited,
                &mut kernel,
                &mut user,
            )
        } == 0
        {
            return Err(Error::ProcessUnavailable);
        }
        let value = (u64::from(created.dwHighDateTime) << 32) | u64::from(created.dwLowDateTime);
        if value == 0 {
            Err(Error::ProcessUnavailable)
        } else {
            Ok(value)
        }
    }
    fn validate(&self) -> Result<(), Error> {
        // SAFETY: owned handle cannot close while borrowed; zero-timeout wait
        // never blocks the GUI. Only WAIT_TIMEOUT means still-live process.
        if unsafe { WaitForSingleObject(self.handle, 0) } != WAIT_TIMEOUT
            || unsafe { GetProcessId(self.handle) } != self.pid
            || self.creation_time()? != self.creation
        {
            return Err(Error::ProcessChanged);
        }
        Ok(())
    }
    fn identity(&self) -> Result<ProcessIdentity, Error> {
        ProcessIdentity::from_native(self.pid, self.creation).ok_or(Error::ProcessChanged)
    }
}

/// Stable process ownership but NOT stable lifetime of a foreign HWND. Captures
/// only the selected HWND; does not enumerate windows/processes or read titles.
pub struct NativeWindow {
    hwnd: HWND,
    tid: u32,
    process: ProcessLease,
    identity: WindowIdentity,
    _thread_bound: PhantomData<Rc<()>>,
}
impl NativeWindow {
    /// Raw HWND comes from a trusted native owner, never renderer IPC. This
    /// diagnostic snapshot alone cannot authorize an external focus effect.
    /// # Safety
    /// `hwnd` must originate from the trusted native window owner. This API is
    /// never a renderer/IPC entry point; foreign HWND lifetime remains unproven.
    pub unsafe fn inspect(hwnd: HWND) -> Result<Self, Error> {
        if hwnd.is_null() {
            return Err(Error::InvalidWindow);
        }
        let mut pid = 0;
        // SAFETY: writes a live scalar PID output; HWND is only queried.
        let tid = unsafe { GetWindowThreadProcessId(hwnd, &mut pid) };
        if tid == 0 || pid == 0 {
            return Err(Error::InvalidWindow);
        }
        let process = ProcessLease::open(pid)?;
        let identity = WindowIdentity::from_native(hwnd as usize, process.identity()?, instance()?)
            .ok_or(Error::InvalidWindow)?;
        let result = Self {
            hwnd,
            tid,
            process,
            identity,
            _thread_bound: PhantomData,
        };
        result.validate_ownership()?;
        Ok(result)
    }
    pub fn identity(&self) -> WindowIdentity {
        self.identity
    }
    fn validate_ownership(&self) -> Result<(), Error> {
        self.process.validate()?;
        let mut pid = 0;
        // SAFETY: scalar OS inspection only; IsWindow is a health hint, never
        // treated as a foreign HWND lifetime/reuse proof.
        if unsafe { IsWindow(self.hwnd) } == 0
            || unsafe { GetWindowThreadProcessId(self.hwnd, &mut pid) } != self.tid
            || pid != self.process.pid
        {
            return Err(Error::WindowChanged);
        }
        Ok(())
    }
    pub fn validate_health(&self) -> Result<(), Error> {
        self.validate_ownership()?;
        if unsafe { IsWindowVisible(self.hwnd) } == 0 {
            return Err(Error::Hidden);
        }
        if unsafe { IsIconic(self.hwnd) } != 0 {
            return Err(Error::Minimized);
        }
        let mut cloaked = 0u32;
        // SAFETY: writable DWORD buffer and its exact size; DWM failure is not
        // interpreted as "not cloaked".
        if unsafe {
            DwmGetWindowAttribute(
                self.hwnd,
                DWMWA_CLOAKED as u32,
                (&mut cloaked as *mut u32).cast(),
                size_of::<u32>() as u32,
            )
        } < 0
        {
            return Err(Error::CloakingUnknown);
        }
        if cloaked != 0 {
            return Err(Error::Cloaked);
        }
        self.validate_ownership()
    }
}

struct Lifecycle {
    alive: Cell<bool>,
    detached: Cell<bool>,
}

unsafe extern "system" fn own_subclass(
    hwnd: HWND,
    message: u32,
    wparam: WPARAM,
    lparam: LPARAM,
    id: usize,
    reference: usize,
) -> LRESULT {
    // SAFETY: SetWindowSubclass holds this stable Box address; guard Drop never
    // frees it while attached. All accesses occur on the HWND creating thread.
    let lifecycle = unsafe { &*(reference as *const Lifecycle) };
    if message == WM_NCDESTROY {
        lifecycle.alive.set(false);
        // Invalidate before forwarding destruction, so reuse cannot preserve
        // this generation. This callback never attaches to a game process.
        lifecycle
            .detached
            .set(unsafe { RemoveWindowSubclass(hwnd, Some(own_subclass), id) } != 0);
    }
    // SAFETY: forwards the unchanged native message to the subclass chain.
    unsafe { DefSubclassProc(hwnd, message, wparam, lparam) }
}

/// Synchronous destruction fence for one explicitly registered OWN surface.
/// !Send/!Sync; every operation additionally checks the native creating thread.
pub struct OwnOverlay {
    window: NativeWindow,
    lifecycle: Option<Box<Lifecycle>>,
    subclass: usize,
}
impl OwnOverlay {
    pub fn register(window: NativeWindow) -> Result<Self, Error> {
        // SAFETY: scalar process/thread IDs from current execution context.
        if window.process.pid != unsafe { GetCurrentProcessId() } {
            return Err(Error::NotOwnSurface);
        }
        if window.tid != unsafe { GetCurrentThreadId() } {
            return Err(Error::WrongThread);
        }
        let mut lifecycle = Box::new(Lifecycle {
            alive: Cell::new(true),
            detached: Cell::new(false),
        });
        // A Box address is unique while attached and fits the native UINT_PTR.
        let subclass = (&mut *lifecycle as *mut Lifecycle) as usize;
        // SAFETY: only own same-thread HWND; callback/data remain valid until
        // explicit removal/destruction, with Drop leaking data on detach failure.
        if unsafe { SetWindowSubclass(window.hwnd, Some(own_subclass), subclass, subclass) } == 0 {
            return Err(Error::LifecycleUnavailable);
        }
        let result = Self {
            window,
            lifecycle: Some(lifecycle),
            subclass,
        };
        result.validate_lifetime(result.window.identity)?;
        Ok(result)
    }
    pub fn identity(&self) -> WindowIdentity {
        self.window.identity
    }
    fn validate_lifetime(&self, expected: WindowIdentity) -> Result<(), Error> {
        if expected != self.window.identity {
            return Err(Error::NotOwnSurface);
        }
        if self.window.tid != unsafe { GetCurrentThreadId() } {
            return Err(Error::WrongThread);
        }
        let Some(lifecycle) = &self.lifecycle else {
            return Err(Error::LifecycleUnavailable);
        };
        if !lifecycle.alive.get() || lifecycle.detached.get() {
            return Err(Error::WindowChanged);
        }
        let mut actual = 0usize;
        // SAFETY: same-thread own window, exact callback+ID and writable scalar
        // output. Detect removed/replaced subclass data, not just matching HWND.
        if unsafe {
            GetWindowSubclass(
                self.window.hwnd,
                Some(own_subclass),
                self.subclass,
                &mut actual,
            )
        } == 0
            || actual != self.subclass
        {
            return Err(Error::LifecycleUnavailable);
        }
        self.window.validate_ownership()
    }
    pub fn validate(&self, expected: WindowIdentity) -> Result<(), Error> {
        self.validate_lifetime(expected)?;
        self.window.validate_health()?;
        self.validate_lifetime(expected)
    }
    pub fn focus_once(&self, expected: WindowIdentity) -> Result<(), Error> {
        self.validate(expected)?;
        // SAFETY: exact live same-thread own HWND under lifecycle guard. One
        // documented focus attempt only; no SendInput/AttachThreadInput fallback.
        if unsafe { SetForegroundWindow(self.window.hwnd) } == 0 {
            return Err(Error::FocusDenied);
        }
        self.validate(expected)?;
        if unsafe { GetForegroundWindow() } != self.window.hwnd {
            return Err(Error::FocusDenied);
        }
        Ok(())
    }
}
impl Drop for OwnOverlay {
    fn drop(&mut self) {
        let Some(lifecycle) = self.lifecycle.take() else {
            return;
        };
        if lifecycle.detached.get() {
            return;
        }
        // SAFETY: guard remains on original window thread. Never free callback
        // data on removal failure: stale callback must not dereference freed Box.
        if self.window.tid == unsafe { GetCurrentThreadId() }
            && unsafe { RemoveWindowSubclass(self.window.hwnd, Some(own_subclass), self.subclass) }
                != 0
        {
            return;
        }
        let _ = Box::leak(lifecycle);
    }
}

/// Production external-game lifetime is deliberately unsupported in this
/// prototype. WinEvents/polling can improve diagnostics, not mint proof.
pub struct Driver {
    pub game: NativeWindow,
    pub overlay: OwnOverlay,
}
impl FocusDriver for Driver {
    fn now(&self) -> Instant {
        Instant::now()
    }
    fn check_game(&mut self) -> Result<CheckedGame, Error> {
        self.game.validate_health()?;
        Ok(CheckedGame {
            observation: GameObservation::from_native(self.game.identity, true),
            lifetime_supported: false,
        })
    }
    fn check_own_overlay(&mut self, expected: WindowIdentity) -> Result<(), Error> {
        self.overlay.validate(expected)
    }
    fn focus_once(&mut self, expected: WindowIdentity) -> Result<(), Error> {
        self.overlay.focus_once(expected)
    }
    fn own_foreground(&mut self) -> Result<WindowIdentity, Error> {
        self.overlay.validate(self.overlay.identity())?;
        if unsafe { GetForegroundWindow() } != self.overlay.window.hwnd {
            return Err(Error::FocusDenied);
        }
        Ok(self.overlay.identity())
    }
}
