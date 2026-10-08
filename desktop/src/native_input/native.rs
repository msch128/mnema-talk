//! Dedicated hidden-window input owner. No rendering, capture, hooks or injection.
use super::{
    Availability, BoundedInput, Config, SafetyGate, SessionEvent, StampedActions, WATCHDOG_INTERVAL,
};
use std::{
    cell::{Cell, UnsafeCell},
    ffi::c_void,
    mem::size_of,
    ptr::{null, null_mut},
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
        mpsc::{Receiver, SyncSender},
    },
    time::Instant,
};
use windows_sys::Win32::UI::Input::KeyboardAndMouse::GetAsyncKeyState;
use windows_sys::{
    Win32::{
        Foundation::*,
        System::{
            LibraryLoader::GetModuleHandleW, RemoteDesktop::*, StationsAndDesktops::*, Threading::*,
        },
        UI::{Input::*, WindowsAndMessaging::*},
    },
    core::w,
};

static RUNNING: AtomicBool = AtomicBool::new(false);

/// Only the authoritative Rust core can send these. No WebView command exposure.
pub enum Command {
    Grant {
        epoch: u64,
        overlay: bool,
        ptt: bool,
    },
    CoreHeartbeat {
        epoch: u64,
        observed_at: Instant,
    },
    Reconfigure(Config),
    Revoke,
    Stop,
}
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Error {
    AlreadyRunning,
    Configuration,
    Window,
    Notification,
    RawRegistration,
    Timer,
    Watchdog,
    MessageLoop,
}
struct Running;
impl Drop for Running {
    fn drop(&mut self) {
        RUNNING.store(false, Ordering::SeqCst);
    }
}
struct WindowSlot {
    context: UnsafeCell<Context>,
    busy: Cell<bool>,
    gate: Arc<SafetyGate>,
}
struct CallbackGuard<'a>(&'a Cell<bool>);
impl Drop for CallbackGuard<'_> {
    fn drop(&mut self) {
        self.0.set(false);
    }
}

struct Context {
    gate: Arc<SafetyGate>,
    availability: Availability,
    input: BoundedInput,
    commands: Receiver<Command>,
    actions: SyncSender<StampedActions>,
    epoch: u64,
    keyboard_source: HANDLE,
    mouse_source: HANDLE,
    conflicted: bool,
}
impl Context {
    fn clear(&mut self) {
        self.gate.revoke();
        self.reset();
    }
    fn reset(&mut self) {
        self.epoch = self.gate.epoch();
        self.input.reset();
    }
    fn event(&mut self, event: SessionEvent) {
        if event == SessionEvent::DeviceChanged {
            self.keyboard_source = null_mut();
            self.mouse_source = null_mut();
        }
        self.availability.event(event, &self.gate);
        self.reset();
    }
    fn observe(&mut self) -> bool {
        if self.epoch != self.gate.epoch() {
            self.reset();
        }
        let (active, thread, desktop) = availability();
        let ready = self
            .availability
            .observe(active, thread, desktop, &self.gate);
        if self.epoch != self.gate.epoch() {
            self.reset();
        }
        ready && !self.conflicted
    }
    fn pulse(&mut self, hwnd: HWND) {
        if !owns_registration(hwnd) {
            self.conflicted = true;
            self.clear();
        }
        if self.observe() {
            self.gate.heartbeat(self.epoch, Instant::now());
        }
        if self.epoch != self.gate.epoch() {
            self.reset();
        }
        let (overlay, ptt) = self.input.primary_keys();
        let overlay_down = unsafe { GetAsyncKeyState(i32::from(overlay)) < 0 };
        let ptt_down = ptt.is_some_and(|key| unsafe { GetAsyncKeyState(i32::from(key)) < 0 });
        if self.input.reconcile_primary_up(overlay_down, ptt_down) {
            self.clear();
        }
        if self.input.reconcile_modifiers(current_modifiers()) {
            self.clear();
        }
        let now = Instant::now();
        if self
            .input
            .tick(self.gate.permissions(self.epoch, now), now)
            .release_ptt
        {
            self.gate.release();
        }
        // Bounded control drain, preventing a producer from monopolizing the pump.
        for _ in 0..16 {
            let command = match super::receive_control(&self.commands, &self.gate) {
                super::ControlPoll::Message(command) => command,
                super::ControlPoll::Idle => break,
                super::ControlPoll::OwnerGone => {
                    self.reset();
                    unsafe {
                        PostQuitMessage(0);
                    }
                    break;
                }
            };
            match command {
                Command::Grant {
                    epoch,
                    overlay,
                    ptt,
                } => {
                    if self.observe() && epoch == self.epoch {
                        self.gate.grant(epoch, overlay, ptt, Instant::now());
                    }
                }
                Command::Revoke => self.clear(),
                Command::Reconfigure(config) => {
                    self.clear();
                    if let Ok(input) = BoundedInput::new(config) {
                        self.input = input;
                    }
                }
                Command::CoreHeartbeat { epoch, observed_at } => {
                    // Preserve producer observation time; delayed queue delivery
                    // cannot turn an old GUI/core heartbeat into fresh evidence.
                    if observed_at <= Instant::now() {
                        self.gate.core_heartbeat(epoch, observed_at);
                    }
                }
                Command::Stop => {
                    self.clear();
                    unsafe {
                        PostQuitMessage(0);
                    }
                    break;
                }
            }
        }
    }
    fn key(&mut self, key: u16, down: bool) {
        if !self.input.interested(key) {
            return;
        }
        if !self.observe() {
            self.input.reset();
            return;
        }
        let now = Instant::now();
        let actions = self
            .input
            .event(key, down, self.gate.permissions(self.epoch, now), now);
        if self.input.modifiers() != current_modifiers() {
            self.clear();
            return; // Includes a modifier already held at startup.
        }
        if actions.release_ptt {
            self.gate.release();
        }
        if actions == super::shortcuts::Actions::default() {
            return;
        }
        let Some(stamped) = self.gate.stamp(self.epoch, actions) else {
            self.clear();
            return;
        };
        if self.actions.try_send(stamped).is_err() {
            self.clear();
        }
    }
    fn source(&mut self, kind: u32, handle: HANDLE) -> bool {
        if handle.is_null() {
            self.clear();
            return false;
        }
        let source = if kind == RIM_TYPEKEYBOARD {
            &mut self.keyboard_source
        } else {
            &mut self.mouse_source
        };
        if source.is_null() {
            *source = handle;
            true
        } else if *source == handle {
            true
        } else {
            self.conflicted = true;
            self.clear();
            false
        } // Multi-device ambiguity is unsupported.
    }
    fn raw(&mut self, handle: HRAWINPUT) {
        let mut raw = RAWINPUT::default();
        let mut bytes = size_of::<RAWINPUT>() as u32;
        // Aligned, initialized, fixed-size storage. No device information queries.
        let read = unsafe {
            GetRawInputData(
                handle,
                RID_INPUT,
                (&mut raw as *mut RAWINPUT).cast(),
                &mut bytes,
                size_of::<RAWINPUTHEADER>() as u32,
            )
        };
        if read == u32::MAX
            || read < size_of::<RAWINPUTHEADER>() as u32
            || read != raw.header.dwSize
            || read > size_of::<RAWINPUT>() as u32
        {
            self.clear();
            return;
        }
        match raw.header.dwType {
            RIM_TYPEKEYBOARD
                if read >= (size_of::<RAWINPUTHEADER>() + size_of::<RAWKEYBOARD>()) as u32 =>
            {
                let keyboard = unsafe { raw.data.keyboard };
                if keyboard.VKey == 0xff
                    || keyboard.MakeCode == 0xff
                    || keyboard.Flags & !(RI_KEY_BREAK | RI_KEY_E0 | RI_KEY_E1) as u16 != 0
                {
                    self.clear();
                    return;
                }
                let key = match keyboard.VKey {
                    0x10 => match keyboard.MakeCode {
                        0x2a => 0xa0,
                        0x36 => 0xa1,
                        _ => {
                            self.clear();
                            return;
                        }
                    },
                    0x11 => {
                        if keyboard.Flags & RI_KEY_E0 as u16 != 0 {
                            0xa3
                        } else {
                            0xa2
                        }
                    }
                    0x12 => {
                        if keyboard.Flags & RI_KEY_E0 as u16 != 0 {
                            0xa5
                        } else {
                            0xa4
                        }
                    }
                    key => key,
                };
                if !self.input.interested(key) {
                    return;
                }
                if !self.source(RIM_TYPEKEYBOARD, raw.header.hDevice) {
                    return;
                }
                self.key(key, keyboard.Flags & RI_KEY_BREAK as u16 == 0);
            }
            RIM_TYPEMOUSE
                if read >= (size_of::<RAWINPUTHEADER>() + size_of::<RAWMOUSE>()) as u32 =>
            {
                let flags = unsafe { raw.data.mouse.Anonymous.Anonymous.usButtonFlags };
                // Mouse movement/coordinates/wheel are never read or retained.
                if flags & 0x3c0 == 0 {
                    return;
                }
                if !self.source(RIM_TYPEMOUSE, raw.header.hDevice) {
                    return;
                }
                for (flag, key, down) in [
                    (64, 0x05, true),
                    (128, 0x05, false),
                    (256, 0x06, true),
                    (512, 0x06, false),
                ] {
                    if flags & flag != 0 {
                        self.key(key, down);
                    }
                }
            }
            _ => {} // Unsupported HID bodies are discarded.
        }
    }
}

fn current_modifiers() -> super::shortcuts::Modifiers {
    let down = |key| unsafe { GetAsyncKeyState(key) < 0 };
    super::shortcuts::Modifiers {
        alt: down(0xa4) || down(0xa5),
        control: down(0xa2) || down(0xa3),
        shift: down(0xa0) || down(0xa1),
        windows: down(0x5b) || down(0x5c),
    }
}

fn receives_input(handle: HANDLE) -> bool {
    if handle.is_null() {
        return false;
    }
    let mut value = 0i32;
    let mut needed = 0;
    unsafe {
        GetUserObjectInformationW(
            handle,
            UOI_IO,
            (&mut value as *mut i32).cast(),
            size_of::<i32>() as u32,
            &mut needed,
        ) != 0
            && needed == size_of::<i32>() as u32
            && value != 0
    }
}
fn availability() -> (bool, bool, bool) {
    unsafe {
        let mut buffer = null_mut();
        let mut bytes = 0;
        let success = WTSQuerySessionInformationW(
            WTS_CURRENT_SERVER_HANDLE,
            WTS_CURRENT_SESSION,
            WTSConnectState,
            &mut buffer,
            &mut bytes,
        );
        let active = success != 0
            && !buffer.is_null()
            && bytes == size_of::<WTS_CONNECTSTATE_CLASS>() as u32
            && buffer.cast::<WTS_CONNECTSTATE_CLASS>().read_unaligned() == WTSActive;
        if !buffer.is_null() {
            WTSFreeMemory(buffer.cast());
        }
        let thread = receives_input(GetThreadDesktop(GetCurrentThreadId())); // borrowed, never closed
        let desktop = OpenInputDesktop(0, 0, DESKTOP_READOBJECTS);
        let mut input = receives_input(desktop);
        if !desktop.is_null() && CloseDesktop(desktop) == 0 {
            input = false;
        }
        (active, thread, input)
    }
}
fn registrations() -> Option<([RAWINPUTDEVICE; 32], usize)> {
    let mut devices = [RAWINPUTDEVICE::default(); 32];
    let mut count = devices.len() as u32;
    let read = unsafe {
        GetRegisteredRawInputDevices(
            devices.as_mut_ptr(),
            &mut count,
            size_of::<RAWINPUTDEVICE>() as u32,
        )
    };
    if read == u32::MAX || read > devices.len() as u32 {
        None
    } else {
        Some((devices, read as usize))
    }
}
fn selected(device: &RAWINPUTDEVICE) -> bool {
    device.usUsagePage == 1 && matches!(device.usUsage, 2 | 6)
}
fn owns_registration(hwnd: HWND) -> bool {
    let Some((devices, count)) = registrations() else {
        return false;
    };
    let mut count_selected = 0;
    for device in devices[..count].iter().filter(|device| selected(device)) {
        count_selected += 1;
        if device.hwndTarget != hwnd || device.dwFlags != RIDEV_INPUTSINK | RIDEV_DEVNOTIFY {
            return false;
        }
    }
    count_selected == 2
}
fn register_raw(hwnd: HWND) -> bool {
    let Some((existing, count)) = registrations() else {
        return false;
    };
    if existing[..count].iter().any(selected) {
        return false;
    } // Never steal WebView/app registration.
    let devices = [2, 6].map(|usage| RAWINPUTDEVICE {
        usUsagePage: 1,
        usUsage: usage,
        dwFlags: RIDEV_INPUTSINK | RIDEV_DEVNOTIFY,
        hwndTarget: hwnd,
    });
    unsafe {
        RegisterRawInputDevices(
            devices.as_ptr(),
            devices.len() as u32,
            size_of::<RAWINPUTDEVICE>() as u32,
        ) != 0
    }
}
fn unregister_raw(hwnd: HWND) {
    let Some((existing, count)) = registrations() else {
        return;
    };
    for device in existing[..count]
        .iter()
        .filter(|device| selected(device) && device.hwndTarget == hwnd)
    {
        let remove = RAWINPUTDEVICE {
            dwFlags: RIDEV_REMOVE,
            hwndTarget: null_mut(),
            ..*device
        };
        unsafe {
            RegisterRawInputDevices(&remove, 1, size_of::<RAWINPUTDEVICE>() as u32);
        }
    }
}

unsafe extern "system" fn window_proc(
    hwnd: HWND,
    message: u32,
    wparam: WPARAM,
    lparam: LPARAM,
) -> LRESULT {
    if message == WM_NCCREATE {
        let create = unsafe { &*(lparam as *const CREATESTRUCTW) };
        unsafe {
            SetWindowLongPtrW(hwnd, GWLP_USERDATA, create.lpCreateParams as isize);
        }
    }
    let pointer = unsafe { GetWindowLongPtrW(hwnd, GWLP_USERDATA) } as *const WindowSlot;
    if !pointer.is_null() {
        // Shared slot/Cell remains distinct from the exclusively borrowed
        // Context. Reentrant delivery revokes without aliasing its mutable state.
        let slot = unsafe { &*pointer };
        if slot.busy.replace(true) {
            slot.gate.revoke();
            return unsafe { DefWindowProcW(hwnd, message, wparam, lparam) };
        }
        let _guard = CallbackGuard(&slot.busy);
        let context = unsafe { &mut *slot.context.get() };
        match message {
            WM_TIMER => context.pulse(hwnd),
            WM_INPUT => context.raw(lparam as HRAWINPUT),
            WM_INPUT_DEVICE_CHANGE => context.event(SessionEvent::DeviceChanged),
            WM_DISPLAYCHANGE | WM_DPICHANGED => context.event(SessionEvent::DisplayChanged),
            WM_WTSSESSION_CHANGE => context.event(match wparam as u32 {
                WTS_SESSION_LOCK => SessionEvent::Lock,
                WTS_SESSION_UNLOCK => SessionEvent::Unlock,
                WTS_CONSOLE_CONNECT | WTS_REMOTE_CONNECT => SessionEvent::Connect,
                WTS_CONSOLE_DISCONNECT | WTS_REMOTE_DISCONNECT | WTS_SESSION_LOGOFF => {
                    SessionEvent::Disconnect
                }
                _ => SessionEvent::DisplayChanged,
            }),
            WM_POWERBROADCAST => context.event(match wparam as u32 {
                PBT_APMSUSPEND => SessionEvent::Suspend,
                PBT_APMRESUMEAUTOMATIC | PBT_APMRESUMESUSPEND => SessionEvent::Resume,
                _ => SessionEvent::DisplayChanged,
            }),
            WM_CLOSE | WM_DESTROY => {
                context.clear();
                unsafe {
                    PostQuitMessage(0);
                }
            }
            WM_NCDESTROY => {
                context.clear();
                unsafe {
                    SetWindowLongPtrW(hwnd, GWLP_USERDATA, 0);
                }
            }
            _ => {}
        }
    }
    // Required WM_INPUT foreground cleanup; input is never swallowed.
    unsafe { DefWindowProcW(hwnd, message, wparam, lparam) }
}

/// Run on a dedicated native thread; caller creates a bounded action channel.
/// Blocking by design: Tauri's GUI thread must never call this directly.
pub fn run(
    config: Config,
    gate: Arc<SafetyGate>,
    commands: Receiver<Command>,
    actions: SyncSender<StampedActions>,
) -> Result<(), Error> {
    let input = BoundedInput::new(config).map_err(|_| Error::Configuration)?;
    RUNNING
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .map_err(|_| Error::AlreadyRunning)?;
    let _running = Running;
    gate.revoke();
    let context = Context {
        epoch: gate.epoch(),
        gate: gate.clone(),
        availability: Availability::default(),
        input,
        commands,
        actions,
        keyboard_source: null_mut(),
        mouse_source: null_mut(),
        conflicted: false,
    };
    let mut slot = Box::new(WindowSlot {
        context: UnsafeCell::new(context),
        busy: Cell::new(false),
        gate: gate.clone(),
    });
    unsafe {
        let module = GetModuleHandleW(null());
        let class = w!("MnemaPrivateBoundedInputProbe");
        let definition = WNDCLASSW {
            lpfnWndProc: Some(window_proc),
            hInstance: module,
            lpszClassName: class,
            ..Default::default()
        };
        if RegisterClassW(&definition) == 0 {
            return Err(Error::Window);
        }
        // HWND_MESSAGE misses broadcasts. This top-level window stays hidden,
        // has no activating/visible style and is never shown.
        let hwnd = CreateWindowExW(
            WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW,
            class,
            w!(""),
            0,
            0,
            0,
            0,
            0,
            null_mut(),
            null_mut(),
            module,
            (&mut *slot as *mut WindowSlot).cast::<c_void>(),
        );
        if hwnd.is_null() {
            UnregisterClassW(class, module);
            return Err(Error::Window);
        }
        let mut result = Ok(());
        let notified = WTSRegisterSessionNotification(hwnd, NOTIFY_FOR_THIS_SESSION) != 0;
        let raw = notified && register_raw(hwnd);
        let timer = raw && SetTimer(hwnd, 1, 25, None) != 0;
        if !notified {
            result = Err(Error::Notification);
        } else if !raw {
            result = Err(Error::RawRegistration);
        } else if !timer {
            result = Err(Error::Timer);
        }
        if timer {
            let stop = Arc::new(AtomicBool::new(false));
            let worker_stop = stop.clone();
            let worker_gate = gate.clone();
            let worker = std::thread::Builder::new()
                .name("mnema-input-watchdog".into())
                .spawn(move || {
                    while !worker_stop.load(Ordering::SeqCst) {
                        worker_gate.watchdog(Instant::now());
                        std::thread::sleep(WATCHDOG_INTERVAL);
                    }
                    worker_gate.revoke();
                });
            if let Ok(worker) = worker {
                let mut message = MSG::default();
                loop {
                    let read = GetMessageW(&mut message, null_mut(), 0, 0);
                    if read <= 0 {
                        if read < 0 {
                            result = Err(Error::MessageLoop);
                        }
                        break;
                    }
                    TranslateMessage(&message);
                    DispatchMessageW(&message);
                }
                stop.store(true, Ordering::SeqCst);
                if worker.join().is_err() {
                    result = Err(Error::Watchdog);
                }
            } else {
                result = Err(Error::Watchdog);
            }
            KillTimer(hwnd, 1);
        }
        gate.revoke();
        unregister_raw(hwnd);
        if notified {
            WTSUnRegisterSessionNotification(hwnd);
        }
        (*slot.context.get()).conflicted = true;
        if DestroyWindow(hwnd) == 0 {
            // Never free callback data while a failed destruction leaves a live
            // native window referencing it. Disabled small leak is fail-closed.
            Box::leak(slot);
            result = Err(Error::Window);
        }
        UnregisterClassW(class, module);
        result
    }
}
