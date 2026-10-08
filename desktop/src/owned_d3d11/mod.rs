//! Owned D3D11 synthetic fixture. No external-game hook, Tauri overlay,
//! production media grant or endpoint-compromise protection is implemented.
#[cfg(windows)]
pub mod native;

#[derive(Clone, Copy, Default, Debug, PartialEq, Eq)]
pub struct DrawPolicy {
    voice: bool,
    overlay: bool,
    reported_fullscreen: bool,
}
impl DrawPolicy {
    pub fn observe(&mut self, own_foreground: bool, healthy: bool, dxgi_result: Option<bool>) {
        self.reported_fullscreen = healthy && dxgi_result == Some(true);
        if !own_foreground || !healthy || dxgi_result.is_none() {
            self.voice = false;
            self.overlay = false;
        }
    }
    pub fn toggle_voice(&mut self) {
        self.voice = !self.voice;
        if !self.voice {
            self.overlay = false;
        }
    }
    pub fn toggle_overlay(&mut self) {
        if self.voice {
            self.overlay = !self.overlay;
        }
    }
    pub fn hud(self) -> bool {
        self.voice
    }
    pub fn overlay(self) -> bool {
        self.voice && self.overlay
    }
    pub fn dxgi_fullscreen_reported(self) -> bool {
        self.reported_fullscreen
    }
}
/// An uncertain mode request cannot erase its own unconfirmed timeout.
#[derive(Default)]
pub struct FullscreenTrial {
    deadline: Option<std::time::Instant>,
}
impl FullscreenTrial {
    pub fn request<E>(
        &mut self,
        fullscreen: bool,
        now: std::time::Instant,
        // The native callback accepts only a successfully double-checked mode.
        effect: impl FnOnce() -> Result<(), E>,
    ) -> Result<(), E> {
        if fullscreen {
            self.deadline = Some(now + std::time::Duration::from_secs(15));
        }
        effect()?;
        if !fullscreen {
            self.deadline = None;
        }
        Ok(())
    }
    pub fn confirm(&mut self) {
        self.deadline = None;
    }
    pub fn pending(&self) -> bool {
        self.deadline.is_some()
    }
    pub fn overdue(&self, now: std::time::Instant) -> bool {
        self.deadline.is_some_and(|deadline| now >= deadline)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn geometry_or_request_never_proves_swap_chain_mode() {
        let mut policy = DrawPolicy::default();
        policy.observe(true, true, Some(false));
        assert!(!policy.dxgi_fullscreen_reported());
        policy.observe(true, true, None);
        assert!(!policy.dxgi_fullscreen_reported());
        policy.observe(true, true, Some(true));
        assert!(policy.dxgi_fullscreen_reported());
        policy.observe(true, false, Some(true));
        assert!(!policy.dxgi_fullscreen_reported());
    }
    #[test]
    fn no_voice_no_overlay_and_focus_loss_does_not_restore_voice() {
        let mut policy = DrawPolicy::default();
        policy.toggle_overlay();
        assert!(!policy.hud());
        policy.toggle_voice();
        policy.toggle_overlay();
        assert!(policy.overlay());
        policy.observe(false, true, Some(false));
        assert!(!policy.hud());
        policy.observe(true, true, Some(false));
        assert!(!policy.hud());
    }
    #[test]
    fn voice_leave_and_unknown_swapchain_clear_overlay() {
        let mut policy = DrawPolicy::default();
        policy.toggle_voice();
        policy.toggle_overlay();
        policy.toggle_voice();
        assert!(!policy.overlay());
        policy.toggle_voice();
        policy.toggle_overlay();
        policy.observe(true, true, None);
        assert!(!policy.hud() && !policy.overlay());
    }
    #[test]
    fn accepted_request_with_uncertain_query_cannot_drop_trial_timeout() {
        let now = std::time::Instant::now();
        let mut trial = FullscreenTrial::default();
        let swap_fullscreen = std::cell::Cell::new(false);
        let result = trial.request(true, now, || {
            swap_fullscreen.set(true); // Native mode request accepted.
            Err::<(), ()>(()) // Immediate actual query failed/mismatched.
        });
        // This is the same Result propagated out of the actual render loop;
        // it cannot be ignored as successful and carries an armed trial.
        assert!(result.is_err() && trial.pending());
        assert!(swap_fullscreen.get() && trial.pending());
        assert!(trial.overdue(now + std::time::Duration::from_secs(15)));
    }
    #[test]
    fn uncertain_windowed_request_keeps_timeout_until_verified_rollback() {
        let now = std::time::Instant::now();
        let mut trial = FullscreenTrial::default();
        trial.request(true, now, || Ok::<(), ()>(())).unwrap();
        assert!(trial.request(false, now, || Err::<(), ()>(())).is_err());
        assert!(trial.pending());
        trial.request(false, now, || Ok::<(), ()>(())).unwrap();
        assert!(!trial.pending());
        trial.request(true, now, || Ok::<(), ()>(())).unwrap();
        trial.confirm();
        assert!(!trial.pending());
    }
}
