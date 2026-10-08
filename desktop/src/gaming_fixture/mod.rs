//! Private render integration. Opaque native ownership remains inside Rust.
pub use crate::game_owner as policy;
use crate::native_input::{CLEAR_CONTEXT, RELEASE_PTT, SafetyGate};
use policy::{
    FocusCompletion, FocusRequest, GameObservation, GameOwner, OwnerError, Presentation,
    SessionGrant, Transition, WindowIdentity,
};
use serde::Serialize;
use std::time::Instant;

#[cfg(windows)]
pub mod fixture_window;
#[cfg(feature = "shell")]
pub mod shell;

#[cfg(windows)]
pub mod fixture_child;
pub mod fixture_control;
pub mod fixture_session;
pub mod lifecycle;
#[cfg(all(windows, feature = "shell", feature = "gaming-fixture"))]
pub mod native_game;
pub mod observed;
pub mod options;
#[cfg(all(windows, feature = "shell", feature = "gaming-fixture"))]
pub mod runtime;

pub const PRODUCTION_GAME_RENDER_ENABLED: bool = false;
const _: () = assert!(!PRODUCTION_GAME_RENDER_ENABLED);

#[derive(Clone, Debug, Serialize, PartialEq, Eq)]
pub struct VoiceMember {
    name: String,
    speaking: bool,
    muted: bool,
    sharing: bool,
}
impl VoiceMember {
    #[cfg(any(test, feature = "gaming-fixture"))]
    pub fn local_fixture(name: &str, speaking: bool, muted: bool, sharing: bool) -> Self {
        // Text nodes receive these names. Bound length and reject controls/bidi
        // markers even though this experiment accepts only local fixture input.
        let name: String = name
            .chars()
            .filter(|character| {
                !character.is_control()
                    && !matches!(character, '\u{202a}'..='\u{202e}' | '\u{2066}'..='\u{2069}')
            })
            .take(40)
            .collect();
        Self {
            name: if name.is_empty() {
                "Fixture".into()
            } else {
                name
            },
            speaking,
            muted,
            sharing,
        }
    }
}

#[derive(Clone, Debug, Default, Serialize, PartialEq, Eq)]
pub struct Frame {
    passive_widget: bool,
    overlay: bool,
    overlay_widget: bool,
    members: Vec<VoiceMember>,
}
impl Frame {
    pub fn presentation(&self) -> Presentation {
        Presentation {
            passive_widget: self.passive_widget,
            overlay: self.overlay,
            overlay_widget: self.overlay_widget,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    Owner,
    SafetyExpired,
    NoVoiceProjection,
    Render,
    NativeRouteUnsupported,
}

/// Native driver owns actual windows. It never receives a renderer-supplied HWND.
/// The release method is only a fixture latch here, never a second media owner.
pub trait Surfaces {
    fn hide_all(&mut self) -> Result<(), Error>;
    fn prepare_frame(&mut self, _frame: &Frame) -> Result<(), Error> {
        self.hide_all()
    }
    fn release_ptt(&mut self);
    fn publish(&mut self, frame: &Frame) -> Result<(), Error>;
    fn show_passive_widget(&mut self) -> Result<(), Error>;
    fn show_committed_overlay(&mut self) -> Result<(), Error>;
}

pub struct RenderOwner {
    owner: GameOwner,
    members: Vec<VoiceMember>,
    release_pending: bool,
    projection_grant: Option<SessionGrant>,
}
impl RenderOwner {
    pub fn new(owner: GameOwner) -> Self {
        Self {
            owner,
            members: Vec::new(),
            release_pending: true,
            projection_grant: None,
        }
    }
    fn note(&mut self, transition: Transition) -> Transition {
        self.release_pending |= transition.release_ptt;
        transition
    }
    pub fn apply_session(&mut self, grant: SessionGrant) -> Transition {
        if self.projection_grant != Some(grant) {
            self.members.clear();
            self.projection_grant = Some(grant);
        }
        let transition = self.owner.apply_session(grant);
        self.note(transition)
    }
    pub fn register_overlay(&mut self, identity: WindowIdentity) -> Result<Transition, OwnerError> {
        let transition = self
            .owner
            .register_surface(policy::SurfaceRole::Overlay, identity)?;
        Ok(self.note(transition))
    }
    #[cfg(any(test, feature = "gaming-fixture"))]
    pub fn set_local_fixture(&mut self, members: Vec<VoiceMember>) {
        self.invalidate();
        self.members = members.into_iter().take(8).collect();
    }
    #[cfg(all(windows, feature = "gaming-fixture"))]
    pub(crate) fn refresh_synthetic_projection(
        &mut self,
        session: &fixture_session::SyntheticSession,
        now: Instant,
    ) {
        if session.active(now) && self.projection_grant == Some(session.grant(now)) {
            let (speaking, muted, sharing) = session.states();
            self.members = vec![VoiceMember::local_fixture(
                "Own synthetic voice",
                speaking,
                muted,
                sharing,
            )];
        } else {
            self.members.clear();
        }
    }
    pub fn observe(
        &mut self,
        foreground: Option<WindowIdentity>,
        game: Option<GameObservation>,
    ) -> Transition {
        let transition = self.owner.observe_foreground(foreground, game);
        self.note(transition)
    }
    pub fn toggle(&mut self, now: Instant) -> Result<Transition, OwnerError> {
        match self.owner.request_toggle(false, now) {
            Ok(transition) => Ok(self.note(transition)),
            Err(OwnerError::Retired(transition)) => {
                self.note(transition);
                Err(OwnerError::Retired(transition))
            }
            Err(error) => Err(error),
        }
    }
    pub fn focus_current(&self, request: FocusRequest, now: Instant) -> bool {
        self.owner.focus_request_current(request, now)
    }
    pub fn complete_focus(
        &mut self,
        request: FocusRequest,
        foreground: Option<WindowIdentity>,
        game: GameObservation,
        now: Instant,
    ) -> FocusCompletion {
        let completion = self.owner.complete_focus(request, foreground, game, now);
        if let FocusCompletion::Applied(transition) | FocusCompletion::Rejected(transition) =
            completion
        {
            self.note(transition);
        }
        completion
    }
    pub fn execute_native_focus(
        &mut self,
        request: FocusRequest,
        driver: &mut impl crate::native_window_owner::FocusDriver,
    ) -> Result<FocusCompletion, crate::native_window_owner::Failure> {
        let result = crate::native_window_owner::execute_focus(&mut self.owner, request, driver);
        match &result {
            Ok(FocusCompletion::Applied(transition) | FocusCompletion::Rejected(transition)) => {
                self.note(*transition);
            }
            Err(failure) => {
                self.note(failure.transition);
            }
            _ => {}
        }
        result
    }
    pub fn widget_enabled(&mut self, enabled: bool) {
        self.owner.set_widget_enabled(enabled);
    }
    pub fn invalidate(&mut self) -> Transition {
        let transition = self.owner.invalidate();
        self.note(transition)
    }
    pub fn frame(&self) -> Frame {
        let presentation = self.owner.presentation();
        let visible = presentation.passive_widget || presentation.overlay;
        Frame {
            passive_widget: presentation.passive_widget,
            overlay: presentation.overlay,
            overlay_widget: presentation.overlay_widget,
            members: if visible {
                self.members.clone()
            } else {
                Vec::new()
            },
        }
    }
    fn failed(&mut self, surfaces: &mut impl Surfaces, error: Error) -> Result<(), Error> {
        self.owner.invalidate();
        surfaces.release_ptt();
        // Independent best-effort effects: a hide failure must not suppress
        // projection clearing, and a publish failure must not suppress hiding.
        let _ = surfaces.hide_all();
        let _ = surfaces.publish(&Frame::default());
        Err(error)
    }
    /// Main-thread render transaction: hide/release before publishing or show.
    /// Caller applies native focus separately while owner pending hides all UI.
    pub fn drive(
        &mut self,
        surfaces: &mut impl Surfaces,
        gate: &SafetyGate,
        epoch: u64,
        now: Instant,
    ) -> Result<(), Error> {
        let effects = gate.take_effects();
        let tick = self.owner.tick(now);
        self.note(tick);
        if effects & RELEASE_PTT != 0 || self.release_pending {
            surfaces.release_ptt();
            self.release_pending = false;
        }
        if effects & CLEAR_CONTEXT != 0 {
            self.owner.invalidate();
        }
        if !gate.permissions(epoch, now).0 {
            return self.failed(surfaces, Error::SafetyExpired);
        }
        let frame = self.frame();
        if (frame.passive_widget || frame.overlay) && frame.members.is_empty() {
            return self.failed(surfaces, Error::NoVoiceProjection);
        }
        // Hide first also handles failed publication: old voice data stays hidden.
        if surfaces
            .prepare_frame(&frame)
            .and_then(|()| surfaces.publish(&frame))
            .is_err()
        {
            return self.failed(surfaces, Error::Render);
        }
        // Recheck after UI work; no queue-delayed permission can show a stale view.
        if !gate.permissions(epoch, Instant::now().max(now)).0 {
            return self.failed(surfaces, Error::SafetyExpired);
        }
        let result = if frame.overlay {
            surfaces.show_committed_overlay()
        } else if frame.passive_widget {
            surfaces.show_passive_widget()
        } else {
            Ok(())
        };
        if let Err(error) = result {
            return self.failed(surfaces, error);
        }
        if !gate.permissions(epoch, Instant::now().max(now)).0 {
            return self.failed(surfaces, Error::SafetyExpired);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use policy::{Generation, ProcessIdentity};
    use std::time::Duration;
    #[derive(Default)]
    struct Driver {
        calls: Vec<&'static str>,
        fail_publish: bool,
        fail_hide: bool,
        frames: Vec<Frame>,
    }
    impl Surfaces for Driver {
        fn hide_all(&mut self) -> Result<(), Error> {
            self.calls.push("hide");
            if self.fail_hide {
                Err(Error::Render)
            } else {
                Ok(())
            }
        }
        fn release_ptt(&mut self) {
            self.calls.push("release")
        }
        fn publish(&mut self, frame: &Frame) -> Result<(), Error> {
            self.calls.push("publish");
            self.frames.push(frame.clone());
            if self.fail_publish {
                Err(Error::Render)
            } else {
                Ok(())
            }
        }
        fn show_passive_widget(&mut self) -> Result<(), Error> {
            self.calls.push("widget");
            Ok(())
        }
        fn show_committed_overlay(&mut self) -> Result<(), Error> {
            self.calls.push("overlay");
            Ok(())
        }
    }
    fn identity(handle: usize, pid: u32) -> WindowIdentity {
        WindowIdentity::from_native(
            handle,
            ProcessIdentity::from_native(pid, 100).unwrap(),
            Generation::new(1).unwrap(),
        )
        .unwrap()
    }
    fn setup(now: Instant) -> (RenderOwner, SafetyGate, u64, WindowIdentity, WindowIdentity) {
        // Explicitly simulated identities for portable tests, not real HWNDs.
        let mut owner = RenderOwner::new(
            GameOwner::new(ProcessIdentity::from_native(2, 100).unwrap(), true).unwrap(),
        );
        owner.apply_session(SessionGrant::new(
            Generation::new(1).unwrap(),
            Generation::new(1).unwrap(),
            true,
            true,
            false,
        ));
        let game = identity(10, 1);
        let overlay = identity(20, 2);
        owner.register_overlay(overlay).unwrap();
        owner.set_local_fixture(vec![VoiceMember::local_fixture(
            "Fixture", true, false, false,
        )]);
        let gate = SafetyGate::new(now);
        let epoch = gate.epoch();
        gate.take_effects();
        gate.heartbeat(epoch, now);
        gate.core_heartbeat(epoch, now);
        assert!(gate.grant(epoch, true, false, now));
        owner.observe(Some(game), Some(GameObservation::from_native(game, true)));
        (owner, gate, epoch, game, overlay)
    }
    #[test]
    fn widget_only_after_game_and_voice_with_hide_first() {
        let now = Instant::now();
        let (mut owner, gate, epoch, game, _) = setup(now);
        let mut driver = Driver::default();
        owner.drive(&mut driver, &gate, epoch, now).unwrap();
        assert_eq!(driver.calls, vec!["release", "hide", "publish", "widget"]);
        driver.calls.clear();
        owner.observe(
            Some(identity(30, 3)),
            Some(GameObservation::from_native(game, true)),
        );
        owner.drive(&mut driver, &gate, epoch, now).unwrap();
        assert_eq!(driver.calls, vec!["release", "hide", "publish"]);
        assert!(owner.frame().members.is_empty());
    }
    #[test]
    fn pending_hidden_committed_exact_overlay_shown() {
        let now = Instant::now();
        let (mut owner, gate, epoch, game, overlay) = setup(now);
        let request = owner.toggle(now).unwrap().focus.unwrap();
        let mut driver = Driver::default();
        owner.drive(&mut driver, &gate, epoch, now).unwrap();
        assert_eq!(driver.calls, vec!["release", "hide", "publish"]);
        assert!(owner.focus_current(request, now));
        assert!(matches!(
            owner.complete_focus(
                request,
                Some(overlay),
                GameObservation::from_native(game, true),
                now
            ),
            FocusCompletion::Applied(_)
        ));
        driver.calls.clear();
        owner.drive(&mut driver, &gate, epoch, now).unwrap();
        assert_eq!(driver.calls, vec!["release", "hide", "publish", "overlay"]);
        owner.observe(
            Some(overlay),
            Some(GameObservation::from_native(game, true)),
        );
        assert!(owner.frame().overlay);
    }
    #[test]
    fn voice_loss_watchdog_and_stale_focus_never_show() {
        let now = Instant::now();
        let (mut owner, gate, epoch, game, overlay) = setup(now);
        let request = owner.toggle(now).unwrap().focus.unwrap();
        owner.apply_session(SessionGrant::new(
            Generation::new(1).unwrap(),
            Generation::new(2).unwrap(),
            false,
            false,
            false,
        ));
        assert!(!owner.focus_current(request, now));
        assert_eq!(
            owner.complete_focus(
                request,
                Some(overlay),
                GameObservation::from_native(game, true),
                now
            ),
            FocusCompletion::Stale
        );
        let mut driver = Driver::default();
        gate.watchdog(now + Duration::from_millis(251));
        assert_eq!(
            owner.drive(&mut driver, &gate, epoch, now + Duration::from_millis(251)),
            Err(Error::SafetyExpired)
        );
        assert!(!driver.calls.contains(&"widget") && !driver.calls.contains(&"overlay"));
        assert!(driver.calls.contains(&"release"));
    }
    #[test]
    fn widget_preference_does_not_disable_overlay() {
        let now = Instant::now();
        let (mut owner, gate, epoch, game, overlay) = setup(now);
        owner.widget_enabled(false);
        let mut driver = Driver::default();
        owner.drive(&mut driver, &gate, epoch, now).unwrap();
        assert!(!driver.calls.contains(&"widget"));
        let request = owner.toggle(now).unwrap().focus.unwrap();
        owner.complete_focus(
            request,
            Some(overlay),
            GameObservation::from_native(game, true),
            now,
        );
        driver.calls.clear();
        owner.drive(&mut driver, &gate, epoch, now).unwrap();
        assert!(driver.calls.contains(&"overlay"));
        assert!(!owner.frame().overlay_widget);
    }
    #[test]
    fn failed_publication_never_shows_stale_surface() {
        let now = Instant::now();
        let (mut owner, gate, epoch, _, _) = setup(now);
        let mut driver = Driver {
            fail_publish: true,
            ..Default::default()
        };
        assert_eq!(
            owner.drive(&mut driver, &gate, epoch, now),
            Err(Error::Render)
        );
        assert_eq!(
            driver.calls,
            vec!["release", "hide", "publish", "release", "hide", "publish"]
        );
        assert_eq!(driver.frames.last(), Some(&Frame::default()));
        assert_eq!(owner.frame(), Frame::default());
    }
    #[test]
    fn fixture_projection_is_bounded_and_no_native_identity_is_serialized() {
        let now = Instant::now();
        let (mut owner, _, _, game, _) = setup(now);
        owner.set_local_fixture(
            (0..20)
                .map(|_| {
                    VoiceMember::local_fixture(
                        &format!("\u{202e}\n{}", "X".repeat(100)),
                        false,
                        true,
                        true,
                    )
                })
                .collect(),
        );
        owner.observe(Some(game), Some(GameObservation::from_native(game, true)));
        let frame = owner.frame();
        assert_eq!(frame.members.len(), 8);
        assert!(
            frame
                .members
                .iter()
                .all(|member| member.name == "X".repeat(40))
        );
        let serialized = serde_json::to_value(&frame).unwrap();
        let keys: std::collections::BTreeSet<_> = serialized
            .as_object()
            .unwrap()
            .keys()
            .map(String::as_str)
            .collect();
        assert_eq!(
            keys,
            std::collections::BTreeSet::from([
                "passive_widget",
                "overlay",
                "overlay_widget",
                "members"
            ])
        );
        let member_keys: std::collections::BTreeSet<_> = serialized["members"][0]
            .as_object()
            .unwrap()
            .keys()
            .map(String::as_str)
            .collect();
        assert_eq!(
            member_keys,
            std::collections::BTreeSet::from(["name", "speaking", "muted", "sharing"])
        );
    }
    #[test]
    fn failed_hide_still_clears_member_projection_and_never_shows() {
        let now = Instant::now();
        let (mut owner, gate, epoch, _, _) = setup(now);
        let mut driver = Driver {
            fail_hide: true,
            ..Default::default()
        };
        assert_eq!(
            owner.drive(&mut driver, &gate, epoch, now),
            Err(Error::Render)
        );
        assert_eq!(driver.frames, vec![Frame::default()]);
        assert!(!driver.calls.contains(&"widget") && !driver.calls.contains(&"overlay"));
    }
    #[test]
    fn revoke_during_publication_clears_retained_payload() {
        struct Revoker<'a> {
            inner: Driver,
            gate: &'a SafetyGate,
        }
        impl Surfaces for Revoker<'_> {
            fn hide_all(&mut self) -> Result<(), Error> {
                self.inner.hide_all()
            }
            fn release_ptt(&mut self) {
                self.inner.release_ptt();
            }
            fn publish(&mut self, frame: &Frame) -> Result<(), Error> {
                let result = self.inner.publish(frame);
                self.gate.revoke();
                result
            }
            fn show_passive_widget(&mut self) -> Result<(), Error> {
                self.inner.show_passive_widget()
            }
            fn show_committed_overlay(&mut self) -> Result<(), Error> {
                self.inner.show_committed_overlay()
            }
        }
        let now = Instant::now();
        let (mut owner, gate, epoch, _, _) = setup(now);
        let mut driver = Revoker {
            inner: Default::default(),
            gate: &gate,
        };
        assert_eq!(
            owner.drive(&mut driver, &gate, epoch, now),
            Err(Error::SafetyExpired)
        );
        assert_eq!(driver.inner.frames.len(), 2);
        assert_eq!(driver.inner.frames.last(), Some(&Frame::default()));
        assert!(
            !driver.inner.calls.contains(&"widget") && !driver.inner.calls.contains(&"overlay")
        );
    }
    #[test]
    fn new_voice_grant_requires_new_local_projection() {
        let now = Instant::now();
        let (mut owner, gate, epoch, game, _) = setup(now);
        owner.apply_session(SessionGrant::new(
            Generation::new(1).unwrap(),
            Generation::new(2).unwrap(),
            true,
            true,
            false,
        ));
        owner.observe(Some(game), Some(GameObservation::from_native(game, true)));
        let mut driver = Driver::default();
        assert_eq!(
            owner.drive(&mut driver, &gate, epoch, now),
            Err(Error::NoVoiceProjection)
        );
        assert_eq!(driver.frames.last(), Some(&Frame::default()));
        assert!(!driver.calls.contains(&"widget"));
    }
    #[test]
    fn committed_overlay_can_refresh_projection_without_hiding_foreground() {
        struct Stable {
            inner: Driver,
        }
        impl Surfaces for Stable {
            fn hide_all(&mut self) -> Result<(), Error> {
                self.inner.hide_all()
            }
            fn prepare_frame(&mut self, frame: &Frame) -> Result<(), Error> {
                assert!(frame.presentation().overlay);
                self.inner.calls.push("preserve-exact-overlay");
                Ok(())
            }
            fn release_ptt(&mut self) {
                self.inner.release_ptt();
            }
            fn publish(&mut self, frame: &Frame) -> Result<(), Error> {
                self.inner.publish(frame)
            }
            fn show_passive_widget(&mut self) -> Result<(), Error> {
                self.inner.show_passive_widget()
            }
            fn show_committed_overlay(&mut self) -> Result<(), Error> {
                self.inner.show_committed_overlay()
            }
        }
        let now = Instant::now();
        let (mut owner, gate, epoch, game, overlay) = setup(now);
        let request = owner.toggle(now).unwrap().focus.unwrap();
        owner.complete_focus(
            request,
            Some(overlay),
            GameObservation::from_native(game, true),
            now,
        );
        let mut surfaces = Stable {
            inner: Driver::default(),
        };
        owner.drive(&mut surfaces, &gate, epoch, now).unwrap();
        assert!(!surfaces.inner.calls.contains(&"hide"));
        assert!(surfaces.inner.calls.contains(&"overlay"));
        assert!(owner.frame().presentation().overlay);
        // Preservation cannot suppress fail-closed cleanup on voice loss.
        gate.revoke();
        assert!(owner.drive(&mut surfaces, &gate, epoch, now).is_err());
        assert!(surfaces.inner.calls.contains(&"hide"));
        assert_eq!(surfaces.inner.frames.last(), Some(&Frame::default()));
    }
    #[test]
    fn revoke_during_successful_show_clears_immediately() {
        struct Revoker<'a> {
            inner: Driver,
            gate: &'a SafetyGate,
        }
        impl Surfaces for Revoker<'_> {
            fn hide_all(&mut self) -> Result<(), Error> {
                self.inner.hide_all()
            }
            fn release_ptt(&mut self) {
                self.inner.release_ptt();
            }
            fn publish(&mut self, frame: &Frame) -> Result<(), Error> {
                self.inner.publish(frame)
            }
            fn show_passive_widget(&mut self) -> Result<(), Error> {
                let result = self.inner.show_passive_widget();
                self.gate.revoke();
                result
            }
            fn show_committed_overlay(&mut self) -> Result<(), Error> {
                let result = self.inner.show_committed_overlay();
                self.gate.revoke();
                result
            }
        }
        let now = Instant::now();
        let (mut owner, gate, epoch, _, _) = setup(now);
        let mut driver = Revoker {
            inner: Default::default(),
            gate: &gate,
        };
        assert_eq!(
            owner.drive(&mut driver, &gate, epoch, now),
            Err(Error::SafetyExpired)
        );
        assert_eq!(owner.frame(), Frame::default());
        assert_eq!(driver.inner.frames.last(), Some(&Frame::default()));
        assert_eq!(
            &driver.inner.calls[driver.inner.calls.len() - 3..],
            ["release", "hide", "publish"]
        );
    }
}
