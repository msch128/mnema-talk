//! Policy independent of window APIs. Only the session/media owner may supply state.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct Context {
    pub voice_ready: bool,
    pub trusted_media_ready: bool,
    pub game_active: bool,
    pub input_available: bool,
    pub locked: bool,
    pub widget_enabled: bool,
}

#[derive(Debug, Default, PartialEq, Eq)]
pub struct GamingState {
    context: Context,
    overlay_open: bool,
    ptt_pressed: bool,
}

impl GamingState {
    fn voice_allowed(&self) -> bool {
        self.context.voice_ready && self.context.trusted_media_ready && !self.context.locked
    }

    fn overlay_allowed(&self) -> bool {
        self.voice_allowed() && self.context.game_active && self.context.input_available
    }

    pub fn update(&mut self, next: Context) {
        // Transition invalidates held input; a fresh press is required after recovery.
        if self.context != next {
            self.ptt_pressed = false;
        }
        self.context = next;
        if !self.overlay_allowed() {
            self.overlay_open = false;
        }
    }

    pub fn widget_visible(&self) -> bool {
        self.voice_allowed() && self.context.game_active && self.context.widget_enabled
    }

    pub fn toggle_overlay(&mut self, repeated: bool) {
        if !repeated && self.overlay_allowed() {
            self.overlay_open = !self.overlay_open;
        }
    }

    pub fn overlay_open(&self) -> bool {
        self.overlay_open
    }

    pub fn press_ptt(&mut self) {
        self.ptt_pressed = self.voice_allowed() && self.context.input_available;
    }

    pub fn release_ptt(&mut self) {
        self.ptt_pressed = false;
    }

    pub fn ptt_pressed(&self) -> bool {
        self.ptt_pressed
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn ready() -> Context {
        Context {
            voice_ready: true,
            trusted_media_ready: true,
            game_active: true,
            input_available: true,
            locked: false,
            widget_enabled: true,
        }
    }

    #[test]
    fn exhaustive_widget_and_shortcut_policy() {
        for flags in 0..64 {
            let c = Context {
                voice_ready: flags & 1 != 0,
                trusted_media_ready: flags & 2 != 0,
                game_active: flags & 4 != 0,
                input_available: flags & 8 != 0,
                locked: flags & 16 != 0,
                widget_enabled: flags & 32 != 0,
            };
            let mut state = GamingState::default();
            state.update(c);
            state.toggle_overlay(false);
            state.press_ptt();
            let voice = c.voice_ready && c.trusted_media_ready && !c.locked;
            assert_eq!(
                state.widget_visible(),
                voice && c.game_active && c.widget_enabled
            );
            assert_eq!(
                state.overlay_open(),
                voice && c.game_active && c.input_available
            );
            assert_eq!(state.ptt_pressed(), voice && c.input_available);
        }
    }

    #[test]
    fn loss_of_voice_game_crypto_input_or_lock_closes_overlay_and_releases_ptt() {
        let variants = [
            Context {
                voice_ready: false,
                ..ready()
            },
            Context {
                trusted_media_ready: false,
                ..ready()
            },
            Context {
                game_active: false,
                ..ready()
            },
            Context {
                input_available: false,
                ..ready()
            },
            Context {
                locked: true,
                ..ready()
            },
        ];
        for next in variants {
            let mut state = GamingState::default();
            state.update(ready());
            state.toggle_overlay(false);
            state.press_ptt();
            state.update(next);
            assert!(!state.overlay_open());
            assert!(!state.ptt_pressed());
            state.update(ready());
            assert!(!state.overlay_open());
            assert!(!state.ptt_pressed());
        }
    }

    #[test]
    fn repeat_does_not_toggle_and_hiding_widget_does_not_disable_overlay() {
        let mut state = GamingState::default();
        state.update(Context {
            widget_enabled: false,
            ..ready()
        });
        state.toggle_overlay(true);
        assert!(!state.overlay_open());
        state.toggle_overlay(false);
        assert!(state.overlay_open());
        state.toggle_overlay(true);
        assert!(state.overlay_open());
        assert!(!state.widget_visible());
        state.toggle_overlay(false);
        assert!(!state.overlay_open());
        state.press_ptt();
        state.release_ptt();
        assert!(!state.ptt_pressed());
    }
}
