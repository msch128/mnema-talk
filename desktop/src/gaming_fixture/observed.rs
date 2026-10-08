//! Bounded observed-game executor. A retained process/window double-check is
//! evidence about observations, not an atomic proof against foreign HWND reuse.
use crate::gaming_fixture::{
    Error, RenderOwner,
    policy::{FocusCompletion, FocusRequest, GameObservation, WindowIdentity},
};
use crate::native_input::SafetyGate;
use std::time::Instant;

pub struct Snapshot {
    pub game: GameObservation,
    pub foreground: Option<WindowIdentity>,
}
pub trait ObservedDriver {
    fn now(&self) -> Instant;
    /// Includes retained process, ownership, health, display and WinEvent fences.
    fn snapshot(&mut self) -> Result<Snapshot, Error>;
    /// Only exact registered overlay or the retained, bound game is accepted.
    fn focus_once(&mut self, request: FocusRequest) -> Result<(), Error>;
}

/// Revalidates the owner-bound game BEFORE any effect. There is no arbitrary
/// HWND parameter, general own-process exemption, synthesized input or retry.
pub fn execute(
    owner: &mut RenderOwner,
    driver: &mut impl ObservedDriver,
    request: FocusRequest,
    gate: &SafetyGate,
    epoch: u64,
) -> Result<FocusCompletion, Error> {
    let result = (|| {
        if !owner.focus_current(request, driver.now()) {
            return Err(Error::Owner);
        }
        let before = driver.snapshot()?;
        owner.observe(before.foreground, Some(before.game));
        // observe rejects a changed bound game and unrelated foreground even
        // when the newer snapshot itself is healthy.
        if !owner.focus_current(request, driver.now()) || !gate.permissions(epoch, driver.now()).0 {
            return Err(Error::SafetyExpired);
        }
        driver.focus_once(request)?;
        let after = driver.snapshot()?;
        owner.observe(after.foreground, Some(after.game));
        if !owner.focus_current(request, driver.now()) || !gate.permissions(epoch, driver.now()).0 {
            return Err(Error::SafetyExpired);
        }
        match owner.complete_focus(request, after.foreground, after.game, driver.now()) {
            completion @ FocusCompletion::Applied(_) => Ok(completion),
            _ => Err(Error::Owner),
        }
    })();
    if result.is_err() {
        owner.invalidate();
        gate.revoke();
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::gaming_fixture::policy::{GameOwner, Generation, ProcessIdentity, SessionGrant};
    use std::time::Duration;
    fn id(handle: usize, pid: u32) -> WindowIdentity {
        WindowIdentity::from_native(
            handle,
            ProcessIdentity::from_native(pid, 100).unwrap(),
            Generation::new(1).unwrap(),
        )
        .unwrap()
    }
    struct Driver {
        now: Instant,
        game: WindowIdentity,
        foreground: Option<WindowIdentity>,
        effects: usize,
        abort: bool,
        delay: Duration,
    }
    impl ObservedDriver for Driver {
        fn now(&self) -> Instant {
            self.now
        }
        fn snapshot(&mut self) -> Result<Snapshot, Error> {
            Ok(Snapshot {
                game: GameObservation::from_native(self.game, true),
                foreground: self.foreground,
            })
        }
        fn focus_once(&mut self, request: FocusRequest) -> Result<(), Error> {
            self.effects += 1;
            self.now += self.delay;
            if !self.abort {
                self.foreground = Some(request.target());
            }
            Ok(())
        }
    }
    fn setup() -> (RenderOwner, Driver, SafetyGate, FocusRequest) {
        let now = Instant::now();
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
        owner.register_overlay(id(20, 2)).unwrap();
        owner.observe(
            Some(id(10, 1)),
            Some(GameObservation::from_native(id(10, 1), true)),
        );
        let request = owner.toggle(now).unwrap().focus.unwrap();
        let gate = SafetyGate::new(now);
        gate.heartbeat(gate.epoch(), now);
        gate.core_heartbeat(gate.epoch(), now);
        assert!(gate.grant(gate.epoch(), true, false, now));
        (
            owner,
            Driver {
                now,
                game: id(10, 1),
                foreground: Some(id(10, 1)),
                effects: 0,
                abort: false,
                delay: Duration::ZERO,
            },
            gate,
            request,
        )
    }
    #[test]
    fn changed_bound_game_rejected_before_any_focus() {
        let (mut owner, mut driver, gate, request) = setup();
        driver.game = id(11, 1);
        driver.foreground = Some(driver.game);
        assert!(execute(&mut owner, &mut driver, request, &gate, gate.epoch()).is_err());
        assert_eq!(driver.effects, 0);
        assert_eq!(owner.frame().presentation(), Default::default());
    }
    #[test]
    fn own_main_does_not_authorize_effect() {
        let (mut owner, mut driver, gate, request) = setup();
        driver.foreground = Some(id(21, 2));
        assert!(execute(&mut owner, &mut driver, request, &gate, gate.epoch()).is_err());
        assert_eq!(driver.effects, 0);
    }
    #[test]
    fn exact_overlay_keeps_game_lease_and_return_uses_one_effect() {
        let (mut owner, mut driver, gate, request) = setup();
        let epoch = gate.epoch();
        assert!(execute(&mut owner, &mut driver, request, &gate, epoch).is_ok());
        assert!(owner.frame().presentation().overlay);
        let close = owner.toggle(driver.now).unwrap().focus.unwrap();
        assert!(execute(&mut owner, &mut driver, close, &gate, epoch).is_ok());
        assert_eq!(driver.effects, 2);
        assert_eq!(driver.foreground, Some(id(10, 1)));
        assert!(owner.frame().presentation().passive_widget);
    }
    #[test]
    fn denied_or_expired_focus_cannot_commit_overlay() {
        for expired in [false, true] {
            let (mut owner, mut driver, gate, request) = setup();
            driver.abort = !expired;
            if expired {
                driver.delay = Duration::from_millis(251);
            }
            assert!(execute(&mut owner, &mut driver, request, &gate, gate.epoch()).is_err());
            assert_eq!(driver.effects, 1);
            assert_eq!(owner.frame().presentation(), Default::default());
        }
    }
    #[test]
    fn voice_loss_before_executor_rejects_old_request() {
        let (mut owner, mut driver, gate, request) = setup();
        owner.apply_session(SessionGrant::new(
            Generation::new(1).unwrap(),
            Generation::new(2).unwrap(),
            false,
            false,
            false,
        ));
        assert!(execute(&mut owner, &mut driver, request, &gate, gate.epoch()).is_err());
        assert_eq!(driver.effects, 0);
    }
}
