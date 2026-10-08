//! Selected retained game and bounded WinEvent observation. No process injection,
//! foreign subclassing, arbitrary focus, key synthesis or anti-cheat workaround.
use crate::gaming_fixture::{
    Error,
    observed::{ObservedDriver, Snapshot},
    policy::{FocusKind, FocusRequest, GameObservation, WindowIdentity},
};
use crate::native_input::SafetyGate;
use crate::native_window_owner::native::{NativeWindow, OwnOverlay};
use std::{
    cell::RefCell,
    mem::size_of,
    ptr::null_mut,
    sync::{
        Arc,
        mpsc::{self, Receiver, SyncSender},
    },
    time::Instant,
};
use windows_sys::Win32::{
    Foundation::*,
    Graphics::Gdi::*,
    UI::{Accessibility::*, HiDpi::GetDpiForWindow, WindowsAndMessaging::*},
};

#[cfg(all(feature = "shell", feature = "gaming-fixture"))]
pub(crate) fn current_process_identity()
-> Result<crate::gaming_fixture::policy::ProcessIdentity, Error> {
    use windows_sys::Win32::System::Threading::{
        GetCurrentProcess, GetCurrentProcessId, GetProcessTimes,
    };
    let mut created = FILETIME::default();
    let mut exited = FILETIME::default();
    let mut kernel = FILETIME::default();
    let mut user = FILETIME::default();
    if unsafe {
        GetProcessTimes(
            GetCurrentProcess(),
            &mut created,
            &mut exited,
            &mut kernel,
            &mut user,
        )
    } == 0
    {
        return Err(Error::Owner);
    }
    let creation = (u64::from(created.dwHighDateTime) << 32) | u64::from(created.dwLowDateTime);
    crate::gaming_fixture::policy::ProcessIdentity::from_native(
        unsafe { GetCurrentProcessId() },
        creation,
    )
    .ok_or(Error::Owner)
}

#[derive(Clone, Copy, PartialEq, Eq)]
struct Display {
    monitor: usize,
    rect: [i32; 4],
    monitor_rect: [i32; 4],
    dpi: u32,
}
fn display(hwnd: HWND) -> Result<Display, Error> {
    unsafe {
        let mut rect = RECT::default();
        let monitor = MonitorFromWindow(hwnd, MONITOR_DEFAULTTONULL);
        let mut info = MONITORINFO {
            cbSize: size_of::<MONITORINFO>() as u32,
            ..Default::default()
        };
        let dpi = GetDpiForWindow(hwnd);
        if monitor.is_null()
            || dpi == 0
            || GetWindowRect(hwnd, &mut rect) == 0
            || GetMonitorInfoW(monitor, &mut info) == 0
            || rect.right <= rect.left
            || rect.bottom <= rect.top
        {
            return Err(Error::Render);
        }
        Ok(Display {
            monitor: monitor as usize,
            rect: [rect.left, rect.top, rect.right, rect.bottom],
            monitor_rect: [
                info.rcMonitor.left,
                info.rcMonitor.top,
                info.rcMonitor.right,
                info.rcMonitor.bottom,
            ],
            dpi,
        })
    }
}

struct EventSlot {
    game: usize,
    overlay: usize,
    sender: SyncSender<u32>,
    gate: Arc<SafetyGate>,
}
thread_local! {static EVENTS:RefCell<Option<EventSlot>>=const {RefCell::new(None)};}
unsafe extern "system" fn event_callback(
    _: HWINEVENTHOOK,
    event: u32,
    hwnd: HWND,
    object: i32,
    child: i32,
    _: u32,
    _: u32,
) {
    EVENTS.with(|slot| {
        let Ok(slot) = slot.try_borrow() else { return };
        let Some(slot) = slot.as_ref() else { return };
        let foreground = event == EVENT_SYSTEM_FOREGROUND;
        if !foreground
            && (hwnd as usize != slot.game
                || object != OBJID_WINDOW
                || child != CHILDID_SELF as i32)
        {
            return;
        }
        // Exact overlay alone is exempt. Main/other app windows clear immediately.
        let foreign = foreground && hwnd as usize != slot.game && hwnd as usize != slot.overlay;
        let disruptive = foreign
            || matches!(
                event,
                EVENT_OBJECT_DESTROY
                    | EVENT_OBJECT_HIDE
                    | EVENT_OBJECT_LOCATIONCHANGE
                    | EVENT_SYSTEM_MINIMIZESTART
            );
        if disruptive {
            slot.gate.revoke();
        }
        let code = if foreign {
            EVENT_SYSTEM_FOREGROUND
        } else {
            event
        };
        if slot.sender.try_send(code).is_err() {
            slot.gate.revoke();
        }
    });
}
struct Events {
    hooks: Vec<HWINEVENTHOOK>,
    receiver: Receiver<u32>,
    destroyed: bool,
}
impl Events {
    fn register(game: HWND, overlay: HWND, pid: u32, gate: Arc<SafetyGate>) -> Result<Self, Error> {
        let (sender, receiver) = mpsc::sync_channel(32);
        if EVENTS.with(|slot| slot.borrow().is_some()) {
            return Err(Error::Render);
        }
        EVENTS.with(|slot| {
            *slot.borrow_mut() = Some(EventSlot {
                game: game as usize,
                overlay: overlay as usize,
                sender,
                gate: gate.clone(),
            })
        });
        let mut result = Self {
            hooks: Vec::new(),
            receiver,
            destroyed: false,
        };
        for (min, max, process) in [
            (EVENT_SYSTEM_FOREGROUND, EVENT_SYSTEM_FOREGROUND, 0),
            (EVENT_SYSTEM_MINIMIZESTART, EVENT_SYSTEM_MINIMIZEEND, pid),
            (EVENT_OBJECT_CREATE, EVENT_OBJECT_LOCATIONCHANGE, pid),
        ] {
            let hook = unsafe {
                SetWinEventHook(
                    min,
                    max,
                    null_mut(),
                    Some(event_callback),
                    process,
                    0,
                    WINEVENT_OUTOFCONTEXT,
                )
            };
            if hook.is_null() {
                gate.revoke();
                return Err(Error::Render);
            }
            result.hooks.push(hook);
        }
        Ok(result)
    }
    fn drain(&mut self) -> Result<(), Error> {
        while let Ok(event) = self.receiver.try_recv() {
            if event == EVENT_OBJECT_DESTROY {
                self.destroyed = true;
            }
        }
        if self.destroyed {
            Err(Error::Owner)
        } else {
            Ok(())
        }
    }
}
impl Drop for Events {
    fn drop(&mut self) {
        EVENTS.with(|slot| {
            if let Some(slot) = slot.borrow_mut().take() {
                slot.gate.revoke();
            }
        });
        for hook in self.hooks.drain(..) {
            unsafe {
                UnhookWinEvent(hook);
            }
        }
    }
}

pub struct NativeGame {
    window: NativeWindow,
    hwnd: HWND,
    initial_display: Display,
    events: Events,
    gate: Arc<SafetyGate>,
}
impl NativeGame {
    /// Native-only selection: this HWND was enumerated on the thread reported
    /// by the stdout pipe of a child WE spawned. Actual child PID is compared.
    /// An arbitrary renderer/server HWND cannot enter this constructor.
    pub(crate) unsafe fn cooperating_child(
        hwnd: HWND,
        actual_child_pid: u32,
        overlay: HWND,
        gate: Arc<SafetyGate>,
    ) -> Result<Self, Error> {
        let mut pid = 0;
        if unsafe { GetWindowThreadProcessId(hwnd, &mut pid) } == 0 || pid != actual_child_pid {
            return Err(Error::Owner);
        }
        let window = unsafe { NativeWindow::inspect(hwnd) }.map_err(|_| Error::Owner)?;
        window.validate_health().map_err(|_| Error::Owner)?;
        let initial_display = display(hwnd)?;
        let events = Events::register(hwnd, overlay, pid, gate.clone())?;
        let result = Self {
            window,
            hwnd,
            initial_display,
            events,
            gate,
        };
        result.window.validate_health().map_err(|_| Error::Owner)?;
        Ok(result)
    }
    pub fn identity(&self) -> WindowIdentity {
        self.window.identity()
    }
    pub fn placement(&self) -> Result<(i32, i32), Error> {
        let current = display(self.hwnd)?;
        Ok((
            current.monitor_rect[0]
                .checked_add(20)
                .ok_or(Error::Render)?,
            current.monitor_rect[1]
                .checked_add(20)
                .ok_or(Error::Render)?,
        ))
    }
    pub fn validate(&mut self) -> Result<GameObservation, Error> {
        self.events.drain()?;
        self.window.validate_health().map_err(|_| Error::Owner)?;
        let current = display(self.hwnd)?;
        if current != self.initial_display {
            // Revoke on move/resize/monitor/DPI changes; new placement is only
            // allowed after an independently observed foreground reacquisition.
            self.initial_display = current;
            self.gate.revoke();
            return Err(Error::Owner);
        }
        self.window.validate_health().map_err(|_| Error::Owner)?;
        self.events.drain()?;
        Ok(GameObservation::from_native(self.identity(), true))
    }
    pub fn foreground(
        &mut self,
        overlay: &OwnOverlay,
        overlay_hwnd: HWND,
    ) -> Result<Snapshot, Error> {
        let first = unsafe { GetForegroundWindow() };
        let game = self.validate()?;
        let foreground = if first == self.hwnd {
            Some(self.identity())
        } else if first == overlay_hwnd {
            overlay
                .validate(overlay.identity())
                .map_err(|_| Error::Owner)?;
            Some(overlay.identity())
        } else {
            None
        };
        if unsafe { GetForegroundWindow() } != first {
            return Err(Error::Owner);
        }
        self.events.drain()?;
        Ok(Snapshot { game, foreground })
    }
    fn return_once(&mut self, expected: WindowIdentity) -> Result<(), Error> {
        if expected != self.identity() {
            return Err(Error::Owner);
        }
        self.validate()?;
        if unsafe { SetForegroundWindow(self.hwnd) } == 0 {
            return Err(Error::Render);
        }
        self.validate()?;
        if unsafe { GetForegroundWindow() } != self.hwnd {
            return Err(Error::Owner);
        }
        Ok(())
    }
}

pub struct NativeFocus<'a> {
    pub game: &'a mut NativeGame,
    pub overlay: &'a OwnOverlay,
    pub overlay_hwnd: HWND,
    pub gate: &'a SafetyGate,
    pub epoch: u64,
}
impl ObservedDriver for NativeFocus<'_> {
    fn now(&self) -> Instant {
        Instant::now()
    }
    fn snapshot(&mut self) -> Result<Snapshot, Error> {
        self.game.foreground(self.overlay, self.overlay_hwnd)
    }
    fn focus_once(&mut self, request: FocusRequest) -> Result<(), Error> {
        self.game.validate()?;
        match self.overlay.validate(self.overlay.identity()) {
            Ok(()) | Err(crate::native_window_owner::Error::Hidden) => {}
            _ => return Err(Error::Owner),
        }
        if !self.gate.permissions(self.epoch, Instant::now()).0 {
            return Err(Error::SafetyExpired);
        }
        match request.kind() {
            FocusKind::OpenOverlay => {
                if request.target() != self.overlay.identity() {
                    return Err(Error::Owner);
                }
                unsafe {
                    ShowWindow(self.overlay_hwnd, SW_SHOWNOACTIVATE);
                }
                if !self.gate.permissions(self.epoch, Instant::now()).0 {
                    return Err(Error::SafetyExpired);
                }
                self.overlay
                    .focus_once(request.target())
                    .map_err(|_| Error::Render)
            }
            FocusKind::ReturnGame => self.game.return_once(request.target()),
        }
    }
}
