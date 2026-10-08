//! Portable native window-owner policy; it creates no windows and proves no
//! render/fullscreen route. Inputs belong to the trusted native/session owners,
//! never a renderer. No type in this module is serialized or exposed via IPC.
//!
//! A real adapter must retain a live OS process handle, validate creation time
//! and HWND ownership before every observation/effect, and detect lock, suspend,
//! minimize, cloaking, exit, DPI/monitor changes and lost event delivery. Numeric
//! identities here are comparison tokens, not substitutes for retained handles.

use std::{
    fmt,
    num::NonZeroU64,
    sync::atomic::{AtomicU64, Ordering},
    time::{Duration, Instant},
};

// Requests never leave this process or survive restart. Owner replacement must
// not reuse identity even when its local lease/transaction counters start over.
static NEXT_OWNER_IDENTITY: AtomicU64 = AtomicU64::new(1);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct OwnerIdentity(NonZeroU64);

impl OwnerIdentity {
    fn allocate(counter: &AtomicU64) -> Result<Self, OwnerError> {
        let value = counter
            .try_update(Ordering::Relaxed, Ordering::Relaxed, |current| {
                if current == 0 {
                    None
                } else {
                    current.checked_add(1)
                }
            })
            .map_err(|_| OwnerError::GenerationExhausted)?;
        Ok(Self(
            NonZeroU64::new(value).ok_or(OwnerError::GenerationExhausted)?,
        ))
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Generation(NonZeroU64);

impl Generation {
    pub fn new(value: u64) -> Option<Self> {
        NonZeroU64::new(value).map(Self)
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub struct ProcessIdentity {
    pid: u32,
    creation_time: NonZeroU64,
}

impl ProcessIdentity {
    /// Native adapter only: creation_time is the OS process creation FILETIME.
    pub fn from_native(pid: u32, creation_time: u64) -> Option<Self> {
        if pid == 0 {
            return None;
        }
        Some(Self {
            pid,
            creation_time: NonZeroU64::new(creation_time)?,
        })
    }
}

impl fmt::Debug for ProcessIdentity {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("ProcessIdentity(<native>)")
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub struct WindowIdentity {
    handle: usize,
    process: ProcessIdentity,
    instance: Generation,
}

impl WindowIdentity {
    /// Native adapter only: instance changes after destruction/recreation, even
    /// if Windows reuses the same HWND in the same still-running process.
    pub fn from_native(
        handle: usize,
        process: ProcessIdentity,
        instance: Generation,
    ) -> Option<Self> {
        (handle != 0).then_some(Self {
            handle,
            process,
            instance,
        })
    }
}

impl fmt::Debug for WindowIdentity {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("WindowIdentity(<native>)")
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SessionGrant {
    session: Generation,
    voice: Generation,
    voice_ready: bool,
    trusted_media_ready: bool,
    locked: bool,
}

impl SessionGrant {
    /// Only the authoritative session/media owner may produce this grant.
    pub fn new(
        session: Generation,
        voice: Generation,
        voice_ready: bool,
        trusted_media_ready: bool,
        locked: bool,
    ) -> Self {
        Self {
            session,
            voice,
            voice_ready,
            trusted_media_ready,
            locked,
        }
    }

    fn allowed(self) -> bool {
        self.voice_ready && self.trusted_media_ready && !self.locked
    }
}

#[derive(Debug, Clone, Copy)]
pub struct GameObservation {
    identity: WindowIdentity,
    eligible: bool,
}

impl GameObservation {
    /// `eligible` requires a fresh native liveness, visible/not-minimized,
    /// not-cloaked, display geometry and ownership check. False covers any loss.
    pub fn from_native(identity: WindowIdentity, eligible: bool) -> Self {
        Self { identity, eligible }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SurfaceRole {
    Main,
    Overlay,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FocusKind {
    OpenOverlay,
    ReturnGame,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FocusRequest {
    owner: OwnerIdentity,
    transaction: Generation,
    lease: Generation,
    kind: FocusKind,
    target: WindowIdentity,
}

impl FocusRequest {
    pub fn kind(self) -> FocusKind {
        self.kind
    }

    /// Resolve this opaque identity through the native adapter's own registry;
    /// never accept a renderer-supplied handle or focus arbitrary windows.
    pub fn target(self) -> WindowIdentity {
        self.target
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct Transition {
    pub clear_views: bool,
    pub release_ptt: bool,
    pub focus: Option<FocusRequest>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FocusCompletion {
    Applied(Transition),
    Rejected(Transition),
    Stale,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OwnerError {
    NotOwnOverlay,
    NoContext,
    NoOverlay,
    Busy,
    GenerationExhausted,
    /// Caller must apply these clear/release effects before handling the error.
    Retired(Transition),
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct Presentation {
    pub passive_widget: bool,
    pub overlay: bool,
    pub overlay_widget: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Phase {
    Dormant,
    Game,
    Overlay,
}

#[derive(Debug, Clone, Copy)]
struct Lease {
    id: Generation,
    game: WindowIdentity,
    overlay: Option<WindowIdentity>,
}

#[derive(Debug, Clone, Copy)]
struct Pending {
    request: FocusRequest,
    deadline: Instant,
}

#[derive(Debug)]
pub struct GameOwner {
    identity: OwnerIdentity,
    own_process: ProcessIdentity,
    registered_overlay: Option<WindowIdentity>,
    grant: Option<SessionGrant>,
    lease: Option<Lease>,
    phase: Phase,
    pending: Option<Pending>,
    next_generation: u64,
    widget_enabled: bool,
    retired: bool,
}

impl GameOwner {
    /// Fallible rather than recycling an identity on process-wide exhaustion.
    pub fn new(own_process: ProcessIdentity, widget_enabled: bool) -> Result<Self, OwnerError> {
        Ok(Self {
            identity: OwnerIdentity::allocate(&NEXT_OWNER_IDENTITY)?,
            own_process,
            registered_overlay: None,
            grant: None,
            lease: None,
            phase: Phase::Dormant,
            pending: None,
            next_generation: 1,
            widget_enabled,
            retired: false,
        })
    }

    fn generation(&mut self) -> Result<Generation, OwnerError> {
        let current = Generation::new(self.next_generation);
        let next = self.next_generation.checked_add(1);
        match (current, next) {
            (Some(current), Some(next)) if !self.retired => {
                self.next_generation = next;
                Ok(current)
            }
            _ => {
                self.retired = true;
                Err(OwnerError::Retired(self.invalidate()))
            }
        }
    }

    /// Hide and revoke before dispatching any other effects. Adapter also stops
    /// media PTT independently; renderer liveness is not a microphone watchdog.
    pub fn invalidate(&mut self) -> Transition {
        self.lease = None;
        self.phase = Phase::Dormant;
        self.pending = None;
        Transition {
            clear_views: true,
            release_ptt: true,
            focus: None,
        }
    }

    pub fn apply_session(&mut self, grant: SessionGrant) -> Transition {
        let changed = self.grant != Some(grant);
        self.grant = Some(grant);
        if self.retired || changed || !grant.allowed() {
            self.invalidate()
        } else {
            Transition::default()
        }
    }

    pub fn register_surface(
        &mut self,
        role: SurfaceRole,
        identity: WindowIdentity,
    ) -> Result<Transition, OwnerError> {
        if role != SurfaceRole::Overlay || identity.process != self.own_process {
            return Err(OwnerError::NotOwnOverlay);
        }
        if self.registered_overlay == Some(identity) {
            return Ok(Transition::default());
        }
        self.registered_overlay = Some(identity);
        Ok(self.invalidate())
    }

    pub fn unregister_surface(&mut self, identity: WindowIdentity) -> Transition {
        if self.registered_overlay == Some(identity) {
            self.registered_overlay = None;
            self.invalidate()
        } else {
            Transition::default()
        }
    }

    pub fn set_widget_enabled(&mut self, enabled: bool) {
        self.widget_enabled = enabled;
    }

    pub fn presentation(&self) -> Presentation {
        if self.lease.is_none() || self.pending.is_some() {
            return Presentation::default();
        }
        Presentation {
            passive_widget: self.phase == Phase::Game && self.widget_enabled,
            overlay: self.phase == Phase::Overlay,
            overlay_widget: self.phase == Phase::Overlay && self.widget_enabled,
        }
    }

    pub fn game_context_active(&self) -> bool {
        self.lease.is_some()
    }

    /// Adapter supplies both current foreground and live health of the bound
    /// game, including while overlay has focus. Background discovery alone can
    /// never acquire a lease. No general own-process or Main-window exemption.
    pub fn observe_foreground(
        &mut self,
        foreground: Option<WindowIdentity>,
        game: Option<GameObservation>,
    ) -> Transition {
        if self.retired || !self.grant.is_some_and(SessionGrant::allowed) {
            return self.invalidate();
        }
        let Some(game) = game.filter(|game| game.eligible) else {
            return self.invalidate();
        };
        let Some(foreground) = foreground else {
            return self.invalidate();
        };
        if let Some(lease) = self.lease {
            if game.identity != lease.game {
                return self.invalidate();
            }
            if foreground == lease.game {
                if self.phase == Phase::Overlay && self.pending.is_none() {
                    self.phase = Phase::Game;
                    return Transition {
                        clear_views: true,
                        release_ptt: true,
                        focus: None,
                    };
                }
                return Transition::default();
            }
            let own_overlay = lease.overlay == Some(foreground)
                && self.registered_overlay == Some(foreground)
                && (self.phase == Phase::Overlay || self.pending.is_some());
            if own_overlay {
                return Transition::default();
            }
            return self.invalidate();
        }
        if foreground != game.identity || foreground.process == self.own_process {
            return self.invalidate();
        }
        let Ok(id) = self.generation() else {
            return self.invalidate();
        };
        self.lease = Some(Lease {
            id,
            game: game.identity,
            overlay: None,
        });
        self.phase = Phase::Game;
        Transition {
            clear_views: true,
            release_ptt: true,
            focus: None,
        }
    }

    /// Returns a bounded focus intention. Before executing it the adapter must
    /// call `focus_request_current` and revalidate native game/window health.
    pub fn request_toggle(
        &mut self,
        repeated: bool,
        now: Instant,
    ) -> Result<Transition, OwnerError> {
        if repeated {
            return Ok(Transition::default());
        }
        if self.retired {
            return Err(OwnerError::Retired(self.invalidate()));
        }
        if self.pending.is_some() {
            return Err(OwnerError::Busy);
        }
        let Some(mut lease) = self.lease else {
            return Err(OwnerError::NoContext);
        };
        let (kind, target) = match self.phase {
            Phase::Game => {
                let target = self.registered_overlay.ok_or(OwnerError::NoOverlay)?;
                lease.overlay = Some(target);
                (FocusKind::OpenOverlay, target)
            }
            Phase::Overlay => (FocusKind::ReturnGame, lease.game),
            Phase::Dormant => return Err(OwnerError::NoContext),
        };
        let transaction = self.generation()?;
        let request = FocusRequest {
            owner: self.identity,
            transaction,
            lease: lease.id,
            kind,
            target,
        };
        self.lease = Some(lease);
        self.pending = Some(Pending {
            request,
            deadline: now + Duration::from_millis(300),
        });
        Ok(Transition {
            clear_views: true,
            release_ptt: true,
            focus: Some(request),
        })
    }

    /// This check must occur immediately before the single native focus call,
    /// not just when queueing. A stale request cannot target a new game/session.
    pub fn focus_request_current(&self, request: FocusRequest, now: Instant) -> bool {
        request.owner == self.identity
            && self.grant.is_some_and(SessionGrant::allowed)
            && self.lease.is_some_and(|lease| lease.id == request.lease)
            && self
                .pending
                .is_some_and(|pending| pending.request == request && now < pending.deadline)
    }

    /// Actual focus and revalidated game identity, not the focus API's success
    /// flag, commit a transition. Current failed effects revoke; stale ones do
    /// nothing to a newer lease. Call `observe_foreground` for all OS events too.
    pub fn complete_focus(
        &mut self,
        request: FocusRequest,
        actual: Option<WindowIdentity>,
        game: GameObservation,
        now: Instant,
    ) -> FocusCompletion {
        if !self
            .pending
            .is_some_and(|pending| pending.request == request)
        {
            return FocusCompletion::Stale;
        }
        if !self.focus_request_current(request, now)
            || actual != Some(request.target)
            || !game.eligible
            || !self.lease.is_some_and(|lease| lease.game == game.identity)
        {
            return FocusCompletion::Rejected(self.invalidate());
        }
        self.pending = None;
        self.phase = match request.kind {
            FocusKind::OpenOverlay => Phase::Overlay,
            FocusKind::ReturnGame => Phase::Game,
        };
        FocusCompletion::Applied(Transition {
            release_ptt: true,
            ..Transition::default()
        })
    }

    pub fn tick(&mut self, now: Instant) -> Transition {
        if self.pending.is_some_and(|pending| now >= pending.deadline) {
            self.invalidate()
        } else {
            Transition::default()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn generation(value: u64) -> Generation {
        Generation::new(value).unwrap()
    }
    fn process(pid: u32, created: u64) -> ProcessIdentity {
        ProcessIdentity::from_native(pid, created).unwrap()
    }
    fn window(handle: usize, pid: u32, created: u64, instance: u64) -> WindowIdentity {
        WindowIdentity::from_native(handle, process(pid, created), generation(instance)).unwrap()
    }
    fn grant() -> SessionGrant {
        SessionGrant::new(generation(1), generation(1), true, true, false)
    }
    fn game() -> WindowIdentity {
        window(100, 10, 1000, 1)
    }
    fn overlay() -> WindowIdentity {
        window(200, 20, 2000, 1)
    }
    fn health() -> GameObservation {
        GameObservation::from_native(game(), true)
    }
    fn owner() -> GameOwner {
        let mut owner = GameOwner::new(process(20, 2000), true).unwrap();
        owner.apply_session(grant());
        owner
            .register_surface(SurfaceRole::Overlay, overlay())
            .unwrap();
        owner.observe_foreground(Some(game()), Some(health()));
        owner
    }
    fn open(owner: &mut GameOwner, now: Instant) -> FocusRequest {
        let request = owner.request_toggle(false, now).unwrap().focus.unwrap();
        assert_eq!(request.kind(), FocusKind::OpenOverlay);
        assert!(matches!(
            owner.complete_focus(request, Some(overlay()), health(), now),
            FocusCompletion::Applied(_)
        ));
        request
    }

    #[test]
    fn native_identities_reject_zero_and_debug_does_not_expose_handles() {
        assert!(Generation::new(0).is_none());
        assert!(ProcessIdentity::from_native(0, 1).is_none());
        assert!(ProcessIdentity::from_native(1, 0).is_none());
        assert!(WindowIdentity::from_native(0, process(1, 1), generation(1)).is_none());
        assert_eq!(format!("{:?}", game()), "WindowIdentity(<native>)");
    }

    #[test]
    fn bound_overlay_focus_keeps_game_context_and_embeds_widget() {
        let now = Instant::now();
        let mut owner = owner();
        assert!(owner.presentation().passive_widget);
        let opening = owner.request_toggle(false, now).unwrap();
        assert!(opening.release_ptt && opening.clear_views);
        assert_eq!(owner.presentation(), Presentation::default());
        owner.observe_foreground(Some(overlay()), Some(health()));
        assert!(owner.game_context_active());
        // Observation cannot commit an unconfirmed focus transaction.
        assert!(!owner.presentation().overlay);
        let request = opening.focus.unwrap();
        assert!(matches!(
            owner.complete_focus(request, Some(overlay()), health(), now),
            FocusCompletion::Applied(_)
        ));
        assert_eq!(
            owner.presentation(),
            Presentation {
                passive_widget: false,
                overlay: true,
                overlay_widget: true
            }
        );
        owner.observe_foreground(Some(overlay()), Some(health()));
        assert!(owner.presentation().overlay);
        owner.set_widget_enabled(false);
        assert_eq!(
            owner.presentation(),
            Presentation {
                overlay: true,
                ..Presentation::default()
            }
        );
    }

    #[test]
    fn main_foreign_and_reused_own_surface_do_not_inherit_overlay_exemption() {
        let now = Instant::now();
        for unrelated in [
            window(201, 20, 2000, 1),
            window(200, 99, 9000, 1),
            window(200, 20, 2000, 2),
            window(300, 30, 3000, 1),
        ] {
            let mut owner = owner();
            open(&mut owner, now);
            let transition = owner.observe_foreground(Some(unrelated), Some(health()));
            assert!(transition.release_ptt && transition.clear_views);
            assert!(!owner.game_context_active());
            assert_eq!(owner.presentation(), Presentation::default());
        }
        let mut owner = owner();
        assert_eq!(
            owner.register_surface(SurfaceRole::Main, window(201, 20, 2000, 1)),
            Err(OwnerError::NotOwnOverlay)
        );
        assert_eq!(
            owner.register_surface(SurfaceRole::Overlay, window(201, 99, 9000, 1)),
            Err(OwnerError::NotOwnOverlay)
        );
        // A registered overlay cannot acquire its own game lease or open itself.
        owner.invalidate();
        owner.observe_foreground(
            Some(overlay()),
            Some(GameObservation::from_native(overlay(), true)),
        );
        assert!(!owner.game_context_active());
    }

    #[test]
    fn voice_crypto_lock_and_generation_changes_clear_lease_without_autoresume() {
        let now = Instant::now();
        for next in [
            SessionGrant::new(generation(1), generation(1), false, true, false),
            SessionGrant::new(generation(1), generation(1), true, false, false),
            SessionGrant::new(generation(1), generation(1), true, true, true),
            SessionGrant::new(generation(2), generation(1), true, true, false),
            SessionGrant::new(generation(1), generation(2), true, true, false),
        ] {
            let mut owner = owner();
            open(&mut owner, now);
            assert!(owner.apply_session(next).release_ptt);
            owner.apply_session(grant());
            assert!(!owner.game_context_active());
            assert_eq!(owner.presentation(), Presentation::default());
            assert_eq!(owner.request_toggle(false, now), Err(OwnerError::NoContext));
        }
    }

    #[test]
    fn exit_minimize_unknown_health_and_process_reuse_revoke_even_with_overlay_focus() {
        let now = Instant::now();
        for unhealthy in [
            None,
            Some(GameObservation::from_native(game(), false)),
            Some(GameObservation::from_native(window(100, 10, 1001, 1), true)),
            Some(GameObservation::from_native(window(100, 10, 1000, 2), true)),
        ] {
            let mut owner = owner();
            open(&mut owner, now);
            assert!(
                owner
                    .observe_foreground(Some(overlay()), unhealthy)
                    .release_ptt
            );
            assert!(!owner.game_context_active());
            assert_eq!(owner.presentation(), Presentation::default());
        }
    }

    #[test]
    fn stale_open_and_return_requests_cannot_focus_or_resurrect_new_lease() {
        let now = Instant::now();
        let mut owner = owner();
        let stale_open = owner.request_toggle(false, now).unwrap().focus.unwrap();
        owner.invalidate();
        owner.observe_foreground(Some(game()), Some(health()));
        assert!(!owner.focus_request_current(stale_open, now));
        assert_eq!(
            owner.complete_focus(stale_open, Some(overlay()), health(), now),
            FocusCompletion::Stale
        );
        assert!(owner.presentation().passive_widget);
        open(&mut owner, now);
        let stale_return = owner.request_toggle(false, now).unwrap().focus.unwrap();
        owner.invalidate();
        owner.observe_foreground(Some(game()), Some(health()));
        open(&mut owner, now);
        assert!(!owner.focus_request_current(stale_return, now));
        assert_eq!(
            owner.complete_focus(stale_return, Some(game()), health(), now),
            FocusCompletion::Stale
        );
        assert!(owner.presentation().overlay);
    }

    #[test]
    fn foreign_focus_during_opening_and_denied_actual_focus_abort() {
        let now = Instant::now();
        let mut owner = owner();
        let request = owner.request_toggle(false, now).unwrap().focus.unwrap();
        owner.observe_foreground(Some(window(400, 40, 4000, 1)), Some(health()));
        assert_eq!(
            owner.complete_focus(request, Some(overlay()), health(), now),
            FocusCompletion::Stale
        );
        assert!(!owner.game_context_active());
        for actual in [None, Some(game()), Some(window(400, 40, 4000, 1))] {
            let mut owner = self::owner();
            let request = owner.request_toggle(false, now).unwrap().focus.unwrap();
            assert!(matches!(
                owner.complete_focus(request, actual, health(), now),
                FocusCompletion::Rejected(Transition {
                    release_ptt: true,
                    clear_views: true,
                    ..
                })
            ));
            assert!(!owner.game_context_active());
        }
    }

    #[test]
    fn opening_deadline_and_repeat_do_not_leave_infinite_exemptions() {
        let now = Instant::now();
        let mut owner = owner();
        assert_eq!(owner.request_toggle(true, now), Ok(Transition::default()));
        let request = owner.request_toggle(false, now).unwrap().focus.unwrap();
        assert_eq!(owner.request_toggle(false, now), Err(OwnerError::Busy));
        assert!(owner.focus_request_current(request, now + Duration::from_millis(299)));
        assert!(!owner.focus_request_current(request, now + Duration::from_millis(300)));
        assert!(owner.tick(now + Duration::from_millis(300)).release_ptt);
        assert!(!owner.game_context_active());
        assert_eq!(
            owner.complete_focus(request, Some(overlay()), health(), now),
            FocusCompletion::Stale
        );
    }

    #[test]
    fn return_requires_actual_bound_game_and_old_overlay_cannot_reopen_itself() {
        let now = Instant::now();
        let mut owner = owner();
        open(&mut owner, now);
        let returning = owner.request_toggle(false, now).unwrap();
        let request = returning.focus.unwrap();
        assert_eq!(request.kind(), FocusKind::ReturnGame);
        assert_eq!(request.target(), game());
        assert_eq!(owner.presentation(), Presentation::default());
        owner.observe_foreground(Some(game()), Some(health()));
        assert!(matches!(
            owner.complete_focus(request, Some(game()), health(), now),
            FocusCompletion::Applied(_)
        ));
        assert!(owner.presentation().passive_widget);
        owner.observe_foreground(Some(overlay()), Some(health()));
        assert!(!owner.game_context_active());
    }

    #[test]
    fn replacing_registered_overlay_and_null_foreground_remove_current_lease() {
        let now = Instant::now();
        let mut owner = owner();
        open(&mut owner, now);
        let replacement = window(202, 20, 2000, 2);
        assert!(
            owner
                .register_surface(SurfaceRole::Overlay, replacement)
                .unwrap()
                .release_ptt
        );
        assert!(!owner.game_context_active());
        assert_eq!(owner.unregister_surface(overlay()), Transition::default());
        owner.observe_foreground(Some(game()), Some(health()));
        assert!(owner.observe_foreground(None, Some(health())).clear_views);
        assert!(!owner.game_context_active());
        assert!(owner.unregister_surface(replacement).clear_views);
    }

    #[test]
    fn background_game_and_missing_grant_cannot_acquire_lease() {
        let mut owner = GameOwner::new(process(20, 2000), true).unwrap();
        owner.observe_foreground(Some(game()), Some(health()));
        assert!(!owner.game_context_active());
        owner.apply_session(grant());
        owner.observe_foreground(Some(window(201, 20, 2000, 1)), Some(health()));
        assert!(!owner.game_context_active());
        owner.observe_foreground(Some(game()), Some(health()));
        assert!(owner.game_context_active());
    }

    #[test]
    fn identical_grants_keep_lease_but_all_unready_grants_block_acquisition() {
        let mut owner = owner();
        assert_eq!(owner.apply_session(grant()), Transition::default());
        assert!(owner.game_context_active());
        for flags in 0..8 {
            let voice = flags & 1 != 0;
            let media = flags & 2 != 0;
            let locked = flags & 4 != 0;
            owner.invalidate();
            owner.apply_session(SessionGrant::new(
                generation(1),
                generation(1),
                voice,
                media,
                locked,
            ));
            owner.observe_foreground(Some(game()), Some(health()));
            assert_eq!(owner.game_context_active(), voice && media && !locked);
        }
    }

    #[test]
    fn generations_never_wrap_and_old_effect_cannot_match_after_exhaustion() {
        let now = Instant::now();
        let mut owner = owner();
        let old_request = owner.request_toggle(false, now).unwrap().focus.unwrap();
        owner.invalidate();
        owner.next_generation = u64::MAX;
        assert!(
            owner
                .observe_foreground(Some(game()), Some(health()))
                .clear_views
        );
        assert!(!owner.game_context_active());
        assert!(!owner.focus_request_current(old_request, now));
        assert_eq!(
            owner.complete_focus(old_request, Some(overlay()), health(), now),
            FocusCompletion::Stale
        );
    }

    #[test]
    fn focus_requests_from_another_owner_cannot_complete_identical_transactions() {
        let now = Instant::now();
        let mut first = owner();
        let mut second = owner();
        let first_open = first.request_toggle(false, now).unwrap().focus.unwrap();
        let second_open = second.request_toggle(false, now).unwrap().focus.unwrap();
        // Deliberately identical native observations, lease and transaction
        // sequences: replacing a native owner must still revoke its intentions.
        assert!(!second.focus_request_current(first_open, now));
        assert_eq!(
            second.complete_focus(first_open, Some(overlay()), health(), now),
            FocusCompletion::Stale
        );
        assert!(second.focus_request_current(second_open, now));
        assert!(matches!(
            second.complete_focus(second_open, Some(overlay()), health(), now),
            FocusCompletion::Applied(_)
        ));
        assert!(matches!(
            first.complete_focus(first_open, Some(overlay()), health(), now),
            FocusCompletion::Applied(_)
        ));
        let first_return = first.request_toggle(false, now).unwrap().focus.unwrap();
        let second_return = second.request_toggle(false, now).unwrap().focus.unwrap();
        assert!(!second.focus_request_current(first_return, now));
        assert_eq!(
            second.complete_focus(first_return, Some(game()), health(), now),
            FocusCompletion::Stale
        );
        assert!(second.focus_request_current(second_return, now));
        assert!(matches!(
            second.complete_focus(second_return, Some(game()), health(), now),
            FocusCompletion::Applied(_)
        ));
        assert!(second.presentation().passive_widget);
    }

    #[test]
    fn exhausted_owner_identity_allocator_fails_without_reset_or_reuse() {
        let counter = AtomicU64::new(u64::MAX - 1);
        let last = OwnerIdentity::allocate(&counter).unwrap();
        assert_eq!(last.0.get(), u64::MAX - 1);
        for _ in 0..2 {
            assert_eq!(
                OwnerIdentity::allocate(&counter),
                Err(OwnerError::GenerationExhausted)
            );
            assert_eq!(counter.load(Ordering::Relaxed), u64::MAX);
        }
        assert_eq!(
            OwnerIdentity::allocate(&AtomicU64::new(0)),
            Err(OwnerError::GenerationExhausted)
        );
    }

    #[test]
    fn transaction_exhaustion_retires_active_overlay_and_cannot_reacquire() {
        let now = Instant::now();
        let mut owner = owner();
        open(&mut owner, now);
        assert!(owner.presentation().overlay);
        owner.next_generation = u64::MAX;
        let error = owner.request_toggle(false, now).unwrap_err();
        assert_eq!(
            error,
            OwnerError::Retired(Transition {
                clear_views: true,
                release_ptt: true,
                focus: None,
            })
        );
        assert!(!owner.game_context_active());
        assert_eq!(owner.presentation(), Presentation::default());
        owner.apply_session(SessionGrant::new(
            generation(9),
            generation(9),
            true,
            true,
            false,
        ));
        owner.observe_foreground(Some(game()), Some(health()));
        assert!(!owner.game_context_active());
        assert_eq!(owner.request_toggle(false, now), Err(error));
        // Even an accidental counter reset cannot unretire this owner instance.
        owner.next_generation = 1;
        owner.observe_foreground(Some(game()), Some(health()));
        assert!(!owner.game_context_active());
    }
}
