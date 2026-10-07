//! Bounded Windows input foundation, not a qualified global-hotkey service.
//!
//! Polls only configured buttons and four modifier classes; no keyboard hook,
//! input interception, key history, process injection, or native IPC is installed.
//! Polling can miss short taps. GetAsyncKeyState cannot distinguish every access
//! failure from an unpressed key. A session owner must supply availability/lock/
//! Voice policy, keep polling alive, release on suspend/exit, and enforce a media
//! watchdog independently. This adapter alone must never authorize transmission.
//! https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getasynckeystate

use std::time::{Duration, Instant};

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct Modifiers {
    pub alt: bool,
    pub control: bool,
    pub shift: bool,
    pub windows: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BindingError {
    UnsupportedKey,
    ReservedCombination,
    Conflict,
}

/// Validated Win32 virtual-key binding. This is layout-dependent, not a portable
/// physical scan-code binding. Mouse 4/5 are physical buttons on Windows.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Binding {
    key: u16,
    modifiers: Modifiers,
}

impl Binding {
    pub fn new(key: u16, modifiers: Modifiers) -> Result<Self, BindingError> {
        // Deliberately bounded: letters, digits, F1-F24, space, and side mouse
        // buttons. System/navigation/modifier-only keys are not accepted.
        if !(matches!(key, 0x05 | 0x06 | 0x20 | 0x30..=0x39 | 0x41..=0x5a | 0x70..=0x87)) {
            return Err(BindingError::UnsupportedKey);
        }
        if modifiers.windows || (modifiers.alt && matches!(key, 0x20 | 0x73)) {
            return Err(BindingError::ReservedCombination);
        }
        Ok(Self { key, modifiers })
    }

    pub fn key(self) -> u16 {
        self.key
    }

    pub fn modifiers(self) -> Modifiers {
        self.modifiers
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Bindings {
    overlay: Binding,
    ptt: Option<Binding>,
}

impl Default for Bindings {
    fn default() -> Self {
        Self {
            overlay: Binding {
                key: 0x4d, // Alt+M.
                modifiers: Modifiers {
                    alt: true,
                    ..Modifiers::default()
                },
            },
            ptt: None,
        }
    }
}

impl Bindings {
    pub fn new(overlay: Binding, ptt: Option<Binding>) -> Result<Self, BindingError> {
        if ptt == Some(overlay) {
            return Err(BindingError::Conflict);
        }
        Ok(Self { overlay, ptt })
    }
}

/// Input from this sampler remains internal to the native input owner. Only
/// action transitions should be forwarded to the authoritative session core.
#[derive(Debug, Clone, Copy, Default)]
pub struct Sample {
    pub modifiers: Modifiers,
    pub overlay_down: bool,
    pub ptt_down: bool,
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct Actions {
    pub toggle_overlay: bool,
    pub press_ptt: bool,
    pub release_ptt: bool,
}

#[derive(Debug, Default)]
struct ButtonState {
    enabled: bool,
    armed: bool,
    down: bool,
    active: bool,
}

impl ButtonState {
    fn update(&mut self, enabled: bool, down: bool, matches: bool) -> (bool, bool) {
        let was_active = self.active;
        if !enabled || self.enabled != enabled {
            self.armed = false;
            self.active = false;
        }
        self.enabled = enabled;
        if enabled && !down {
            self.armed = true;
            self.active = false;
        }
        let pressed = enabled && down && !self.down && self.armed && matches;
        if down {
            // A fresh primary-key release is required after any press, including
            // a press with wrong modifiers; restoring a held chord cannot unmute.
            self.armed = false;
        }
        if pressed {
            self.active = true;
        }
        if !matches {
            self.active = false;
        }
        self.down = down;
        (pressed, was_active && !self.active)
    }
}

#[derive(Debug)]
pub struct ShortcutState {
    bindings: Bindings,
    overlay: ButtonState,
    ptt: ButtonState,
    last_poll: Option<Instant>,
}

impl ShortcutState {
    pub fn new(bindings: Bindings) -> Self {
        Self {
            bindings,
            overlay: ButtonState::default(),
            ptt: ButtonState::default(),
            last_poll: None,
        }
    }

    pub fn bindings(&self) -> Bindings {
        self.bindings
    }

    /// Called on lock, lost input, session/profile change, suspend or shutdown.
    /// Caller must also force the media owner to release PTT independently.
    pub fn reset(&mut self) -> Actions {
        let release_ptt = self.ptt.active;
        self.overlay = ButtonState::default();
        self.ptt = ButtonState::default();
        self.last_poll = None;
        Actions {
            release_ptt,
            ..Actions::default()
        }
    }

    pub fn reconfigure(&mut self, bindings: Bindings) -> Actions {
        let actions = self.reset();
        self.bindings = bindings;
        actions
    }

    /// `overlay_allowed` and `ptt_allowed` must come from session policy, never
    /// from an arbitrary WebView. A delayed poll releases held PTT and requires
    /// release/repress. 250ms is a safety bound, not a polling frequency promise.
    pub fn update(
        &mut self,
        sample: Sample,
        overlay_allowed: bool,
        ptt_allowed: bool,
        now: Instant,
    ) -> Actions {
        let interrupted = self.last_poll.is_some_and(|previous| {
            now.saturating_duration_since(previous) > Duration::from_millis(250)
        });
        let lost_actions = if interrupted {
            self.reset()
        } else {
            Actions::default()
        };
        self.last_poll = Some(now);
        let (toggle_overlay, _) = self.overlay.update(
            overlay_allowed,
            sample.overlay_down,
            sample.modifiers == self.bindings.overlay.modifiers,
        );
        let (press_ptt, release_ptt) = self.ptt.update(
            ptt_allowed && self.bindings.ptt.is_some(),
            sample.ptt_down,
            self.bindings
                .ptt
                .is_some_and(|binding| binding.modifiers == sample.modifiers),
        );
        Actions {
            toggle_overlay,
            press_ptt,
            release_ptt: lost_actions.release_ptt || release_ptt,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SampleError {
    UnsupportedHost,
    ForegroundChanged,
}

#[cfg(not(windows))]
pub fn sample_configured(_bindings: Bindings) -> Result<Sample, SampleError> {
    Err(SampleError::UnsupportedHost)
}

#[cfg(windows)]
pub fn sample_configured(bindings: Bindings) -> Result<Sample, SampleError> {
    use windows_sys::Win32::UI::Input::KeyboardAndMouse::GetAsyncKeyState;
    use windows_sys::Win32::UI::WindowsAndMessaging::{
        GetForegroundWindow, GetWindowThreadProcessId,
    };

    fn down(key: u16) -> bool {
        // SAFETY: GetAsyncKeyState reads a bounded validated key and takes no
        // pointers. Only the high bit is used; the "pressed since" bit is racy.
        unsafe { GetAsyncKeyState(i32::from(key)) < 0 }
    }
    fn modifiers() -> Modifiers {
        Modifiers {
            alt: down(0x12),
            control: down(0x11),
            shift: down(0x10),
            windows: down(0x5b) || down(0x5c),
        }
    }
    // SAFETY: GetForegroundWindow returns an OS-owned borrowed handle; it is
    // compared only and never closed or dereferenced by this code.
    let foreground = unsafe { GetForegroundWindow() };
    if foreground.is_null() {
        return Err(SampleError::ForegroundChanged);
    }
    let mut pid = 0;
    // SAFETY: the live local PID output is a valid writable DWORD pointer.
    if unsafe { GetWindowThreadProcessId(foreground, &mut pid) } == 0 || pid == 0 {
        return Err(SampleError::ForegroundChanged);
    }
    let initial_modifiers = modifiers();
    let result = Sample {
        modifiers: initial_modifiers,
        overlay_down: down(bindings.overlay.key),
        ptt_down: bindings.ptt.is_some_and(|binding| down(binding.key)),
    };
    // A torn sample/focus transition is discarded; callers reset on errors.
    let final_modifiers = modifiers();
    // SAFETY: both calls only inspect an OS-owned borrowed window and a valid
    // local PID output. Checking ownership also rejects recycled HWNDs.
    let final_foreground = unsafe { GetForegroundWindow() };
    let mut final_pid = 0;
    if initial_modifiers != final_modifiers
        || foreground != final_foreground
        || unsafe { GetWindowThreadProcessId(final_foreground, &mut final_pid) } == 0
        || pid != final_pid
    {
        return Err(SampleError::ForegroundChanged);
    }
    Ok(result)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn mouse_bindings() -> Bindings {
        Bindings::new(
            Bindings::default().overlay,
            Some(Binding::new(0x05, Modifiers::default()).unwrap()),
        )
        .unwrap()
    }

    #[test]
    fn bindings_validate_keys_reserved_combinations_and_conflicts() {
        assert_eq!(Bindings::default().overlay.key(), 0x4d);
        for key in [0, 0x10, 0x12, 0x1b, 0x2e, 0x5b, 0x88, 256] {
            assert_eq!(
                Binding::new(key, Modifiers::default()),
                Err(BindingError::UnsupportedKey)
            );
        }
        assert!(Binding::new(0x06, Modifiers::default()).is_ok());
        for key in [0x30, 0x41, 0x70, 0x87] {
            assert!(Binding::new(key, Modifiers::default()).is_ok());
        }
        assert_eq!(
            Binding::new(
                0x4c,
                Modifiers {
                    windows: true,
                    ..Modifiers::default()
                }
            ),
            Err(BindingError::ReservedCombination)
        );
        assert_eq!(
            Binding::new(
                0x73,
                Modifiers {
                    alt: true,
                    ..Modifiers::default()
                }
            ),
            Err(BindingError::ReservedCombination)
        );
        let overlay = Bindings::default().overlay;
        assert_eq!(
            Bindings::new(overlay, Some(overlay)),
            Err(BindingError::Conflict)
        );
    }

    #[test]
    fn overlay_toggles_once_and_requires_exact_modifiers_and_fresh_primary_press() {
        let now = Instant::now();
        let mut state = ShortcutState::new(Bindings::default());
        let alt = Modifiers {
            alt: true,
            ..Modifiers::default()
        };
        let held = Sample {
            modifiers: alt,
            overlay_down: true,
            ptt_down: false,
        };
        // Holding the shortcut before startup or policy activation cannot open it.
        assert_eq!(state.update(held, true, false, now), Actions::default());
        state.update(Sample::default(), true, false, now);
        assert!(state.update(held, true, false, now).toggle_overlay);
        assert!(!state.update(held, true, false, now).toggle_overlay);
        state.update(Sample::default(), true, false, now);
        let altgr = Sample {
            modifiers: Modifiers {
                control: true,
                ..alt
            },
            ..held
        };
        assert!(!state.update(altgr, true, false, now).toggle_overlay);
        assert!(!state.update(held, true, false, now).toggle_overlay);
        state.update(Sample::default(), true, false, now);
        assert!(state.update(held, true, false, now).toggle_overlay);
    }

    #[test]
    fn ptt_release_and_context_restore_cannot_resume_a_held_button() {
        let now = Instant::now();
        let mut state = ShortcutState::new(mouse_bindings());
        let held = Sample {
            ptt_down: true,
            ..Sample::default()
        };
        state.update(Sample::default(), false, true, now);
        assert!(state.update(held, false, true, now).press_ptt);
        assert!(state.update(held, false, false, now).release_ptt);
        assert!(!state.update(held, false, true, now).press_ptt);
        state.update(Sample::default(), false, true, now);
        assert!(state.update(held, false, true, now).press_ptt);
        assert!(
            state
                .update(Sample::default(), false, true, now)
                .release_ptt
        );
    }

    #[test]
    fn modifier_change_releases_ptt_without_reactivating_held_primary() {
        let now = Instant::now();
        let mut state = ShortcutState::new(mouse_bindings());
        let held = Sample {
            ptt_down: true,
            ..Sample::default()
        };
        state.update(Sample::default(), false, true, now);
        assert!(state.update(held, false, true, now).press_ptt);
        let shifted = Sample {
            modifiers: Modifiers {
                shift: true,
                ..Modifiers::default()
            },
            ..held
        };
        assert!(state.update(shifted, false, true, now).release_ptt);
        assert!(!state.update(held, false, true, now).press_ptt);
    }

    #[test]
    fn stalled_poll_or_reconfiguration_releases_and_requires_repress() {
        let now = Instant::now();
        let mut state = ShortcutState::new(mouse_bindings());
        let held = Sample {
            ptt_down: true,
            ..Sample::default()
        };
        state.update(Sample::default(), false, true, now);
        state.update(held, false, true, now);
        let later = now + Duration::from_millis(251);
        let actions = state.update(held, false, true, later);
        assert!(actions.release_ptt);
        assert!(!actions.press_ptt);
        state.update(Sample::default(), false, true, later);
        state.update(held, false, true, later);
        assert!(state.reconfigure(Bindings::default()).release_ptt);
        assert_eq!(state.update(held, false, true, later), Actions::default());
        assert!(!state.reset().release_ptt);
    }

    #[test]
    fn policy_cannot_be_bypassed_by_any_combination_of_held_keys() {
        let now = Instant::now();
        for flags in 0..64 {
            let sample = Sample {
                modifiers: Modifiers {
                    alt: flags & 1 != 0,
                    control: flags & 2 != 0,
                    shift: flags & 4 != 0,
                    windows: flags & 8 != 0,
                },
                overlay_down: flags & 16 != 0,
                ptt_down: flags & 32 != 0,
            };
            let mut state = ShortcutState::new(mouse_bindings());
            state.update(Sample::default(), true, true, now);
            assert_eq!(state.update(sample, false, false, now), Actions::default());
            // Re-enabling with the same held keys must still do nothing.
            assert_eq!(state.update(sample, true, true, now), Actions::default());
        }
    }

    #[cfg(not(windows))]
    #[test]
    fn other_hosts_do_not_claim_native_shortcut_samples() {
        assert!(matches!(
            sample_configured(Bindings::default()),
            Err(SampleError::UnsupportedHost)
        ));
    }
}
