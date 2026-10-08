//! Renders INSIDE OUR OWN swap chain. No foreign process, hook, injection,
//! anti-cheat interaction, pixel capture, network or media owner exists.
use crate::owned_d3d11::{DrawPolicy, FullscreenTrial};
use std::{
    cell::Cell,
    ptr::{null, null_mut},
    time::{Duration, Instant},
};
use windows::{
    Win32::{
        Foundation::{HMODULE, HWND as ComHwnd},
        Graphics::{
            Direct3D::*,
            Direct3D11::*,
            Dxgi::{Common::*, *},
        },
    },
    core::{BOOL, Interface},
};
use windows_sys::{
    Win32::{
        Foundation::{HWND, LPARAM, LRESULT, WPARAM},
        System::LibraryLoader::GetModuleHandleW,
        UI::WindowsAndMessaging::*,
    },
    core::w,
};

#[derive(Clone, Copy, Default)]
struct Events {
    voice: u64,
    overlay: u64,
    fullscreen: u64,
    escape: bool,
    confirm: bool,
    clear: bool,
    resize: bool,
    width: u32,
    height: u32,
}
thread_local! {static EVENTS:Cell<Events>=Cell::new(Events::default());}
unsafe extern "system" fn window_proc(
    hwnd: HWND,
    message: u32,
    wparam: WPARAM,
    lparam: LPARAM,
) -> LRESULT {
    if message == WM_CLOSE {
        // Default WM_CLOSE destroys the HWND synchronously. Ask the owner loop
        // to exit instead, so Renderer Drop releases OUR fullscreen swapchain
        // before the owner's explicit DestroyWindow below. No foreign HWND.
        unsafe { PostQuitMessage(0) };
        return 0;
    }
    EVENTS.with(|events| {
        let mut event = events.get();
        match message {
            WM_KEYDOWN | WM_SYSKEYDOWN if lparam & (1 << 30) == 0 => {
                let counter = match wparam as u32 {
                    0x77 => Some(&mut event.voice),
                    0x7a => Some(&mut event.fullscreen),
                    0x4d if message == WM_SYSKEYDOWN => Some(&mut event.overlay),
                    0x1b => {
                        event.escape = true;
                        None
                    }
                    0x20 => {
                        event.confirm = true;
                        None
                    }
                    _ => None,
                };
                if let Some(counter) = counter {
                    if let Some(next) = counter.checked_add(1) {
                        *counter = next;
                    } else {
                        event.clear = true;
                    }
                }
            }
            WM_SIZE => {
                event.resize = true;
                event.width = (lparam as u32) & 0xffff;
                event.height = ((lparam as u32) >> 16) & 0xffff;
                if wparam as u32 == SIZE_MINIMIZED {
                    event.clear = true;
                }
            }
            WM_ACTIVATEAPP if wparam == 0 => event.clear = true,
            WM_DISPLAYCHANGE | WM_DPICHANGED => event.clear = true,
            _ => {}
        }
        events.set(event);
    });
    if message == WM_DESTROY {
        unsafe { PostQuitMessage(0) };
        return 0;
    }
    unsafe { DefWindowProcW(hwnd, message, wparam, lparam) }
}
#[derive(Debug)]
pub struct Error;
type Result<T> = std::result::Result<T, Error>;

struct Renderer {
    swap: IDXGISwapChain,
    device: ID3D11Device,
    context: ID3D11DeviceContext1,
    target: Option<ID3D11RenderTargetView>,
    width: u32,
    height: u32,
}
impl Renderer {
    fn create(hwnd: HWND) -> Result<Self> {
        let description = DXGI_SWAP_CHAIN_DESC {
            BufferDesc: DXGI_MODE_DESC {
                Width: 1280,
                Height: 720,
                Format: DXGI_FORMAT_R8G8B8A8_UNORM,
                ..Default::default()
            },
            SampleDesc: DXGI_SAMPLE_DESC {
                Count: 1,
                Quality: 0,
            },
            BufferUsage: DXGI_USAGE_RENDER_TARGET_OUTPUT,
            BufferCount: 2,
            OutputWindow: ComHwnd(hwnd),
            Windowed: BOOL(1),
            SwapEffect: DXGI_SWAP_EFFECT_DISCARD,
            Flags: DXGI_SWAP_CHAIN_FLAG_ALLOW_MODE_SWITCH.0 as u32,
        };
        let mut device = None;
        let mut context = None;
        let mut swap = None;
        unsafe {
            D3D11CreateDeviceAndSwapChain(
                None,
                D3D_DRIVER_TYPE_HARDWARE,
                HMODULE(null_mut()),
                D3D11_CREATE_DEVICE_BGRA_SUPPORT,
                Some(&[D3D_FEATURE_LEVEL_11_0]),
                D3D11_SDK_VERSION,
                Some(&description),
                Some(&mut swap),
                Some(&mut device),
                None,
                Some(&mut context),
            )
        }
        .map_err(|_| Error)?;
        let swap = swap.ok_or(Error)?;
        let device = device.ok_or(Error)?;
        let context: ID3D11DeviceContext1 = context.ok_or(Error)?.cast().map_err(|_| Error)?;
        let factory: IDXGIFactory = unsafe { swap.GetParent() }.map_err(|_| Error)?;
        unsafe { factory.MakeWindowAssociation(ComHwnd(hwnd), DXGI_MWA_NO_ALT_ENTER) }
            .map_err(|_| Error)?;
        let mut renderer = Self {
            swap,
            device,
            context,
            target: None,
            width: 1280,
            height: 720,
        };
        renderer.create_target()?;
        Ok(renderer)
    }
    fn create_target(&mut self) -> Result<()> {
        let texture: ID3D11Texture2D = unsafe { self.swap.GetBuffer(0) }.map_err(|_| Error)?;
        let mut target = None;
        unsafe {
            self.device
                .CreateRenderTargetView(&texture, None, Some(&mut target))
        }
        .map_err(|_| Error)?;
        self.target = Some(target.ok_or(Error)?);
        Ok(())
    }
    fn resize(&mut self, width: u32, height: u32) -> Result<()> {
        if width == 0 || height == 0 {
            return Ok(());
        }
        if width > 16384 || height > 16384 {
            return Err(Error);
        }
        unsafe {
            self.context.OMSetRenderTargets(None, None);
            self.context.ClearState();
        }
        self.target.take();
        unsafe {
            self.swap.ResizeBuffers(
                2,
                width,
                height,
                DXGI_FORMAT_R8G8B8A8_UNORM,
                DXGI_SWAP_CHAIN_FLAG_ALLOW_MODE_SWITCH,
            )
        }
        .map_err(|_| Error)?;
        self.width = width;
        self.height = height;
        self.create_target()
    }
    fn fullscreen(&self) -> Result<bool> {
        let mut state = BOOL(0);
        unsafe { self.swap.GetFullscreenState(Some(&mut state), None) }.map_err(|_| Error)?;
        Ok(state.as_bool())
    }
    fn mode(&self, fullscreen: bool) -> Result<bool> {
        unsafe { self.swap.SetFullscreenState(fullscreen, None) }.map_err(|_| Error)?;
        // The request/result code and window geometry are NOT evidence. Query
        // our actual swap chain and require it to match the requested state.
        let actual = self.fullscreen()?;
        if actual != fullscreen {
            Err(Error)
        } else {
            Ok(actual)
        }
    }
    fn rect(&self, x: i32, y: i32, w: i32, h: i32, color: [f32; 4]) -> Result<()> {
        let target = self.target.as_ref().ok_or(Error)?;
        let rect = windows::Win32::Foundation::RECT {
            left: x,
            top: y,
            right: x.checked_add(w).ok_or(Error)?,
            bottom: y.checked_add(h).ok_or(Error)?,
        };
        unsafe {
            self.context.ClearView(target, &color, Some(&[rect]));
        }
        Ok(())
    }
    fn text(&self, text: &str, x: i32, y: i32, color: [f32; 4]) -> Result<()> {
        for (index, character) in text.chars().take(48).enumerate() {
            for (row, bits) in glyph(character).into_iter().enumerate() {
                for col in 0..5 {
                    if bits & (1 << (4 - col)) != 0 {
                        self.rect(
                            x + index as i32 * 18 + col * 3,
                            y + row as i32 * 3,
                            3,
                            3,
                            color,
                        )?;
                    }
                }
            }
        }
        Ok(())
    }
    fn render(&mut self, policy: DrawPolicy, confirm: bool) -> Result<bool> {
        let target = self.target.as_ref().ok_or(Error)?;
        unsafe {
            self.context
                .ClearRenderTargetView(target, &[0.045, 0.065, 0.055, 1.]);
        }
        // Own scene, then HUD inside the SAME back buffer before Present.
        self.rect(320, 150, 440, 250, [0.10, 0.16, 0.13, 1.])?;
        self.text("OWN FIXTURE", 34, 30, [0.80, 0.88, 0.79, 1.])?;
        self.text(
            if policy.dxgi_fullscreen_reported() {
                "DXGI FULLSCREEN"
            } else {
                "WINDOWED"
            },
            34,
            self.height as i32 - 70,
            [0.75, 0.68, 0.40, 1.],
        )?;
        self.text(
            "F11 MODE F8 VOICE ALT M OVERLAY",
            34,
            self.height as i32 - 36,
            [0.66, 0.73, 0.67, 1.],
        )?;
        if policy.hud() {
            self.rect(20, 76, 268, 208, [0.10, 0.13, 0.10, 1.])?;
            self.text("VOICE FIXTURE", 36, 94, [0.78, 0.86, 0.78, 1.])?;
            for (row, label, color) in [
                (0, "ACTIVE", [0.37, 0.69, 0.42, 1.]),
                (1, "MUTED", [0.50, 0.53, 0.50, 1.]),
                (2, "LIVE", [0.79, 0.66, 0.27, 1.]),
            ] {
                self.rect(36, 134 + row * 44, 28, 28, color)?;
                self.text(label, 82, 136 + row * 44, color)?;
            }
        }
        if policy.overlay() {
            let x = (self.width as i32 - 720).max(0) / 2;
            let y = (self.height as i32 - 370).max(0) / 2;
            self.rect(x, y, 720, 370, [0.08, 0.11, 0.09, 1.])?;
            self.text("OWN OVERLAY", x + 28, y + 32, [0.80, 0.88, 0.80, 1.])?;
            self.text("SYNTHETIC ONLY", x + 28, y + 82, [0.68, 0.74, 0.68, 1.])?;
            self.text(
                "NO MEDIA NO NETWORK",
                x + 28,
                y + 140,
                [0.68, 0.74, 0.68, 1.],
            )?;
            self.text("ALT M CLOSE", x + 28, y + 228, [0.66, 0.70, 0.42, 1.])?;
        }
        if confirm {
            self.rect(300, 30, 380, 50, [0.19, 0.20, 0.09, 1.])?;
            self.text("SPACE TO KEEP", 320, 46, [0.88, 0.85, 0.54, 1.])?;
        }
        let result = unsafe { self.swap.Present(1, DXGI_PRESENT(0)) };
        if result.is_err() {
            return Err(Error);
        }
        // Positive occlusion/status codes are NOT success/pixel evidence.
        Ok(result.0 == 0)
    }
}
impl Drop for Renderer {
    fn drop(&mut self) {
        // Release OUR fullscreen swap chain before HWND/device destruction.
        let _ = unsafe { self.swap.SetFullscreenState(false, None) };
        unsafe {
            self.context.ClearState();
            self.context.Flush();
        }
        self.target.take();
    }
}

/// Authored tiny fixture bitmap alphabet, not vendored font/code.
fn glyph(c: char) -> [u8; 7] {
    match c {
        'A' => [14, 17, 17, 31, 17, 17, 17],
        'B' => [30, 17, 17, 30, 17, 17, 30],
        'C' => [15, 16, 16, 16, 16, 16, 15],
        'D' => [30, 17, 17, 17, 17, 17, 30],
        'E' => [31, 16, 16, 30, 16, 16, 31],
        'F' => [31, 16, 16, 30, 16, 16, 16],
        'G' => [15, 16, 16, 23, 17, 17, 15],
        'H' => [17, 17, 17, 31, 17, 17, 17],
        'I' => [31, 4, 4, 4, 4, 4, 31],
        'J' => [7, 2, 2, 2, 18, 18, 12],
        'K' => [17, 18, 20, 24, 20, 18, 17],
        'L' => [16, 16, 16, 16, 16, 16, 31],
        'M' => [17, 27, 21, 21, 17, 17, 17],
        'N' => [17, 25, 21, 19, 17, 17, 17],
        'O' => [14, 17, 17, 17, 17, 17, 14],
        'P' => [30, 17, 17, 30, 16, 16, 16],
        'Q' => [14, 17, 17, 17, 21, 18, 13],
        'R' => [30, 17, 17, 30, 20, 18, 17],
        'S' => [15, 16, 16, 14, 1, 1, 30],
        'T' => [31, 4, 4, 4, 4, 4, 4],
        'U' => [17, 17, 17, 17, 17, 17, 14],
        'V' => [17, 17, 17, 17, 17, 10, 4],
        'W' => [17, 17, 17, 21, 21, 27, 17],
        'X' => [17, 17, 10, 4, 10, 17, 17],
        'Y' => [17, 17, 10, 4, 4, 4, 4],
        'Z' => [31, 1, 2, 4, 8, 16, 31],
        '1' => [4, 12, 4, 4, 4, 4, 14],
        '8' => [14, 17, 17, 14, 17, 17, 14],
        _ => [0; 7],
    }
}

pub fn run() -> Result<()> {
    let module = unsafe { GetModuleHandleW(null()) };
    let class = w!("MnemaOwnedD3D11FullscreenFixture");
    let definition = WNDCLASSW {
        lpfnWndProc: Some(window_proc),
        hInstance: module,
        lpszClassName: class,
        ..Default::default()
    };
    if unsafe { RegisterClassW(&definition) } == 0 {
        return Err(Error);
    }
    let hwnd = unsafe {
        CreateWindowExW(
            0,
            class,
            w!("Mnema OWN D3D11 fixture — F11 mode, Escape windowed, F8 voice, Alt+M overlay"),
            WS_OVERLAPPEDWINDOW,
            120,
            80,
            1280,
            720,
            null_mut(),
            null_mut(),
            module,
            null(),
        )
    };
    if hwnd.is_null() {
        unsafe {
            UnregisterClassW(class, module);
        };
        return Err(Error);
    }
    unsafe {
        ShowWindow(hwnd, SW_SHOWNORMAL);
    }
    let result = (|| {
        let mut renderer = Renderer::create(hwnd)?;
        let mut policy = DrawPolicy::default();
        let mut previous = Events::default();
        let mut trial = FullscreenTrial::default();
        let mut frames = 0u64;
        let mut dxgi_fullscreen_hud_submissions = 0u64;
        loop {
            let mut message = MSG::default();
            for _ in 0..256 {
                if unsafe { PeekMessageW(&mut message, null_mut(), 0, 0, PM_REMOVE) } == 0 {
                    break;
                }
                if message.message == WM_QUIT {
                    println!(
                        "owned_fixture_frames={frames} dxgi_fullscreen_hud_submissions={dxgi_fullscreen_hud_submissions}; no third-party game or actual pixel qualification"
                    );
                    return Ok(());
                }
                unsafe {
                    TranslateMessage(&message);
                    DispatchMessageW(&message);
                }
            }
            let event = EVENTS.with(|slot| {
                let event = slot.get();
                let mut current = event;
                current.resize = false;
                current.clear = false;
                current.escape = false;
                current.confirm = false;
                slot.set(current);
                event
            });
            let own_foreground = unsafe { GetForegroundWindow() } == hwnd;
            let healthy = unsafe { IsWindow(hwnd) } != 0
                && unsafe { IsWindowVisible(hwnd) } != 0
                && unsafe { IsIconic(hwnd) } == 0;
            policy.observe(own_foreground, healthy, renderer.fullscreen().ok());
            if event.clear {
                policy.observe(false, false, None);
            }
            if own_foreground && healthy && !event.clear {
                for _ in 0..event.voice.saturating_sub(previous.voice).min(32) {
                    policy.toggle_voice();
                }
                for _ in 0..event.overlay.saturating_sub(previous.overlay).min(32) {
                    policy.toggle_overlay();
                }
                if event.fullscreen != previous.fullscreen {
                    let requested = !renderer.fullscreen()?;
                    // Arms before the native mode request. Uncertain query
                    // returns Err out of this loop and drops/releases renderer.
                    trial.request(requested, Instant::now(), || {
                        renderer.mode(requested).map(|_| ())
                    })?;
                }
            }
            if event.confirm && own_foreground {
                trial.confirm();
            }
            if event.escape || event.clear || trial.overdue(Instant::now()) {
                trial.request(false, Instant::now(), || renderer.mode(false).map(|_| ()))?;
            }
            if event.resize && healthy {
                renderer.resize(event.width, event.height)?;
            }
            previous = event;
            if !healthy || !own_foreground || EVENTS.with(|events| events.get().resize) {
                std::thread::sleep(Duration::from_millis(16));
                continue;
            }
            let before = renderer.fullscreen()?;
            policy.observe(true, true, Some(before));
            let presented = renderer.render(policy, trial.pending())?;
            let after = renderer.fullscreen()?;
            if presented {
                frames = frames.checked_add(1).ok_or(Error)?;
            }
            if presented
                && before
                && after
                && policy.hud()
                && unsafe { GetForegroundWindow() } == hwnd
            {
                dxgi_fullscreen_hud_submissions = dxgi_fullscreen_hud_submissions
                    .checked_add(1)
                    .ok_or(Error)?;
            }
        }
    })();
    // Renderer Drop runs inside closure before destroying its own HWND/class.
    if unsafe { IsWindow(hwnd) } != 0 {
        unsafe {
            DestroyWindow(hwnd);
        }
    }
    unsafe {
        UnregisterClassW(class, module);
    }
    result
}
