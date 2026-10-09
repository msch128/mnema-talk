use super::*;
use objc2::{MainThreadMarker, rc::Retained};
use objc2_app_kit::{NSAlert, NSAlertFirstButtonReturn, NSAlertSecondButtonReturn, NSWindow};
use objc2_foundation::NSString;
use std::{cell::RefCell, collections::HashMap, time::Duration};
struct Sheet {
    instance: DialogInstance,
    parent: Retained<NSWindow>,
    alert: Retained<NSAlert>,
}
thread_local! {static SHEETS:RefCell<HashMap<usize,Sheet>>=RefCell::new(HashMap::new());}
pub(super) fn show(
    window: &WebviewWindow,
    request: &NativeDialogRequest,
    cancelled: Arc<AtomicBool>,
    finished: Arc<AtomicBool>,
    finish: Finish,
) {
    let Some(mtm) = MainThreadMarker::new() else {
        finish(Err(DialogError::Unavailable));
        return;
    };
    let parent = match window.ns_window() {
        Ok(raw) if !raw.is_null() => {
            // Official Tauri gives the live NSWindow pointer; this runs on the
            // native main thread immediately after the actual host registry check.
            unsafe { Retained::retain(raw.cast::<NSWindow>()) }.expect("nonnull native window")
        }
        _ => {
            finish(Err(DialogError::Stale));
            return;
        }
    };
    if parent.attachedSheet().is_some() {
        finish(Err(DialogError::Unavailable));
        return;
    }
    let text = match request.text() {
        Ok(t) => t,
        Err(e) => {
            finish(Err(e));
            return;
        }
    };
    let alert = NSAlert::new(mtm);
    alert.setMessageText(&NSString::from_str(request.confirmation_title()));
    alert.setInformativeText(&NSString::from_str(&text));
    let _ = alert.addButtonWithTitle(&NSString::from_str("Cancel"));
    let _ = alert.addButtonWithTitle(&NSString::from_str(request.confirmation_action()));
    let instance = DialogInstance::fresh();
    let callback_instance = instance.clone();
    let finish = RefCell::new(Some(finish));
    let callback = block2::RcBlock::new(move |result: isize| {
        let _owned = SHEETS.with(|s| s.borrow_mut().remove(&callback_instance.key()));
        if let Some(finish) = finish.borrow_mut().take() {
            finish(Ok(result == NSAlertSecondButtonReturn));
        }
    });
    SHEETS.with(|s| {
        s.borrow_mut().insert(
            instance.key(),
            Sheet {
                instance: instance.clone(),
                parent: parent.clone(),
                alert: alert.clone(),
            },
        )
    });
    alert.beginSheetModalForWindow_completionHandler(&parent, Some(&callback));
    let window = window.clone();
    let deadline = request.deadline();
    tauri::async_runtime::spawn(async move {
        loop {
            tokio::time::sleep(Duration::from_millis(100)).await;
            if finished.load(Ordering::Acquire) {
                break;
            }
            if cancelled.load(Ordering::Acquire) || Instant::now() >= deadline {
                let queued_instance = instance.clone();
                let _ = window.run_on_main_thread(move || {
                    let owned = SHEETS.with(|s| {
                        s.borrow()
                            .get(&queued_instance.key())
                            .filter(|sheet| sheet.instance.same(&queued_instance))
                            .map(|sheet| (sheet.parent.clone(), sheet.alert.window()))
                    });
                    // Release the map borrow before AppKit's potentially
                    // synchronous completion callback removes its own entry.
                    if let Some((parent, sheet)) = owned
                        && parent
                            .attachedSheet()
                            .is_some_and(|attached| std::ptr::eq::<NSWindow>(&*attached, &*sheet))
                    {
                        parent.endSheet_returnCode(&sheet, NSAlertFirstButtonReturn);
                    }
                });
                break;
            }
        }
    });
}
