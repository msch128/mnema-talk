fn main() {
    #[cfg(feature = "shell")]
    tauri_build::try_build(tauri_build::Attributes::new().app_manifest(
        tauri_build::AppManifest::new().commands(&["inspect_server", "inspect_game"]),
    ))
    .expect("Unable to validate desktop capabilities");
}
