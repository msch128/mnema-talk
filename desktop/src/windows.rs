//! Minimal foreground-only inspection. Never enumerate processes, expose full
//! executable paths, inject into games, or equate monitor coverage with exclusivity.
//! A matching basename is a user-selected candidate, not authenticated executable identity.
use serde::Serialize;
#[cfg(any(windows, test))]
#[path = "game_catalog.rs"]
mod game_catalog;

#[cfg(any(windows, test))]
fn approved_game(names: &[String], name: &str, title: &str) -> Option<String> {
    // Java is a shared runtime: it is only a Minecraft candidate with its title.
    if matches!(name.to_ascii_lowercase().as_str(), "javaw.exe" | "java.exe")
        && !title.to_ascii_lowercase().contains("minecraft")
    {
        return None;
    }
    names
        .iter()
        .chain(game_catalog::executables())
        .find(|n| n.eq_ignore_ascii_case(name))
        .cloned()
}

#[derive(Debug, Serialize)]
pub struct GameProbe {
    /// Local identity of the exact validated foreground observation; never IPC data.
    #[serde(skip)]
    pub native_identity: Option<(usize, u32)>,
    pub platform: &'static str,
    pub game: Option<String>,
    pub covers_monitor: bool,
    pub exclusive_fullscreen_verified: bool,
}

pub fn valid_game_names(names: &[String]) -> bool {
    !names.is_empty()
        && names.len() <= 32
        && names.iter().all(|name| {
            let lower = name.to_ascii_lowercase();
            name.len() <= 128
                && lower.ends_with(".exe")
                && name.len() > 4
                && name.as_bytes()[0].is_ascii_alphanumeric()
                && name
                    .bytes()
                    .all(|c| c.is_ascii_alphanumeric() || b"-_. ".contains(&c))
                && !name.contains("..")
        })
}

#[cfg(any(windows, test))]
fn stable_foreground(initial: usize, final_window: usize, pid: u32, final_pid: u32) -> bool {
    initial != 0 && initial == final_window && pid != 0 && pid == final_pid
}

#[cfg(any(windows, test))]
fn covers_monitor(window: [i32; 4], monitor: [i32; 4]) -> bool {
    // A failed/degenerate rectangle must never be reported as fullscreen evidence.
    window[0] < window[2]
        && window[1] < window[3]
        && monitor[0] < monitor[2]
        && monitor[1] < monitor[3]
        && window[0] <= monitor[0]
        && window[1] <= monitor[1]
        && window[2] >= monitor[2]
        && window[3] >= monitor[3]
}

#[cfg(not(windows))]
pub fn inspect_foreground(_names: &[String]) -> GameProbe {
    GameProbe {
        native_identity: None,
        platform: "unsupported_probe_host",
        game: None,
        covers_monitor: false,
        exclusive_fullscreen_verified: false,
    }
}

#[cfg(windows)]
pub fn inspect_foreground(names: &[String]) -> GameProbe {
    use std::mem::size_of;
    use windows_sys::Win32::Foundation::{CloseHandle, HANDLE, RECT};
    use windows_sys::Win32::Graphics::Gdi::{
        GetMonitorInfoW, MONITOR_DEFAULTTONEAREST, MONITORINFO, MonitorFromWindow,
    };
    use windows_sys::Win32::System::Threading::{
        OpenProcess, PROCESS_QUERY_LIMITED_INFORMATION, QueryFullProcessImageNameW,
    };
    use windows_sys::Win32::UI::WindowsAndMessaging::{
        GetForegroundWindow, GetWindowRect, GetWindowTextW, GetWindowThreadProcessId, IsIconic,
        IsWindowVisible,
    };

    struct Process(HANDLE);
    impl Drop for Process {
        fn drop(&mut self) {
            // SAFETY: this wrapper owns one non-null OpenProcess handle.
            unsafe {
                CloseHandle(self.0);
            }
        }
    }
    let mut result = GameProbe {
        native_identity: None,
        platform: "windows",
        game: None,
        covers_monitor: false,
        exclusive_fullscreen_verified: false,
    };
    if !names.is_empty() && !valid_game_names(names) {
        return result;
    }
    // SAFETY: calls inspect OS-owned windows/handles; all output buffers are sized
    // and live across the calls, and all failure paths return an empty snapshot.
    unsafe {
        let window = GetForegroundWindow();
        if window.is_null() || IsIconic(window) != 0 || IsWindowVisible(window) == 0 {
            return result;
        }
        let mut pid = 0;
        if GetWindowThreadProcessId(window, &mut pid) == 0 || pid == 0 {
            return result;
        }
        let handle = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
        if handle.is_null() {
            return result;
        }
        let process = Process(handle);
        let mut buffer = vec![0u16; 32768];
        let mut length = buffer.len() as u32;
        if QueryFullProcessImageNameW(process.0, 0, buffer.as_mut_ptr(), &mut length) == 0 {
            return result;
        }
        if length == 0 || length as usize > buffer.len() {
            return result;
        }
        let Ok(path) = String::from_utf16(&buffer[..length as usize]) else {
            return result;
        };
        let Some(name) = path.rsplit(['\\', '/']).next() else {
            return result;
        };
        let title = if matches!(name.to_ascii_lowercase().as_str(), "javaw.exe" | "java.exe") {
            let mut text = [0u16; 512];
            let len = GetWindowTextW(window, text.as_mut_ptr(), text.len() as i32);
            String::from_utf16_lossy(&text[..len.max(0) as usize])
        } else {
            String::new()
        };
        let Some(approved) = approved_game(names, name, &title) else {
            return result;
        };
        let mut rect = RECT {
            left: 0,
            top: 0,
            right: 0,
            bottom: 0,
        };
        let monitor = MonitorFromWindow(window, MONITOR_DEFAULTTONEAREST);
        let mut info = MONITORINFO {
            cbSize: size_of::<MONITORINFO>() as u32,
            rcMonitor: rect,
            rcWork: rect,
            dwFlags: 0,
        };
        if GetWindowRect(window, &mut rect) != 0
            && !monitor.is_null()
            && GetMonitorInfoW(monitor, &mut info) != 0
        {
            result.covers_monitor = covers_monitor(
                [rect.left, rect.top, rect.right, rect.bottom],
                [
                    info.rcMonitor.left,
                    info.rcMonitor.top,
                    info.rcMonitor.right,
                    info.rcMonitor.bottom,
                ],
            );
        }
        // Focus/window ownership can change while querying the process and monitor.
        // Discard the entire observation rather than show a stale game on the desktop.
        let final_window = GetForegroundWindow();
        let mut final_pid = 0;
        if final_window.is_null()
            || GetWindowThreadProcessId(final_window, &mut final_pid) == 0
            || !stable_foreground(window as usize, final_window as usize, pid, final_pid)
            || IsIconic(final_window) != 0
            || IsWindowVisible(final_window) == 0
        {
            result.covers_monitor = false;
            return result;
        }
        // Only the user-approved basename leaves this function; no full paths/titles.
        result.game = Some(approved);
        result.native_identity = Some((window as usize, pid));
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn catalog_covers_requested_games_without_generic_java_false_positives() {
        let catalog: serde_json::Value =
            serde_json::from_str(include_str!("game-catalog.json")).unwrap();
        assert_eq!(catalog["games"].as_array().unwrap().len(), 300);
        for name in game_catalog::executables() {
            assert!(valid_game_names(std::slice::from_ref(name)));
        }
        for name in [
            "sweaw.exe",
            "payday2_win32_release.exe",
            "actofaggression.exe",
            "WardogsClient-Win64-Shipping.exe",
            "spring.exe",
            "league of legends.exe",
            "eso64.exe",
            "blackdesert64.exe",
            "cod.exe",
        ] {
            assert!(approved_game(&[], name, "").is_some(), "{name}");
        }
        for helper in [
            "isaacanimationeditor.exe",
            "roomeditor.exe",
            "itempooleditor.exe",
            "gmad.exe",
            "acservermanager.exe",
            "kseditor.exe",
            "shootergameserver.exe",
            "supporttool.exe",
            "ddnet-server.exe",
            "dayzuninstaller.exe",
            "ss_setup.exe",
        ] {
            assert!(approved_game(&[], helper, "").is_none(), "{helper}");
        }
        assert!(approved_game(&[], "javaw.exe", "Minecraft 1.21").is_some());
        assert!(approved_game(&[], "javaw.exe", "Java editor").is_none());
        assert!(approved_game(&[], "chrome.exe", "Minecraft website").is_none());
        assert!(approved_game(&["Custom.exe".into()], "CUSTOM.EXE", "").is_some());
    }
    #[test]
    fn allowlist_only_accepts_bounded_executable_names() {
        assert!(valid_game_names(&[
            "Wow.exe".into(),
            "GenshinImpact.exe".into()
        ]));
        for names in [
            vec![],
            vec!["../Wow.exe".into()],
            vec!["C:\\Wow.exe".into()],
            vec!["Wow.exe\n".into()],
            vec!["Wow".into()],
            vec![".exe".into()],
            vec![" Wow.exe".into()],
            vec!["Wow.exe".into(); 33],
            vec![format!("{}.exe", "a".repeat(129))],
        ] {
            assert!(!valid_game_names(&names));
        }
    }

    #[test]
    fn changed_focus_or_reused_window_owner_invalidates_snapshot() {
        assert!(stable_foreground(1, 1, 42, 42));
        for (initial, final_window, pid, final_pid) in
            [(0, 0, 42, 42), (1, 2, 42, 42), (1, 1, 0, 0), (1, 1, 42, 99)]
        {
            assert!(!stable_foreground(initial, final_window, pid, final_pid));
        }
    }

    #[test]
    fn monitor_geometry_is_validated_and_accepts_negative_monitor_coordinates() {
        assert!(covers_monitor([-1920, 0, 0, 1080], [-1920, 0, 0, 1080]));
        assert!(covers_monitor([-1930, -10, 10, 1090], [-1920, 0, 0, 1080]));
        for (window, monitor) in [
            ([0, 0, 1920, 1079], [0, 0, 1920, 1080]),
            ([0, 0, 0, 0], [0, 0, 0, 0]),
            ([1920, 0, 0, 1080], [0, 0, 1920, 1080]),
            ([0, 1080, 1920, 0], [0, 0, 1920, 1080]),
            ([-1920, 0, 0, 1080], [0, 0, 1920, 1080]),
        ] {
            assert!(!covers_monitor(window, monitor));
        }
    }
    #[cfg(not(windows))]
    #[test]
    fn other_hosts_do_not_manufacture_windows_evidence() {
        let snapshot = inspect_foreground(&["Wow.exe".into()]);
        assert_eq!(snapshot.platform, "unsupported_probe_host");
        assert!(snapshot.game.is_none());
        assert!(!snapshot.exclusive_fullscreen_verified);
    }
}
