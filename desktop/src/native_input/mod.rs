//! Private bounded-input/session experiment. No renderer commands or media wiring.
use std::sync::atomic::{AtomicU32, AtomicU64, Ordering};
use std::sync::mpsc::{Receiver, TryRecvError};
use std::time::{Duration, Instant};

#[cfg(windows)]
pub mod native;
pub use crate::shortcuts;

pub const CLEAR_CONTEXT: u32 = 1;
pub const RELEASE_PTT: u32 = 2;
const DEADLINE_MS: u64 = 250;
static NEXT_GATE_OWNER: AtomicU64 = AtomicU64::new(1);

pub enum ControlPoll<T> {
    Message(T),
    Idle,
    OwnerGone,
}
/// A disconnected authoritative owner is a lifecycle loss, not an idle poll.
pub fn receive_control<T>(receiver: &Receiver<T>, gate: &SafetyGate) -> ControlPoll<T> {
    match receiver.try_recv() {
        Ok(command) => ControlPoll::Message(command),
        Err(TryRecvError::Empty) => ControlPoll::Idle,
        Err(TryRecvError::Disconnected) => {
            gate.revoke();
            ControlPoll::OwnerGone
        }
    }
}

/// Native core keeps this outside the GUI/input loop. Encoders must check the
/// epoch/deadline at the transmission boundary; a queued release is insufficient.
pub struct SafetyGate {
    owner: u64,
    origin: Instant,
    state: AtomicU64, // epoch << 2 | overlay permission | PTT permission
    heartbeat_epoch: AtomicU64,
    heartbeat_ms: AtomicU64,
    core_epoch: AtomicU64,
    core_ms: AtomicU64,
    effects: AtomicU32,
    action_sequence: AtomicU64,
}

impl Default for SafetyGate {
    fn default() -> Self {
        Self::new(Instant::now())
    }
}

impl SafetyGate {
    pub fn new(origin: Instant) -> Self {
        let owner = NEXT_GATE_OWNER
            .try_update(Ordering::SeqCst, Ordering::SeqCst, |owner| {
                owner.checked_add(1)
            })
            .unwrap_or(0);
        Self {
            owner,
            origin,
            state: AtomicU64::new(if owner == 0 { 0 } else { 4 }),
            heartbeat_epoch: AtomicU64::new(0),
            heartbeat_ms: AtomicU64::new(0),
            core_epoch: AtomicU64::new(0),
            core_ms: AtomicU64::new(0),
            effects: AtomicU32::new(CLEAR_CONTEXT | RELEASE_PTT),
            action_sequence: AtomicU64::new(1),
        }
    }
    pub fn epoch(&self) -> u64 {
        self.state.load(Ordering::SeqCst) >> 2
    }
    fn elapsed(&self, now: Instant) -> u64 {
        now.saturating_duration_since(self.origin)
            .as_millis()
            .min(u128::from(u64::MAX)) as u64
    }
    pub fn revoke(&self) {
        // Exhaustion retires at epoch zero permanently. Old grants never revive.
        let _ = self
            .state
            .try_update(Ordering::SeqCst, Ordering::SeqCst, |state| {
                let epoch = state >> 2;
                Some(if epoch == 0 || epoch == u64::MAX >> 2 {
                    0
                } else {
                    (epoch + 1) << 2
                })
            });
        self.effects
            .fetch_or(CLEAR_CONTEXT | RELEASE_PTT, Ordering::SeqCst);
    }
    pub fn heartbeat(&self, epoch: u64, now: Instant) {
        if epoch == 0 || self.epoch() != epoch {
            return;
        }
        if self.state.load(Ordering::SeqCst) & 3 != 0 && !self.fresh(epoch, now) {
            self.watchdog(now);
            return; // A recovering input loop cannot renew an expired grant.
        }
        self.heartbeat_ms.store(self.elapsed(now), Ordering::SeqCst);
        self.heartbeat_epoch.store(epoch, Ordering::SeqCst);
    }
    pub fn core_heartbeat(&self, epoch: u64, now: Instant) {
        if epoch == 0 || self.epoch() != epoch {
            return;
        }
        if self.state.load(Ordering::SeqCst) & 3 != 0 && !self.fresh(epoch, now) {
            self.watchdog(now);
            return;
        }
        self.core_ms.store(self.elapsed(now), Ordering::SeqCst);
        self.core_epoch.store(epoch, Ordering::SeqCst);
    }
    /// Only called after independently checked native availability and a fresh
    /// authoritative Voice/crypto/game grant for this exact epoch.
    pub fn grant(&self, epoch: u64, overlay: bool, ptt: bool, now: Instant) -> bool {
        if epoch == 0 || !self.fresh(epoch, now) {
            return false;
        }
        self.state
            .compare_exchange(
                epoch << 2,
                (epoch << 2) | u64::from(overlay) | (u64::from(ptt) << 1),
                Ordering::SeqCst,
                Ordering::SeqCst,
            )
            .is_ok()
    }
    fn fresh(&self, epoch: u64, now: Instant) -> bool {
        self.heartbeat_epoch.load(Ordering::SeqCst) == epoch
            && self
                .elapsed(now)
                .saturating_sub(self.heartbeat_ms.load(Ordering::SeqCst))
                <= DEADLINE_MS
            && self.core_epoch.load(Ordering::SeqCst) == epoch
            && self
                .elapsed(now)
                .saturating_sub(self.core_ms.load(Ordering::SeqCst))
                <= DEADLINE_MS
    }
    pub fn permissions(&self, epoch: u64, now: Instant) -> (bool, bool) {
        let before = self.state.load(Ordering::SeqCst);
        if epoch == 0
            || before >> 2 != epoch
            || !self.fresh(epoch, now)
            || self.state.load(Ordering::SeqCst) != before
        {
            return (false, false);
        }
        (before & 1 != 0, before & 2 != 0)
    }
    /// Call from an independent native watchdog thread, not WM_TIMER or Vue.
    pub fn watchdog(&self, now: Instant) {
        let state = self.state.load(Ordering::SeqCst);
        if state & 3 != 0 && !self.fresh(state >> 2, now) {
            let next = if state >> 2 == u64::MAX >> 2 {
                0
            } else {
                ((state >> 2) + 1) << 2
            };
            if self
                .state
                .compare_exchange(state, next, Ordering::SeqCst, Ordering::SeqCst)
                .is_ok()
            {
                self.effects
                    .fetch_or(CLEAR_CONTEXT | RELEASE_PTT, Ordering::SeqCst);
            }
        }
    }
    pub fn take_effects(&self) -> u32 {
        self.effects.swap(0, Ordering::SeqCst)
    }
    pub fn release(&self) {
        self.next_action_sequence();
        self.effects.fetch_or(RELEASE_PTT, Ordering::SeqCst);
    }
    fn next_action_sequence(&self) -> Option<u64> {
        match self
            .action_sequence
            .try_update(Ordering::SeqCst, Ordering::SeqCst, |sequence| {
                sequence.checked_add(1)
            }) {
            Ok(previous) => Some(previous + 1),
            Err(_) => {
                self.revoke();
                None
            }
        }
    }
    pub fn stamp(&self, epoch: u64, actions: shortcuts::Actions) -> Option<StampedActions> {
        Some(StampedActions {
            owner: self.owner,
            epoch,
            actions,
            sequence: self.next_action_sequence()?,
        })
    }
    /// Reject delayed press/toggle edges after a newer action or PTT release.
    /// Release effects themselves must always be honored, regardless of epoch.
    pub fn action_current(&self, stamped: StampedActions, now: Instant) -> bool {
        if self.owner == 0 || stamped.owner != self.owner {
            return false;
        }
        let allowed = self.permissions(stamped.epoch, now);
        (!stamped.actions.toggle_overlay || allowed.0)
            && (!stamped.actions.press_ptt || allowed.1)
            && self.action_sequence.load(Ordering::SeqCst) == stamped.sequence
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SessionEvent {
    Lock,
    Unlock,
    Disconnect,
    Connect,
    Suspend,
    Resume,
    DisplayChanged,
    DeviceChanged,
}

#[derive(Default)]
pub struct Availability {
    locked: bool,
    disconnected: bool,
    suspended: bool,
    observed: bool,
}
impl Availability {
    pub fn event(&mut self, event: SessionEvent, gate: &SafetyGate) {
        match event {
            SessionEvent::Lock => self.locked = true,
            SessionEvent::Unlock => self.locked = false,
            SessionEvent::Disconnect => self.disconnected = true,
            SessionEvent::Connect => self.disconnected = false,
            SessionEvent::Suspend => self.suspended = true,
            SessionEvent::Resume => self.suspended = false,
            SessionEvent::DisplayChanged | SessionEvent::DeviceChanged => {}
        }
        self.observed = false;
        gate.revoke(); // Recovery does not restore a prior Voice/PTT grant.
    }
    pub fn observe(
        &mut self,
        session_active: bool,
        thread_input: bool,
        input_desktop: bool,
        gate: &SafetyGate,
    ) -> bool {
        let ready = !self.locked
            && !self.disconnected
            && !self.suspended
            && session_active
            && thread_input
            && input_desktop;
        if self.observed && !ready {
            gate.revoke();
        }
        self.observed = ready;
        ready
    }
    pub fn ready(&self) -> bool {
        self.observed
    }
}

#[derive(Clone, Copy)]
pub struct Config {
    pub overlay: shortcuts::Binding,
    pub ptt: Option<shortcuts::Binding>,
}
impl Config {
    pub fn bindings(self) -> Result<shortcuts::Bindings, shortcuts::BindingError> {
        shortcuts::Bindings::new(self.overlay, self.ptt)
    }
}

/// No event history, text, mouse coordinates, device names or diagnostics.
/// Unbound events are discarded immediately; only two primary states and eight
/// modifier sides survive a callback. Native adapter rejects device-source swaps.
pub struct BoundedInput {
    config: Config,
    state: shortcuts::ShortcutState,
    sides: [bool; 8],
    overlay_down: bool,
    ptt_down: bool,
    needs_release: [bool; 2],
}
impl BoundedInput {
    pub fn new(config: Config) -> Result<Self, shortcuts::BindingError> {
        Ok(Self {
            config,
            state: shortcuts::ShortcutState::new(config.bindings()?),
            sides: [false; 8],
            overlay_down: false,
            ptt_down: false,
            needs_release: [true; 2],
        })
    }
    pub fn reset(&mut self) {
        self.state.reset();
        self.sides = [false; 8];
        self.overlay_down = false;
        self.ptt_down = false;
        self.needs_release = [true; 2];
    }
    pub fn interested(&self, key: u16) -> bool {
        key == self.config.overlay.key()
            || self.config.ptt.is_some_and(|binding| key == binding.key())
            || matches!(key, 0xa0..=0xa5 | 0x5b | 0x5c)
    }
    pub fn primary_keys(&self) -> (u16, Option<u16>) {
        (
            self.config.overlay.key(),
            self.config.ptt.map(|binding| binding.key()),
        )
    }
    /// Polling zero can only revoke/reset, never establish a fresh release or
    /// generate an action. A real raw break must rearm after ambiguous input.
    pub fn reconcile_primary_up(
        &mut self,
        overlay_physical_down: bool,
        ptt_physical_down: bool,
    ) -> bool {
        if (self.overlay_down && !overlay_physical_down) || (self.ptt_down && !ptt_physical_down) {
            self.reset();
            true
        } else {
            false
        }
    }
    pub fn reconcile_modifiers(&mut self, observed: shortcuts::Modifiers) -> bool {
        if self.modifiers() != observed {
            self.reset();
            true
        } else {
            false
        }
    }
    /// An actual primary break is required after startup/recovery, avoiding
    /// GetAsyncKeyState's ambiguous zero result and held-key automatic unmute.
    pub fn event(
        &mut self,
        key: u16,
        down: bool,
        allowed: (bool, bool),
        now: Instant,
    ) -> shortcuts::Actions {
        let side = match key {
            0xa4 => Some(0),
            0xa5 => Some(1),
            0xa2 => Some(2),
            0xa3 => Some(3),
            0xa0 => Some(4),
            0xa1 => Some(5),
            0x5b => Some(6),
            0x5c => Some(7),
            _ => None,
        };
        let mut relevant = false;
        if let Some(index) = side {
            self.sides[index] = down;
            relevant = true;
        }
        if key == self.config.overlay.key() {
            self.overlay_down = down;
            if !down {
                self.needs_release[0] = false;
            }
            relevant = true;
        }
        if self.config.ptt.is_some_and(|binding| key == binding.key()) {
            self.ptt_down = down;
            if !down {
                self.needs_release[1] = false;
            }
            relevant = true;
        }
        if !relevant {
            return shortcuts::Actions::default();
        }
        self.tick(allowed, now)
    }
    pub fn tick(&mut self, allowed: (bool, bool), now: Instant) -> shortcuts::Actions {
        let sample = shortcuts::Sample {
            overlay_down: self.overlay_down,
            ptt_down: self.ptt_down,
            modifiers: self.modifiers(),
        };
        self.state.update(
            sample,
            allowed.0 && !self.needs_release[0],
            allowed.1 && !self.needs_release[1],
            now,
        )
    }
    pub fn modifiers(&self) -> shortcuts::Modifiers {
        shortcuts::Modifiers {
            alt: self.sides[0] || self.sides[1],
            control: self.sides[2] || self.sides[3],
            shift: self.sides[4] || self.sides[5],
            windows: self.sides[6] || self.sides[7],
        }
    }
}

/// Only action edges leave the native owner. A consumer must check current
/// gate.permissions(epoch, now) before every press/toggle, never replay edges.
#[derive(Clone, Copy, Debug)]
pub struct StampedActions {
    owner: u64,
    pub epoch: u64,
    pub actions: shortcuts::Actions,
    sequence: u64,
}

pub const WATCHDOG_INTERVAL: Duration = Duration::from_millis(25);

#[cfg(test)]
mod tests {
    use super::*;
    fn config() -> Config {
        Config {
            overlay: shortcuts::Binding::new(
                0x4d,
                shortcuts::Modifiers {
                    alt: true,
                    ..Default::default()
                },
            )
            .unwrap(),
            ptt: Some(shortcuts::Binding::new(0x56, Default::default()).unwrap()),
        }
    }
    fn heartbeat(gate: &SafetyGate, epoch: u64, now: Instant) {
        gate.heartbeat(epoch, now);
        gate.core_heartbeat(epoch, now);
    }
    #[test]
    fn independent_deadline_revokes_stalled_event_loop() {
        let now = Instant::now();
        let gate = SafetyGate::new(now);
        let epoch = gate.epoch();
        gate.take_effects();
        heartbeat(&gate, epoch, now);
        assert!(gate.grant(epoch, true, true, now));
        let late = now + Duration::from_millis(251);
        assert_eq!(gate.permissions(epoch, late), (false, false));
        gate.watchdog(late);
        assert_eq!(gate.take_effects(), CLEAR_CONTEXT | RELEASE_PTT);
        gate.heartbeat(epoch, late);
        assert!(!gate.grant(epoch, true, true, late));
    }
    #[test]
    fn unlock_reconnect_require_fresh_grant() {
        let now = Instant::now();
        let gate = SafetyGate::new(now);
        let mut availability = Availability::default();
        assert!(availability.observe(true, true, true, &gate));
        let epoch = gate.epoch();
        heartbeat(&gate, epoch, now);
        assert!(gate.grant(epoch, true, true, now));
        availability.event(SessionEvent::Disconnect, &gate);
        assert!(!availability.observe(true, true, true, &gate)); // OpenInputDesktop can succeed disconnected.
        availability.event(SessionEvent::Connect, &gate);
        availability.event(SessionEvent::Lock, &gate);
        assert!(!availability.observe(true, true, true, &gate));
        availability.event(SessionEvent::Unlock, &gate);
        assert!(availability.observe(true, true, true, &gate));
        assert_eq!(gate.permissions(gate.epoch(), now), (false, false));
    }
    #[test]
    fn desktop_display_suspend_each_invalidate() {
        let gate = SafetyGate::default();
        let mut availability = Availability::default();
        assert!(availability.observe(true, true, true, &gate));
        let epoch = gate.epoch();
        assert!(!availability.observe(true, false, true, &gate));
        assert!(gate.epoch() > epoch);
        for event in [
            SessionEvent::DisplayChanged,
            SessionEvent::Suspend,
            SessionEvent::Resume,
            SessionEvent::DeviceChanged,
        ] {
            let epoch = gate.epoch();
            availability.event(event, &gate);
            assert!(gate.epoch() > epoch);
            assert!(!availability.ready());
        }
    }
    #[test]
    fn raw_ten_ms_tap_preserved_and_repeat_ignored() {
        let now = Instant::now();
        let mut input = BoundedInput::new(config()).unwrap();
        input.event(0x4d, false, (true, true), now);
        input.event(0xa4, true, (true, true), now);
        assert!(
            input
                .event(0x4d, true, (true, true), now + Duration::from_millis(1))
                .toggle_overlay
        );
        assert!(
            !input
                .event(0x4d, true, (true, true), now + Duration::from_millis(2))
                .toggle_overlay
        );
        assert!(
            !input
                .event(0x4d, false, (true, true), now + Duration::from_millis(10))
                .toggle_overlay
        );
    }
    #[test]
    fn held_recovery_never_auto_unmutes() {
        let now = Instant::now();
        let mut input = BoundedInput::new(config()).unwrap();
        assert!(!input.event(0x56, true, (true, true), now).press_ptt);
        input.event(0x56, false, (true, true), now);
        assert!(input.event(0x56, true, (true, true), now).press_ptt);
        input.reset();
        assert!(!input.event(0x56, true, (true, true), now).press_ptt);
        input.event(0x56, false, (true, true), now);
        assert!(input.event(0x56, true, (true, true), now).press_ptt);
        assert!(input.event(0xa2, true, (true, true), now).release_ptt);
        assert!(!input.event(0xa2, false, (true, true), now).press_ptt);
    }
    #[test]
    fn unbound_events_do_not_change_bounded_state() {
        let now = Instant::now();
        let mut input = BoundedInput::new(config()).unwrap();
        for key in 0..=255 {
            if !matches!(key, 0x4d | 0x56 | 0xa0..=0xa5 | 0x5b | 0x5c) {
                assert_eq!(
                    input.event(key, true, (true, true), now),
                    shortcuts::Actions::default()
                );
            }
        }
        assert_eq!(input.sides, [false; 8]);
        assert!(!input.overlay_down && !input.ptt_down);
    }
    #[test]
    fn generation_exhaustion_retires_gate() {
        let now = Instant::now();
        let gate = SafetyGate::new(now);
        gate.state.store((u64::MAX >> 2) << 2, Ordering::SeqCst);
        gate.revoke();
        assert_eq!(gate.epoch(), 0);
        gate.revoke();
        gate.heartbeat(0, now);
        assert!(!gate.grant(0, true, true, now));
        assert_eq!(gate.permissions(0, now), (false, false));
    }
    #[test]
    fn late_heartbeat_cannot_revive_without_watchdog_scheduling() {
        let now = Instant::now();
        let gate = SafetyGate::new(now);
        let epoch = gate.epoch();
        heartbeat(&gate, epoch, now);
        assert!(gate.grant(epoch, true, true, now));
        let late = now + Duration::from_millis(251);
        gate.heartbeat(epoch, late);
        assert_ne!(gate.epoch(), epoch);
        assert_eq!(gate.permissions(epoch, late), (false, false));
    }
    #[test]
    fn responsive_native_tick_preserves_arming_over_idle() {
        let now = Instant::now();
        let mut input = BoundedInput::new(config()).unwrap();
        input.event(0x56, false, (true, true), now);
        for step in 1..=40 {
            input.tick((true, true), now + Duration::from_millis(step * 25));
        }
        assert!(
            input
                .event(0x56, true, (true, true), now + Duration::from_millis(1001))
                .press_ptt
        );
    }
    #[test]
    fn stalled_core_revokes_even_if_native_input_stays_responsive() {
        let now = Instant::now();
        let gate = SafetyGate::new(now);
        let epoch = gate.epoch();
        heartbeat(&gate, epoch, now);
        assert!(gate.grant(epoch, true, true, now));
        for step in 1..=10 {
            gate.heartbeat(epoch, now + Duration::from_millis(step * 25));
        }
        gate.heartbeat(epoch, now + Duration::from_millis(275));
        assert_ne!(gate.epoch(), epoch);
        assert_eq!(
            gate.permissions(epoch, now + Duration::from_millis(275)),
            (false, false)
        );
    }
    #[test]
    fn late_press_cannot_replay_after_release_effect() {
        let now = Instant::now();
        let gate = SafetyGate::new(now);
        let epoch = gate.epoch();
        heartbeat(&gate, epoch, now);
        assert!(gate.grant(epoch, true, true, now));
        let press = gate
            .stamp(
                epoch,
                shortcuts::Actions {
                    press_ptt: true,
                    ..Default::default()
                },
            )
            .unwrap();
        assert!(gate.action_current(press, now));
        gate.release();
        assert!(!gate.action_current(press, now));
    }
    #[test]
    fn action_from_another_gate_is_rejected_despite_identical_counters() {
        let now = Instant::now();
        let first = SafetyGate::new(now);
        let second = SafetyGate::new(now);
        for gate in [&first, &second] {
            heartbeat(gate, gate.epoch(), now);
            assert!(gate.grant(gate.epoch(), true, true, now));
        }
        let actions = shortcuts::Actions {
            press_ptt: true,
            ..Default::default()
        };
        let foreign = first.stamp(first.epoch(), actions).unwrap();
        let own = second.stamp(second.epoch(), actions).unwrap();
        assert!(second.action_current(own, now));
        assert!(!second.action_current(foreign, now));
    }
    #[test]
    fn owner_channel_loss_revokes_while_empty_channel_keeps_context() {
        let now = Instant::now();
        let gate = SafetyGate::new(now);
        let epoch = gate.epoch();
        heartbeat(&gate, epoch, now);
        assert!(gate.grant(epoch, true, true, now));
        gate.take_effects();
        let (sender, receiver) = std::sync::mpsc::sync_channel::<()>(1);
        assert!(matches!(
            receive_control(&receiver, &gate),
            ControlPoll::Idle
        ));
        assert_eq!(gate.permissions(epoch, now), (true, true));
        drop(sender);
        assert!(matches!(
            receive_control(&receiver, &gate),
            ControlPoll::OwnerGone
        ));
        assert_eq!(gate.permissions(epoch, now), (false, false));
        assert_eq!(gate.take_effects(), CLEAR_CONTEXT | RELEASE_PTT);
    }
    #[test]
    fn lost_raw_break_releases_and_held_repeat_cannot_rearm() {
        let now = Instant::now();
        let mut input = BoundedInput::new(config()).unwrap();
        input.event(0x56, false, (true, true), now);
        assert!(input.event(0x56, true, (true, true), now).press_ptt);
        assert!(input.reconcile_primary_up(false, false));
        assert!(!input.event(0x56, true, (true, true), now).press_ptt);
        assert!(input.reconcile_primary_up(false, false));
        assert!(!input.event(0x56, true, (true, true), now).press_ptt);
        input.event(0x56, false, (true, true), now);
        assert!(input.event(0x56, true, (true, true), now).press_ptt);
        assert!(!input.reconcile_primary_up(false, true));
    }
    #[test]
    fn lost_modifier_break_releases_held_primary_without_rearming() {
        let now = Instant::now();
        let alt = shortcuts::Modifiers {
            alt: true,
            ..Default::default()
        };
        let mut input = BoundedInput::new(Config {
            ptt: Some(shortcuts::Binding::new(0x56, alt).unwrap()),
            ..config()
        })
        .unwrap();
        input.event(0x56, false, (true, true), now);
        input.event(0xa4, true, (true, true), now);
        assert!(input.event(0x56, true, (true, true), now).press_ptt);
        assert!(input.reconcile_modifiers(Default::default()));
        input.event(0xa4, true, (true, true), now);
        assert!(!input.event(0x56, true, (true, true), now).press_ptt);
        input.event(0x56, false, (true, true), now);
        assert!(input.event(0x56, true, (true, true), now).press_ptt);
    }
}
