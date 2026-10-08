//! Local Tauri surfaces. All calls belong on the Tauri/native creating thread.
use crate::gaming_fixture::{Error, Frame, Surfaces};
#[cfg(windows)]
use crate::native_window_owner::native::{NativeWindow, OwnOverlay};
use tauri::{Emitter, Runtime, WebviewUrl, WebviewWindow, WebviewWindowBuilder};

pub struct GuiSurfaces<R: Runtime> {
    widget: WebviewWindow<R>,
    overlay: WebviewWindow<R>,
    #[cfg(windows)]
    widget_guard: OwnOverlay,
    #[cfg(windows)]
    overlay_guard: OwnOverlay,
    released: bool,
}
impl<R: Runtime> GuiSurfaces<R> {
    #[cfg(all(windows, feature = "gaming-fixture"))]
    pub(crate) fn fixture_overlay_guard(&self) -> &OwnOverlay {
        &self.overlay_guard
    }
    #[cfg(all(windows, feature = "gaming-fixture"))]
    pub(crate) fn fixture_overlay_hwnd(
        &self,
    ) -> Result<windows_sys::Win32::Foundation::HWND, Error> {
        Ok(self.overlay.hwnd().map_err(|_| Error::Render)?.0 as _)
    }
    #[cfg(all(windows, feature = "gaming-fixture"))]
    pub(crate) fn prepare_synthetic(&mut self, frame: &Frame) -> Result<(), Error> {
        // Hiding the committed foreground overlay on every roster update would
        // drop focus back to a foreign app and destroy its own lease. Preserve
        // the exact selected surface; hide the obsolete surface independently.
        let presentation = frame.presentation();
        if !presentation.overlay {
            Self::native_visibility(&self.overlay, &self.overlay_guard, false)?;
        }
        if !presentation.passive_widget {
            Self::native_visibility(&self.widget, &self.widget_guard, false)?;
        }
        Ok(())
    }
    /// Explicit cooperating fixture scope only. No public enable flag, renderer
    /// command or real-game/production media grant can call this route.
    #[cfg(all(windows, feature = "gaming-fixture"))]
    pub(crate) fn show_synthetic(
        &mut self,
        widget: bool,
        game: &mut crate::gaming_fixture::native_game::NativeGame,
        session: &crate::gaming_fixture::fixture_session::SyntheticSession,
        gate: &crate::native_input::SafetyGate,
        epoch: u64,
    ) -> Result<(), Error> {
        use windows_sys::Win32::UI::WindowsAndMessaging::*;
        let result = (|| {
            let now = std::time::Instant::now();
            if !session.active(now) || !gate.permissions(epoch, now).0 {
                return Err(Error::SafetyExpired);
            }
            let overlay_hwnd = self.fixture_overlay_hwnd()?;
            if game
                .foreground(&self.overlay_guard, overlay_hwnd)?
                .foreground
                .is_none()
            {
                return Err(Error::Owner);
            }
            let (window, guard) = if widget {
                (&self.widget, &self.widget_guard)
            } else {
                (&self.overlay, &self.overlay_guard)
            };
            match guard.validate(guard.identity()) {
                Ok(()) | Err(crate::native_window_owner::Error::Hidden) => {}
                _ => return Err(Error::Owner),
            }
            let hwnd = window.hwnd().map_err(|_| Error::Render)?.0 as _;
            let (x, y) = game.placement()?;
            if !gate.permissions(epoch, std::time::Instant::now()).0 {
                return Err(Error::SafetyExpired);
            }
            // Exact registered-own HWND, never an arbitrary renderer position.
            if unsafe { SetWindowPos(hwnd, HWND_TOPMOST, x, y, 0, 0, SWP_NOSIZE | SWP_NOACTIVATE) }
                == 0
            {
                return Err(Error::Render);
            }
            game.validate()?;
            Self::native_visibility(window, guard, true)?;
            if !session.active(std::time::Instant::now())
                || !gate.permissions(epoch, std::time::Instant::now()).0
                || game
                    .foreground(&self.overlay_guard, overlay_hwnd)?
                    .foreground
                    .is_none()
            {
                return Err(Error::SafetyExpired);
            }
            Ok(())
        })();
        if result.is_err() {
            gate.revoke();
            let _ = self.hide_all();
        }
        result
    }
    pub fn create(app: &tauri::AppHandle<R>) -> Result<Self, Error> {
        let widget = WebviewWindowBuilder::new(
            app,
            "voice-widget",
            WebviewUrl::App("fixtures/widget.html".into()),
        )
        .title("Mnema Voice fixture")
        .inner_size(244., 184.)
        .decorations(false)
        .resizable(false)
        .always_on_top(true)
        .skip_taskbar(true)
        .focused(false)
        .visible(false)
        .build()
        .map_err(|_| Error::Render)?;
        widget
            .set_ignore_cursor_events(true)
            .map_err(|_| Error::Render)?;
        let overlay = WebviewWindowBuilder::new(
            app,
            "game-overlay",
            WebviewUrl::App("fixtures/overlay.html".into()),
        )
        .title("Mnema Overlay fixture")
        .inner_size(820., 520.)
        .decorations(false)
        .resizable(false)
        .always_on_top(true)
        .skip_taskbar(true)
        .focused(false)
        .visible(false)
        .build()
        .map_err(|_| Error::Render)?;
        #[cfg(windows)]
        let (widget_guard, overlay_guard) = unsafe {
            use windows_sys::Win32::UI::WindowsAndMessaging::*;
            let widget_hwnd = widget.hwnd().map_err(|_| Error::Render)?.0 as _;
            use windows_sys::Win32::Foundation::{GetLastError, SetLastError};
            SetLastError(0);
            let styles = GetWindowLongPtrW(widget_hwnd, GWL_EXSTYLE);
            if styles == 0 && GetLastError() != 0 {
                return Err(Error::Render);
            }
            SetLastError(0);
            let previous =
                SetWindowLongPtrW(widget_hwnd, GWL_EXSTYLE, styles | WS_EX_NOACTIVATE as isize);
            if (previous == 0 && GetLastError() != 0)
                || GetWindowLongPtrW(widget_hwnd, GWL_EXSTYLE) & WS_EX_NOACTIVATE as isize == 0
            {
                return Err(Error::Render);
            }
            let widget_guard = OwnOverlay::register(
                NativeWindow::inspect(widget_hwnd).map_err(|_| Error::Render)?,
            )
            .map_err(|_| Error::Render)?;
            let overlay_guard = OwnOverlay::register(
                NativeWindow::inspect(overlay.hwnd().map_err(|_| Error::Render)?.0 as _)
                    .map_err(|_| Error::Render)?,
            )
            .map_err(|_| Error::Render)?;
            (widget_guard, overlay_guard)
        };
        Ok(Self {
            widget,
            overlay,
            #[cfg(windows)]
            widget_guard,
            #[cfg(windows)]
            overlay_guard,
            released: true,
        })
    }
    /// Do not use generic Tauri set_focus: tao's Windows fallback synthesizes
    /// Alt. Native adapter must first prove the bound game's lifetime; real
    /// external games remain unsupported, so this low-level method is private.
    #[cfg(windows)]
    fn focus_overlay_once(&self) -> Result<(), Error> {
        self.overlay_guard
            .focus_once(self.overlay_guard.identity())
            .map_err(|_| Error::Render)
    }
    #[cfg(windows)]
    fn native_visibility(
        window: &WebviewWindow<R>,
        guard: &OwnOverlay,
        visible: bool,
    ) -> Result<(), Error> {
        use windows_sys::Win32::UI::WindowsAndMessaging::{SW_HIDE, SW_SHOWNOACTIVATE, ShowWindow};
        // Hidden is returned only after exact same-thread lifetime/ownership
        // validation. Every other failure prohibits touching the numeric HWND.
        match guard.validate(guard.identity()) {
            Ok(()) | Err(crate::native_window_owner::Error::Hidden) => {}
            Err(crate::native_window_owner::Error::WindowChanged) => {
                return if visible { Err(Error::Render) } else { Ok(()) };
            }
            Err(_) => return Err(Error::Render),
        }
        let hwnd = window.hwnd().map_err(|_| Error::Render)?.0 as _;
        unsafe {
            ShowWindow(hwnd, if visible { SW_SHOWNOACTIVATE } else { SW_HIDE });
        }
        if visible {
            guard
                .validate(guard.identity())
                .map_err(|_| Error::Render)?;
        }
        Ok(())
    }
}
impl<R: Runtime> Surfaces for GuiSurfaces<R> {
    fn hide_all(&mut self) -> Result<(), Error> {
        #[cfg(windows)]
        {
            let widget = Self::native_visibility(&self.widget, &self.widget_guard, false);
            let overlay = Self::native_visibility(&self.overlay, &self.overlay_guard, false);
            widget.and(overlay)
        }
        #[cfg(not(windows))]
        {
            let widget = self.widget.hide().map_err(|_| Error::Render);
            let overlay = self.overlay.hide().map_err(|_| Error::Render);
            widget.and(overlay)
        }
    }
    fn release_ptt(&mut self) {
        self.released = true;
    } // Fixture only; no microphone exists.
    fn publish(&mut self, frame: &Frame) -> Result<(), Error> {
        let widget = self
            .widget
            .emit("mnema:voice-fixture", frame)
            .map_err(|_| Error::Render);
        let overlay = self
            .overlay
            .emit("mnema:voice-fixture", frame)
            .map_err(|_| Error::Render);
        widget.and(overlay)
    }
    fn show_passive_widget(&mut self) -> Result<(), Error> {
        // No native real-game lifetime proof exists yet. Keep real desktop clean.
        Err(Error::NativeRouteUnsupported)
    }
    fn show_committed_overlay(&mut self) -> Result<(), Error> {
        Err(Error::NativeRouteUnsupported)
    }
}

// The guarded ShowWindow/focus implementation is deliberately not reachable
// from a renderer, ready flag or unsupported real game. A future cooperative
// fixture driver needs a distinct-process lifetime protocol before enabling it.
#[cfg(windows)]
impl<R: Runtime> crate::native_window_owner::FocusDriver for GuiSurfaces<R> {
    fn now(&self) -> std::time::Instant {
        std::time::Instant::now()
    }
    fn check_game(
        &mut self,
    ) -> Result<crate::native_window_owner::CheckedGame, crate::native_window_owner::Error> {
        Err(crate::native_window_owner::Error::UnsupportedGameLifetime)
    }
    fn check_own_overlay(
        &mut self,
        expected: crate::gaming_fixture::policy::WindowIdentity,
    ) -> Result<(), crate::native_window_owner::Error> {
        self.overlay_guard.validate(expected)
    }
    fn focus_once(
        &mut self,
        expected: crate::gaming_fixture::policy::WindowIdentity,
    ) -> Result<(), crate::native_window_owner::Error> {
        if expected != self.overlay_guard.identity() {
            return Err(crate::native_window_owner::Error::WindowChanged);
        }
        self.focus_overlay_once()
            .map_err(|_| crate::native_window_owner::Error::FocusDenied)
    }
    fn own_foreground(
        &mut self,
    ) -> Result<crate::gaming_fixture::policy::WindowIdentity, crate::native_window_owner::Error>
    {
        Err(crate::native_window_owner::Error::UnsupportedGameLifetime)
    }
}
