#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use mnema_desktop_probe::{discovery, windows};
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};

#[derive(Default)]
struct ProbeBudget(Arc<AtomicBool>);
struct InFlight(Arc<AtomicBool>);
impl Drop for InFlight {
    fn drop(&mut self) {
        self.0.store(false, Ordering::Release);
    }
}

fn require_main(window: &tauri::WebviewWindow) -> Result<(), &'static str> {
    if window.label() == "main" {
        Ok(())
    } else {
        Err("forbidden_window")
    }
}

#[tauri::command]
async fn inspect_server(
    window: tauri::WebviewWindow,
    budget: tauri::State<'_, ProbeBudget>,
    address: String,
) -> Result<discovery::ServerCandidate, String> {
    require_main(&window).map_err(str::to_owned)?;
    if budget
        .0
        .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
        .is_err()
    {
        return Err("probe_busy".into());
    }
    let lease = InFlight(Arc::clone(&budget.0));
    // reqwest's blocking client must not run on the UI/runtime event thread.
    tauri::async_runtime::spawn_blocking(move || {
        let _lease = lease;
        discovery::discover(&address)
    })
    .await
    .map_err(|_| "probe_failed".to_owned())?
    .map_err(|error| {
        serde_json::to_value(error)
            .ok()
            .and_then(|v| v.as_str().map(str::to_owned))
            .unwrap_or_else(|| "probe_failed".to_owned())
    })
}

#[tauri::command]
fn inspect_game(
    window: tauri::WebviewWindow,
    names: Vec<String>,
) -> Result<windows::GameProbe, &'static str> {
    require_main(&window)?;
    if !windows::valid_game_names(&names) {
        return Err("invalid_game_names");
    }
    Ok(windows::inspect_foreground(&names))
}

#[cfg(all(windows, feature = "gaming-fixture"))]
thread_local! { static GAMING_FIXTURE:std::cell::RefCell<Option<mnema_desktop_probe::gaming_fixture::runtime::Scheduler>>=const {std::cell::RefCell::new(None)}; }
fn main() {
    #[cfg(all(windows, feature = "gaming-fixture"))]
    if std::env::args().any(|arg| arg == "--cooperating-fixture-child") {
        if mnema_desktop_probe::gaming_fixture::fixture_child::run_child().is_err() {
            std::process::exit(1)
        }
        return;
    }
    let gaming_fixture = std::env::args().any(|arg| arg == "--cooperating-game-session");
    #[cfg(not(all(windows, feature = "gaming-fixture")))]
    if gaming_fixture {
        std::process::exit(2)
    }

    let app = tauri::Builder::default()
        .setup(move |app| {
            #[cfg(all(windows, feature = "gaming-fixture"))]
            if gaming_fixture {
                let config = mnema_desktop_probe::gaming_fixture::options::fixture_config(
                    std::env::args()
                        .collect::<Vec<_>>()
                        .iter()
                        .map(String::as_str),
                )
                .map_err(|_| std::io::Error::other("Invalid synthetic fixture shortcuts"))?;
                let scheduler = mnema_desktop_probe::gaming_fixture::runtime::start(
                    app.handle(),
                    !std::env::args().any(|arg| arg == "--fixture-no-widget"),
                    config,
                )
                .map_err(|_| {
                    std::io::Error::other("Unable to start explicit cooperating synthetic fixture")
                })?;
                GAMING_FIXTURE.with(|slot| *slot.borrow_mut() = Some(scheduler));
            }
            #[cfg(not(all(windows, feature = "gaming-fixture")))]
            let _ = app;
            Ok(())
        })
        .manage(ProbeBudget::default())
        .invoke_handler(tauri::generate_handler![inspect_server, inspect_game])
        .build(tauri::generate_context!())
        .expect("Unable to start the desktop feasibility probe");
    app.run(|handle, event| {
        #[cfg(all(windows, feature = "gaming-fixture"))]
        GAMING_FIXTURE.with(|slot| {
            mnema_desktop_probe::gaming_fixture::lifecycle::handle_event(handle, &event, slot);
        });
        #[cfg(not(all(windows, feature = "gaming-fixture")))]
        let _ = (handle, event);
    });
}
