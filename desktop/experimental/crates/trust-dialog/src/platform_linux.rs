use super::*;
use gtk::prelude::*;
use std::{cell::RefCell, rc::Rc, time::Duration};
pub(super) fn show(
    window: &WebviewWindow,
    request: &NativeDialogRequest,
    cancelled: Arc<AtomicBool>,
    finished: Arc<AtomicBool>,
    finish: Finish,
) {
    let parent = match window.gtk_window() {
        Ok(p) => p,
        Err(_) => {
            finish(Err(DialogError::Stale));
            return;
        }
    };
    let text = match request.text() {
        Ok(t) => t,
        Err(e) => {
            finish(Err(e));
            return;
        }
    };
    let dialog = gtk::MessageDialog::builder()
        .transient_for(&parent)
        .modal(true)
        .message_type(gtk::MessageType::Warning)
        .text(request.confirmation_title())
        .secondary_text(&text)
        .build();
    dialog.add_button("Cancel", gtk::ResponseType::Cancel);
    dialog.add_button(request.confirmation_action(), gtk::ResponseType::Accept);
    dialog.set_default_response(gtk::ResponseType::Cancel);
    let callback = Rc::new(RefCell::new(Some(finish)));
    dialog.connect_response(move |dialog, response| {
        dialog.close();
        if let Some(finish) = callback.borrow_mut().take() {
            finish(Ok(response == gtk::ResponseType::Accept));
        }
    });
    let deadline = request.deadline();
    let owned = dialog.clone();
    gtk::glib::timeout_add_local(Duration::from_millis(100), move || {
        if finished.load(Ordering::Acquire) {
            return gtk::glib::ControlFlow::Break;
        }
        if cancelled.load(Ordering::Acquire) || Instant::now() >= deadline {
            owned.response(gtk::ResponseType::Cancel);
            gtk::glib::ControlFlow::Break
        } else {
            gtk::glib::ControlFlow::Continue
        }
    });
    dialog.show_all();
}
