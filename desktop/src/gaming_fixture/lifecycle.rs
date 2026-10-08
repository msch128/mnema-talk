//! Hidden owned surfaces prevent last-window auto-exit. On authoritative Main
//! close, take the fixture out of TLS first, then drop before requesting exit.
pub fn stop_before_exit<T>(fixture: Option<T>, exit: impl FnOnce()) {
    // Scheduler Drop fences queued ticks, revokes input/PTT, clears surfaces
    // and drops the retained owned child before exiting the app runtime.
    drop(fixture);
    exit();
}
#[cfg(windows)]
pub fn handle_event(
    app: &tauri::AppHandle<tauri::Wry>,
    event: &tauri::RunEvent,
    fixture: &std::cell::RefCell<Option<super::runtime::Scheduler>>,
) {
    match event {
        tauri::RunEvent::WindowEvent { label, event, .. }
            if label == "main"
                && matches!(
                    event,
                    tauri::WindowEvent::CloseRequested { .. } | tauri::WindowEvent::Destroyed
                ) =>
        {
            // End the TLS borrow before teardown/exit can cause callbacks.
            let owned = fixture.borrow_mut().take();
            stop_before_exit(owned, || app.exit(0));
        }
        tauri::RunEvent::Exit => {
            let owned = fixture.borrow_mut().take();
            drop(owned);
        }
        _ => {}
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    use std::{cell::RefCell, rc::Rc};
    struct OwnedFixture(Rc<RefCell<Vec<&'static str>>>);
    impl Drop for OwnedFixture {
        fn drop(&mut self) {
            self.0
                .borrow_mut()
                .extend(["cancel-ticks", "release-context", "owned-child-stop"]);
        }
    }
    #[test]
    fn main_close_stops_owner_before_exit_and_queued_tick_cannot_find_it() {
        let effects = Rc::new(RefCell::new(Vec::new()));
        let slot = RefCell::new(Some(OwnedFixture(effects.clone())));
        let fixture = slot.borrow_mut().take();
        stop_before_exit(fixture, || {
            // Reentrant exit callbacks must be able to borrow the emptied slot.
            assert!(slot.borrow_mut().is_none());
            effects.borrow_mut().push("exit");
        });
        if slot.borrow().is_some() {
            effects.borrow_mut().push("queued-tick");
        }
        assert_eq!(
            *effects.borrow(),
            [
                "cancel-ticks",
                "release-context",
                "owned-child-stop",
                "exit"
            ]
        );
        stop_before_exit(slot.borrow_mut().take(), || {});
        assert_eq!(
            effects
                .borrow()
                .iter()
                .filter(|effect| **effect == "owned-child-stop")
                .count(),
            1
        );
    }
    #[test]
    fn failed_start_with_no_owned_fixture_can_still_exit() {
        let mut exited = false;
        stop_before_exit::<OwnedFixture>(None, || exited = true);
        assert!(exited);
    }
}
