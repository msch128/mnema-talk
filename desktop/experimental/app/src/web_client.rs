//! The production Vue app runs at its own HTTPS origin, with browser transport.
//! Only the bundled instance selector receives native commands.
use reqwest::{Client, redirect::Policy};
use serde::Deserialize;
use std::time::Duration;
use tauri::{Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder};
use url::Url;

const MAX_DISCOVERY_BYTES: usize = 16 * 1024;

fn address(input: &str) -> Result<Url, String> {
    if input.len() > 2048 || input.chars().any(char::is_control) {
        return Err("Enter an HTTPS instance address.".into());
    }
    let input = input.trim();
    let raw = if input.contains("://") {
        input.to_owned()
    } else {
        format!("https://{input}")
    };
    if raw.contains('\\') || raw.chars().any(char::is_whitespace) {
        return Err("Invalid instance address.".into());
    }
    let url = Url::parse(&raw).map_err(|_| "Invalid instance address.")?;
    if url.scheme() != "https"
        || url.host_str().is_none()
        || !url.username().is_empty()
        || url.password().is_some()
        || url.query().is_some()
        || url.fragment().is_some()
        || url.path() != "/"
        || raw
            .split_once("://")
            .is_some_and(|(_, authority)| authority.contains('@'))
    {
        return Err("Use an HTTPS origin without credentials, a path or query.".into());
    }
    Ok(url)
}

fn bundled(url: &Url) -> bool {
    url.port().is_none()
        && url.username().is_empty()
        && url.password().is_none()
        && ((url.scheme() == "tauri" && url.host_str() == Some("localhost"))
            || (url.scheme() == "http" && url.host_str() == Some("tauri.localhost")))
        && matches!(url.path(), "" | "/" | "/index.html")
}

#[derive(Deserialize)]
struct Health {
    status: String,
    web_client_api: u32,
}

fn validate_health(bytes: &[u8]) -> Result<(), String> {
    let health: Health = serde_json::from_slice(bytes)
        .map_err(|_| "This instance needs an update for the desktop client.")?;
    if health.status != "ok" || health.web_client_api != 1 {
        return Err("This instance is not compatible with this desktop client.".into());
    }
    Ok(())
}

async fn compatible(origin: &Url) -> Result<(), String> {
    let client = Client::builder()
        .redirect(Policy::none())
        .no_proxy()
        .timeout(Duration::from_secs(10))
        .build()
        .map_err(|_| "Unable to initialize HTTPS.")?;
    let mut response = client
        .get(
            origin
                .join("api/health")
                .map_err(|_| "Invalid instance address.")?,
        )
        .send()
        .await
        .map_err(|_| "Unable to reach the instance using trusted HTTPS.")?;
    if response.status() != reqwest::StatusCode::OK {
        return Err("The instance is unavailable or redirects to a different address.".into());
    }
    let mut bytes = Vec::new();
    while let Some(chunk) = response
        .chunk()
        .await
        .map_err(|_| "Instance check failed.")?
    {
        if chunk.len() > MAX_DISCOVERY_BYTES.saturating_sub(bytes.len()) {
            return Err("Invalid instance response.".into());
        }
        bytes.extend_from_slice(&chunk);
    }
    validate_health(&bytes)
}

#[tauri::command]
async fn desktop_open_instance(window: WebviewWindow, address: String) -> Result<(), String> {
    if window.label() != "main" || !bundled(&window.url().map_err(|_| "Invalid selector.")?) {
        return Err("Only the instance selector may open an instance.".into());
    }
    let origin = self::address(&address)?;
    compatible(&origin).await?;
    if window.app_handle().get_webview_window("instance").is_some() {
        return Err("Close the current instance before selecting another.".into());
    }
    let allowed_origin = origin.origin();
    let popup_origin = allowed_origin.clone();
    let app = window.app_handle().clone();
    let popup_app = app.clone();
    let instance = WebviewWindowBuilder::new(&app, "instance", WebviewUrl::External(origin))
        .title("Mnema Desktop DEV")
        .inner_size(1200.0, 820.0)
        .on_navigation(move |url| {
            url.scheme() == "https"
                && url.username().is_empty()
                && url.password().is_none()
                && url.origin() == allowed_origin
        })
        .on_new_window(move |url, features| {
            if url.scheme() == "https"
                && url.username().is_empty()
                && url.password().is_none()
                && url.origin() == popup_origin
            {
                if let Some(attachment) = popup_app.get_webview_window("attachment") {
                    let _ = attachment.navigate(url);
                    let _ = attachment.set_focus();
                } else {
                    let allowed = popup_origin.clone();
                    // Reuse the opener's browser environment/profile, including
                    // same-origin cookies, without granting native capabilities.
                    let child = WebviewWindowBuilder::new(
                        &popup_app,
                        "attachment",
                        WebviewUrl::External(url),
                    )
                    .window_features(features)
                    .title("Mnema Desktop DEV — Attachment")
                    .on_navigation(move |url| {
                        url.scheme() == "https"
                            && url.username().is_empty()
                            && url.password().is_none()
                            && url.origin() == allowed
                    })
                    .on_new_window(|_, _| tauri::webview::NewWindowResponse::Deny)
                    .on_permission_request(|_, _| tauri::webview::PermissionResponse::Deny)
                    .build();
                    if let Ok(window) = child {
                        return tauri::webview::NewWindowResponse::Create { window };
                    }
                }
            } else {
                open_external(&url);
            }
            tauri::webview::NewWindowResponse::Deny
        })
        .on_permission_request(|_, _| tauri::webview::PermissionResponse::Default)
        .build()
        .map_err(|_| "Unable to open the instance. Check the WebView2 runtime.")?;
    let selector = window.clone();
    instance.on_window_event(move |event| {
        if matches!(event, tauri::WindowEvent::Destroyed) {
            if let Some(attachment) = selector.app_handle().get_webview_window("attachment") {
                let _ = attachment.close();
            }
            let _ = selector.show();
        }
    });
    window
        .hide()
        .map_err(|_| "Unable to hide the instance selector.")?;
    Ok(())
}

pub(super) fn run() {
    let result = tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![desktop_open_instance])
        .setup(|app| {
            WebviewWindowBuilder::new(app, "main", WebviewUrl::App("index.html".into()))
                .title("Mnema Desktop DEV — Instance")
                .inner_size(520.0, 520.0)
                .initialization_script(
                    "Object.defineProperty(window, '__MNEMA_WEB_DESKTOP__', { value: true });",
                )
                .on_navigation(bundled)
                .on_new_window(|_, _| tauri::webview::NewWindowResponse::Deny)
                .on_permission_request(|_, _| tauri::webview::PermissionResponse::Deny)
                .build()?;
            Ok(())
        })
        .run(tauri::generate_context!());
    if result.is_err() {
        startup_error();
    }
}

fn startup_error() {
    eprintln!(
        "Mnema Desktop DEV could not start. Check that Microsoft Edge WebView2 Runtime is installed."
    );
    #[cfg(target_os = "windows")]
    {
        #[link(name = "user32")]
        unsafe extern "system" {
            fn MessageBoxW(
                hwnd: *mut std::ffi::c_void,
                text: *const u16,
                caption: *const u16,
                flags: u32,
            ) -> i32;
        }
        let text: Vec<u16> = "Mnema Desktop DEV could not start.\nCheck that Microsoft Edge WebView2 Runtime is installed.\nPlease report this startup failure.".encode_utf16().chain(Some(0)).collect();
        let title: Vec<u16> = "Mnema Desktop DEV".encode_utf16().chain(Some(0)).collect();
        // SAFETY: both strings are NUL-terminated and live throughout the call.
        unsafe {
            MessageBoxW(std::ptr::null_mut(), text.as_ptr(), title.as_ptr(), 0x10);
        }
    }
}

fn external_allowed(url: &Url) -> bool {
    url.scheme() == "https"
        && url.host_str().is_some()
        && url.username().is_empty()
        && url.password().is_none()
        && url.as_str().len() <= 4096
}

fn open_external(url: &Url) {
    let allowed = external_allowed(url);
    #[cfg(not(target_os = "windows"))]
    let _ = allowed;
    #[cfg(target_os = "windows")]
    {
        if !allowed {
            return;
        }
        #[link(name = "user32")]
        unsafe extern "system" {
            fn MessageBoxW(
                hwnd: *mut std::ffi::c_void,
                text: *const u16,
                caption: *const u16,
                flags: u32,
            ) -> i32;
        }
        #[link(name = "shell32")]
        unsafe extern "system" {
            fn ShellExecuteW(
                hwnd: *mut std::ffi::c_void,
                operation: *const u16,
                file: *const u16,
                parameters: *const u16,
                directory: *const u16,
                show: i32,
            ) -> *mut std::ffi::c_void;
        }
        let text: Vec<u16> = format!("Open this link in your browser?\n\n{}", url.as_str())
            .encode_utf16()
            .chain(Some(0))
            .collect();
        let title: Vec<u16> = "Mnema Desktop DEV".encode_utf16().chain(Some(0)).collect();
        // SAFETY: NUL-terminated buffers live throughout the synchronous dialog.
        let answer =
            unsafe { MessageBoxW(std::ptr::null_mut(), text.as_ptr(), title.as_ptr(), 0x124) };
        if answer != 6 {
            return;
        }
        let operation: Vec<u16> = "open".encode_utf16().chain(Some(0)).collect();
        let target: Vec<u16> = url.as_str().encode_utf16().chain(Some(0)).collect();
        // SAFETY: the target is a bounded HTTPS URL without credentials; no
        // executable arguments or command shell are supplied. Buffers stay live.
        unsafe {
            ShellExecuteW(
                std::ptr::null_mut(),
                operation.as_ptr(),
                target.as_ptr(),
                std::ptr::null(),
                std::ptr::null(),
                1,
            );
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn normal_server_contract_rejects_old_unavailable_and_future_apis() {
        assert!(validate_health(br#"{"status":"ok","version":"dev","web_client_api":1}"#).is_ok());
        for raw in [
            br#"{"status":"ok","version":"0.7.0"}"#.as_slice(),
            br#"{"status":"ok","web_client_api":0}"#,
            br#"{"status":"ok","web_client_api":2}"#,
            br#"{"status":"unavailable","web_client_api":1}"#,
            b"not JSON",
        ] {
            assert!(validate_health(raw).is_err());
        }
    }
    #[test]
    fn external_links_cannot_launch_files_scripts_or_credentialed_targets() {
        assert!(external_allowed(
            &Url::parse("https://example.invalid/page?q=value").unwrap()
        ));
        for raw in [
            "file:///tmp/example",
            "javascript:alert(1)",
            "mailto:user@example.invalid",
            "https://user:password@example.invalid/",
            "http://example.invalid/",
        ] {
            assert!(!external_allowed(&Url::parse(raw).unwrap()));
        }
    }
    #[test]
    fn only_explicit_https_origins_are_selectable() {
        for valid in [
            "community.example",
            "https://community.example/",
            "https://community.example:8443",
        ] {
            assert!(address(valid).is_ok());
        }
        for invalid in [
            "",
            "http://community.example",
            "https://user@community.example",
            "https://community.example/path",
            "https://community.example?token=x",
            "https://community.example/#x",
            "https://community.example\\other",
            "https://community.example\n",
        ] {
            assert!(address(invalid).is_err(), "{invalid}");
        }
    }
    #[test]
    fn remote_and_nonroot_documents_never_gain_selector_authority() {
        for local in [
            "tauri://localhost",
            "tauri://localhost/index.html",
            "http://tauri.localhost/",
        ] {
            assert!(bundled(&Url::parse(local).unwrap()));
        }
        for remote in [
            "https://community.example/",
            "tauri://localhost/other",
            "http://tauri.localhost:9000/",
            "tauri://user@localhost/",
        ] {
            assert!(!bundled(&Url::parse(remote).unwrap()));
        }
    }
}
