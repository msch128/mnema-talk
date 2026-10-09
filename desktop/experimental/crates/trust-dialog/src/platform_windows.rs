use super::*;
use windows_sys::Win32::{
    Foundation::{HWND, LPARAM, WPARAM},
    UI::{
        Controls::*,
        WindowsAndMessaging::{IDCANCEL, SendMessageW},
    },
};
struct Callback {
    cancelled: Arc<AtomicBool>,
    deadline: Instant,
}
unsafe extern "system" fn callback(
    hwnd: HWND,
    message: u32,
    _: WPARAM,
    _: LPARAM,
    data: isize,
) -> i32 {
    // TaskDialogIndirect synchronously owns this stack context until return;
    // callback HWND belongs to that exact native dialog, never a searched handle.
    let state = unsafe { &*(data as *const Callback) };
    if message == TDN_TIMER as u32
        && (state.cancelled.load(Ordering::Acquire) || Instant::now() >= state.deadline)
    {
        unsafe { SendMessageW(hwnd, TDM_CLICK_BUTTON as u32, IDCANCEL as usize, 0) };
    }
    0
}
fn wide(s: &str) -> Vec<u16> {
    s.encode_utf16().chain(Some(0)).collect()
}
pub(super) fn show(
    window: &WebviewWindow,
    request: &NativeDialogRequest,
    cancelled: Arc<AtomicBool>,
    _: Arc<AtomicBool>,
    finish: Finish,
) {
    let hwnd = match window.hwnd() {
        Ok(h) => h.0 as HWND,
        Err(_) => {
            finish(Err(DialogError::Stale));
            return;
        }
    };
    let text = match request.text() {
        Ok(t) => wide(&t),
        Err(e) => {
            finish(Err(e));
            return;
        }
    };
    let title = wide(request.confirmation_title());
    let label = wide(request.confirmation_action());
    let button = TASKDIALOG_BUTTON {
        nButtonID: 1000,
        pszButtonText: label.as_ptr(),
    };
    let state = Callback {
        cancelled,
        deadline: request.deadline(),
    };
    let config = TASKDIALOGCONFIG {
        cbSize: std::mem::size_of::<TASKDIALOGCONFIG>() as u32,
        hwndParent: hwnd,
        dwFlags: TDF_ALLOW_DIALOG_CANCELLATION
            | TDF_CALLBACK_TIMER
            | TDF_POSITION_RELATIVE_TO_WINDOW,
        dwCommonButtons: TDCBF_CANCEL_BUTTON,
        pszWindowTitle: title.as_ptr(),
        pszContent: text.as_ptr(),
        cButtons: 1,
        pButtons: &button,
        nDefaultButton: IDCANCEL,
        pfCallback: Some(callback),
        lpCallbackData: (&state as *const Callback) as isize,
        ..Default::default()
    };
    let mut result = 0;
    let status = unsafe {
        TaskDialogIndirect(
            &config,
            &mut result,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
        )
    };
    if status < 0 {
        finish(Err(DialogError::Unavailable))
    } else {
        finish(Ok(result == 1000))
    }
}
