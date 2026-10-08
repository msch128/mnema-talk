//! Explicit, separately launched native fixture. The pipe is private to the
//! parent-owned child; packets contain only synthetic state/sequence/thread ID.
use crate::gaming_fixture::{
    Error,
    fixture_control::{HELLO_SIZE, ParentHello},
    fixture_session::{PACKET_SIZE, Packet, SyntheticSession},
};
use crate::native_input::SafetyGate;
use std::{
    cell::{Cell, RefCell},
    io::{Read, Write},
    process::{Child, Command, Stdio},
    ptr::{null, null_mut},
    sync::{
        Arc,
        mpsc::{self, Receiver, TryRecvError},
    },
    thread,
    time::{Duration, Instant},
};
use windows_sys::{
    Win32::{
        Foundation::*,
        Graphics::Gdi::*,
        System::{
            LibraryLoader::GetModuleHandleW,
            Threading::{GetCurrentProcessId, GetCurrentThreadId},
        },
        UI::WindowsAndMessaging::*,
    },
    core::w,
};

thread_local! {
    static FLAGS:Cell<u8>=const {Cell::new(1)};
    static VOICE_EPOCH:Cell<u64>=const {Cell::new(1)};
    static SEQUENCE:Cell<u64>=const {Cell::new(0)};
    static PARENT:RefCell<Option<ParentControl>>=const {RefCell::new(None)};
}
struct ParentControl {
    handle: HANDLE,
    hello: ParentHello,
    os_parent: u32,
}
impl Drop for ParentControl {
    fn drop(&mut self) {
        unsafe {
            CloseHandle(self.handle);
        }
    }
}
fn process_creation(handle: HANDLE) -> Result<u64, Error> {
    use windows_sys::Win32::System::Threading::GetProcessTimes;
    let mut created = FILETIME::default();
    let mut exited = FILETIME::default();
    let mut kernel = FILETIME::default();
    let mut user = FILETIME::default();
    if unsafe { GetProcessTimes(handle, &mut created, &mut exited, &mut kernel, &mut user) } == 0 {
        return Err(Error::Owner);
    }
    Ok((u64::from(created.dwHighDateTime) << 32) | u64::from(created.dwLowDateTime))
}
fn os_spawning_parent() -> Result<u32, Error> {
    use windows_sys::Win32::System::Diagnostics::ToolHelp::*;
    let snapshot = unsafe { CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0) };
    if snapshot == INVALID_HANDLE_VALUE {
        return Err(Error::Owner);
    }
    let mut entry = PROCESSENTRY32W {
        dwSize: std::mem::size_of::<PROCESSENTRY32W>() as u32,
        ..Default::default()
    };
    let own = unsafe { GetCurrentProcessId() };
    let mut parent = None;
    let mut valid = unsafe { Process32FirstW(snapshot, &mut entry) } != 0;
    // One bounded setup snapshot ONLY to obtain OUR parent PID. No game process
    // inventory, executable name, module, path or other entry is retained/logged.
    for _ in 0..65536 {
        if !valid {
            break;
        }
        if entry.th32ProcessID == own {
            parent = Some(entry.th32ParentProcessID);
            break;
        }
        valid = unsafe { Process32NextW(snapshot, &mut entry) } != 0;
    }
    unsafe {
        CloseHandle(snapshot);
    }
    parent
        .filter(|&pid| pid != 0 && pid != own)
        .ok_or(Error::Owner)
}
impl ParentControl {
    fn read_owned_pipe() -> Result<Self, Error> {
        use windows_sys::Win32::{
            Storage::FileSystem::{FILE_TYPE_PIPE, GetFileType},
            System::{
                Console::{GetStdHandle, STD_INPUT_HANDLE},
                Threading::*,
            },
        };
        let stdin = unsafe { GetStdHandle(STD_INPUT_HANDLE) };
        if stdin.is_null()
            || stdin == INVALID_HANDLE_VALUE
            || unsafe { GetFileType(stdin) } != FILE_TYPE_PIPE
        {
            return Err(Error::Owner);
        }
        let mut bytes = [0; HELLO_SIZE];
        std::io::stdin()
            .read_exact(&mut bytes)
            .map_err(|_| Error::Owner)?;
        let hello = ParentHello::decode(bytes).ok_or(Error::Owner)?;
        let actual_parent = os_spawning_parent()?;
        if hello.pid != actual_parent {
            return Err(Error::Owner);
        }
        let handle = unsafe {
            OpenProcess(
                PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SYNCHRONIZE,
                0,
                actual_parent,
            )
        };
        if handle.is_null() {
            return Err(Error::Owner);
        }
        let parent = Self {
            handle,
            hello,
            os_parent: actual_parent,
        };
        parent.validate()?;
        Ok(parent)
    }
    fn validate(&self) -> Result<(), Error> {
        use windows_sys::Win32::System::Threading::{GetProcessId, WaitForSingleObject};
        if unsafe { WaitForSingleObject(self.handle, 0) } != WAIT_TIMEOUT
            || unsafe { GetProcessId(self.handle) } != self.hello.pid
            || !self
                .hello
                .peer_matches(self.os_parent, process_creation(self.handle)?, unsafe {
                    GetCurrentProcessId()
                })
        {
            return Err(Error::Owner);
        }
        Ok(())
    }
    fn permit_on_real_shortcut(&self, hwnd: HWND, message: u32, wparam: WPARAM) {
        use crate::native_input::shortcuts::Modifiers;
        use windows_sys::Win32::UI::Input::KeyboardAndMouse::GetAsyncKeyState;
        let key = match message {
            WM_KEYDOWN | WM_SYSKEYDOWN => wparam as u16,
            WM_XBUTTONDOWN => match (wparam >> 16) & 0xffff {
                1 => 5,
                2 => 6,
                _ => return,
            },
            _ => return,
        };
        if key != self.hello.overlay.key()
            || unsafe { GetForegroundWindow() } != hwnd
            || !FLAGS.with(|flags| flags.get() & 1 != 0)
            || self.validate().is_err()
            || unsafe { GetAsyncKeyState(i32::from(key)) } >= 0
        {
            return;
        }
        let observed = Modifiers {
            alt: unsafe { GetAsyncKeyState(0x12) } < 0,
            control: unsafe { GetAsyncKeyState(0x11) } < 0,
            shift: unsafe { GetAsyncKeyState(0x10) } < 0,
            windows: unsafe { GetAsyncKeyState(0x5b) } < 0 || unsafe { GetAsyncKeyState(0x5c) } < 0,
        };
        if observed != self.hello.overlay.modifiers() {
            return;
        }
        // Documented, cooperating FOREGROUND fixture grants the exact retained
        // actual spawning parent only. Not elevation, ASFW_ANY, input synthesis,
        // an external-game bypass or a fallback focus retry.
        unsafe {
            AllowSetForegroundWindow(self.hello.pid);
        }
    }
}
struct ParentInstalled;
impl Drop for ParentInstalled {
    fn drop(&mut self) {
        PARENT.with(|slot| slot.borrow_mut().take());
    }
}
unsafe extern "system" fn child_window(
    hwnd: HWND,
    message: u32,
    wparam: WPARAM,
    lparam: LPARAM,
) -> LRESULT {
    PARENT.with(|parent| {
        if let Ok(parent) = parent.try_borrow()
            && let Some(parent) = parent.as_ref()
        {
            parent.permit_on_real_shortcut(hwnd, message, wparam);
        }
    });
    match message {
        WM_TIMER => {
            let Some(next) = SEQUENCE.with(|seq| {
                let next = seq.get().checked_add(1)?;
                seq.set(next);
                Some(next)
            }) else {
                unsafe { PostQuitMessage(1) };
                return 0;
            };
            let bytes = Packet {
                sequence: next,
                thread: unsafe { GetCurrentThreadId() },
                flags: FLAGS.with(Cell::get),
                voice_epoch: VOICE_EPOCH.with(Cell::get),
            }
            .encode();
            if std::io::stdout().lock().write_all(&bytes).is_err() {
                unsafe { PostQuitMessage(1) };
            }
            unsafe {
                InvalidateRect(hwnd, null(), 0);
            }
            0
        }
        WM_KEYDOWN if lparam & (1 << 30) == 0 => {
            let bit = match wparam as u32 {
                VK_F8_VALUE => 1,
                VK_F9_VALUE => 2,
                VK_F10_VALUE => 4,
                VK_F11_VALUE => 8,
                _ => 0,
            };
            if bit == 1 {
                let incremented = VOICE_EPOCH.with(|epoch| {
                    let Some(next) = epoch.get().checked_add(1) else {
                        return false;
                    };
                    epoch.set(next);
                    true
                });
                if !incremented {
                    unsafe { PostQuitMessage(1) };
                    return 0;
                }
            }
            if bit != 0 {
                FLAGS.with(|flags| flags.set(flags.get() ^ bit));
            }
            unsafe { DefWindowProcW(hwnd, message, wparam, lparam) }
        }
        WM_PAINT => {
            let mut paint = PAINTSTRUCT::default();
            let dc = unsafe { BeginPaint(hwnd, &mut paint) };
            let mut rect = RECT::default();
            unsafe {
                GetClientRect(hwnd, &mut rect);
            }
            let brush = unsafe { CreateSolidBrush(0x00211e15) };
            if !brush.is_null() {
                unsafe {
                    FillRect(dc, &rect, brush);
                    DeleteObject(brush as _);
                }
            }
            unsafe {
                SetBkMode(dc, TRANSPARENT as i32);
                SetTextColor(dc, 0x00eee8d5);
            }
            let flags = FLAGS.with(Cell::get);
            let text = format!(
                "MNEMA OWN COOPERATING GAME FIXTURE\n\nThis is a synthetic window, not World of Warcraft / Genshin / Wardogs.\nNo microphone, network, stream or encryption session exists.\n\nF8: synthetic voice join/leave (currently {})\nF9: speaking fixture ({})\nF10: muted fixture ({})\nF11: sharing fixture ({})\n\nAlt+M: native overlay toggle, only while synthetic voice is joined.\nAlt+V: synthetic PTT latch; no microphone is captured.\n\nTry Alt-Tab, minimize, move/resize and close this window.\nWindowed fixture only; true fullscreen remains unqualified.",
                flags & 1 != 0,
                flags & 2 != 0,
                flags & 4 != 0,
                flags & 8 != 0
            );
            let mut wide: Vec<u16> = text.encode_utf16().collect();
            rect.left += 26;
            rect.top += 28;
            unsafe {
                DrawTextW(
                    dc,
                    wide.as_mut_ptr(),
                    wide.len() as i32,
                    &mut rect,
                    DT_LEFT | DT_TOP | DT_WORDBREAK,
                );
                EndPaint(hwnd, &paint);
            }
            0
        }
        WM_DESTROY => {
            unsafe { PostQuitMessage(0) };
            0
        }
        _ => unsafe { DefWindowProcW(hwnd, message, wparam, lparam) },
    }
}
const VK_F8_VALUE: u32 = 0x77;
const VK_F9_VALUE: u32 = 0x78;
const VK_F10_VALUE: u32 = 0x79;
const VK_F11_VALUE: u32 = 0x7a;

/// Manual CLI branch only. The parent must own stdout and actual Child::id().
pub fn run_child() -> Result<(), Error> {
    let parent = ParentControl::read_owned_pipe()?;
    PARENT.with(|slot| *slot.borrow_mut() = Some(parent));
    let _installed = ParentInstalled;
    unsafe {
        let module = GetModuleHandleW(null());
        let class = w!("MnemaCooperatingOwnedGameFixtureV1");
        let definition = WNDCLASSW {
            lpfnWndProc: Some(child_window),
            hInstance: module,
            lpszClassName: class,
            ..Default::default()
        };
        if RegisterClassW(&definition) == 0 {
            return Err(Error::Render);
        }
        let hwnd = CreateWindowExW(
            0,
            class,
            w!("Mnema own game fixture — not a real game"),
            WS_OVERLAPPEDWINDOW,
            80,
            80,
            1000,
            680,
            null_mut(),
            null_mut(),
            module,
            null(),
        );
        if hwnd.is_null() {
            UnregisterClassW(class, module);
            return Err(Error::Render);
        }
        ShowWindow(hwnd, SW_SHOWNORMAL);
        if SetTimer(hwnd, 1, 50, None) == 0 {
            DestroyWindow(hwnd);
            UnregisterClassW(class, module);
            return Err(Error::Render);
        }
        let mut message = MSG::default();
        let result = loop {
            let got = GetMessageW(&mut message, null_mut(), 0, 0);
            if got == 0 {
                break Ok(());
            }
            if got < 0 {
                break Err(Error::Render);
            }
            TranslateMessage(&message);
            DispatchMessageW(&message);
        };
        if IsWindow(hwnd) != 0 {
            DestroyWindow(hwnd);
        }
        UnregisterClassW(class, module);
        PARENT.with(|slot| slot.borrow_mut().take());
        result
    }
}

pub struct ChildFixture {
    child: Child,
    packets: Receiver<(Packet, Instant)>,
    reader: Option<thread::JoinHandle<()>>,
    gate: Arc<SafetyGate>,
    session: SyntheticSession,
}
impl ChildFixture {
    /// This explicit fixture-only launch is never automatic for normal clients.
    pub fn spawn(
        gate: Arc<SafetyGate>,
        overlay: crate::native_input::shortcuts::Binding,
    ) -> Result<Self, Error> {
        let exe = std::env::current_exe().map_err(|_| Error::Render)?;
        let header = ParentHello {
            pid: unsafe { GetCurrentProcessId() },
            creation: process_creation(unsafe {
                windows_sys::Win32::System::Threading::GetCurrentProcess()
            })?,
            overlay,
        };
        let mut child = Command::new(exe)
            .arg("--cooperating-fixture-child")
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|_| Error::Render)?;
        let write = child
            .stdin
            .take()
            .ok_or(Error::Owner)
            .and_then(|mut pipe| pipe.write_all(&header.encode()).map_err(|_| Error::Owner));
        if write.is_err() {
            let _ = child.kill();
            let _ = child.wait();
            return Err(Error::Owner);
        }
        let Some(mut pipe) = child.stdout.take() else {
            let _ = child.kill();
            let _ = child.wait();
            return Err(Error::Render);
        };
        let (sender, packets) = mpsc::sync_channel(32);
        let child_gate = gate.clone();
        let reader = thread::Builder::new()
            .name("mnema-fixture-pipe".into())
            .spawn(move || {
                loop {
                    let mut bytes = [0; PACKET_SIZE];
                    if pipe.read_exact(&mut bytes).is_err() {
                        break;
                    }
                    let Some(packet) = Packet::decode(bytes) else {
                        break;
                    };
                    if sender.try_send((packet, Instant::now())).is_err() {
                        break;
                    }
                }
                child_gate.revoke();
            });
        let reader = match reader {
            Ok(reader) => reader,
            Err(_) => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(Error::Render);
            }
        };
        let first = packets.recv_timeout(Duration::from_secs(10));
        let (first, received) = match first {
            Ok(first) => first,
            Err(_) => {
                gate.revoke();
                let _ = child.kill();
                let _ = child.wait();
                let _ = reader.join();
                return Err(Error::Render);
            }
        };
        // Thread came through the pipe of the actual spawned child, but window
        // enumeration below still compares its real OS PID, not supplied data.
        let session = SyntheticSession::from_owned_child(first, received);
        Ok(Self {
            child,
            packets,
            reader: Some(reader),
            gate,
            session,
        })
    }
    pub fn poll(&mut self, now: Instant) -> Result<(), Error> {
        if self.child.try_wait().map_err(|_| Error::Owner)?.is_some() {
            self.session.retire();
            self.gate.revoke();
            return Err(Error::Owner);
        }
        loop {
            match self.packets.try_recv() {
                Ok((packet, received)) => {
                    if !self.session.observe(packet, received, now) {
                        self.gate.revoke();
                        return Err(Error::Owner);
                    }
                }
                Err(TryRecvError::Empty) => break,
                Err(TryRecvError::Disconnected) => {
                    self.session.retire();
                    self.gate.revoke();
                    return Err(Error::Owner);
                }
            }
        }
        Ok(())
    }
    pub fn session(&self) -> &SyntheticSession {
        &self.session
    }
    pub fn game_window(&self) -> Result<HWND, Error> {
        struct Find {
            pid: u32,
            hwnd: HWND,
            multiple: bool,
        }
        unsafe extern "system" fn find(hwnd: HWND, data: LPARAM) -> windows_sys::core::BOOL {
            let found = unsafe { &mut *(data as *mut Find) };
            let mut pid = 0;
            unsafe {
                GetWindowThreadProcessId(hwnd, &mut pid);
            }
            if pid != found.pid {
                return 1;
            }
            let mut class = [0u16; 64];
            let length = unsafe { GetClassNameW(hwnd, class.as_mut_ptr(), class.len() as i32) };
            let expected: Vec<u16> = "MnemaCooperatingOwnedGameFixtureV1"
                .encode_utf16()
                .collect();
            if length < 0 || class[..length as usize] != expected {
                return 1;
            }
            if !found.hwnd.is_null() {
                found.multiple = true;
            } else {
                found.hwnd = hwnd;
            }
            1
        }
        let mut found = Find {
            pid: self.child.id(),
            hwnd: null_mut(),
            multiple: false,
        };
        unsafe {
            EnumThreadWindows(
                self.session.thread(),
                Some(find),
                (&mut found as *mut Find) as LPARAM,
            );
        }
        if found.hwnd.is_null() || found.multiple || found.pid == unsafe { GetCurrentProcessId() } {
            Err(Error::Owner)
        } else {
            Ok(found.hwnd)
        }
    }
    pub fn pid(&self) -> u32 {
        self.child.id()
    }
}
impl Drop for ChildFixture {
    fn drop(&mut self) {
        self.session.retire();
        self.gate.revoke();
        // This is only the child we explicitly launched and retained. Never
        // kill a discovered real game or another process selected by a PID.
        let _ = self.child.kill();
        let _ = self.child.wait();
        if let Some(reader) = self.reader.take() {
            let _ = reader.join();
        }
    }
}
