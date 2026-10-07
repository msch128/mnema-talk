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

fn main() {
    tauri::Builder::default()
        .manage(ProbeBudget::default())
        .invoke_handler(tauri::generate_handler![inspect_server, inspect_game])
        .run(tauri::generate_context!())
        .expect("Unable to start the desktop feasibility probe");
}
