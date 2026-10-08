//! Own hidden synthetic-window lifecycle harness. Never an eligible game.
use crate::gaming_fixture::Error;
use crate::native_window_owner::{
    Error as NativeError,
    native::{NativeWindow, OwnOverlay},
};
use std::ptr::{null, null_mut};
use windows_sys::{
    Win32::{System::LibraryLoader::GetModuleHandleW, UI::WindowsAndMessaging::*},
    core::w,
};

/// Explicit native harness entry point. Does not show a window, enumerate
/// processes, synthesize input or pretend same-process fixture is a game lease.
pub fn verify_own_fixture_lifetime() -> Result<(), Error> {
    unsafe {
        let module = GetModuleHandleW(null());
        let class = w!("MnemaOwnSyntheticGameLifecycleFixture");
        let definition = WNDCLASSW {
            lpfnWndProc: Some(DefWindowProcW),
            hInstance: module,
            lpszClassName: class,
            ..Default::default()
        };
        if RegisterClassW(&definition) == 0 {
            return Err(Error::Render);
        }
        let hwnd = CreateWindowExW(
            WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW,
            class,
            w!("Synthetic fixture, not a game"),
            0,
            0,
            0,
            320,
            200,
            null_mut(),
            null_mut(),
            module,
            null(),
        );
        if hwnd.is_null() {
            UnregisterClassW(class, module);
            return Err(Error::Render);
        }
        let result = (|| {
            let snapshot = NativeWindow::inspect(hwnd).map_err(|_| Error::Render)?;
            let guarded = OwnOverlay::register(snapshot).map_err(|_| Error::Render)?;
            let identity = guarded.identity();
            if guarded.validate(identity) != Err(NativeError::Hidden) {
                return Err(Error::Render);
            }
            if DestroyWindow(hwnd) == 0 {
                return Err(Error::Render);
            }
            if guarded.validate(identity) != Err(NativeError::WindowChanged) {
                return Err(Error::Render);
            }
            drop(guarded);
            Ok(())
        })();
        // Idempotent failure cleanup; the guard itself retains callback data on
        // failed removal, so this harness never frees an attached pointer.
        if IsWindow(hwnd) != 0 {
            DestroyWindow(hwnd);
        }
        UnregisterClassW(class, module);
        result
    }
}
