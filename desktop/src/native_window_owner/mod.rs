//! Ignored-only adapter feasibility experiment. No app wiring or runtime proof.
pub use crate::game_owner as policy;

use policy::{
    FocusCompletion, FocusKind, FocusRequest, GameObservation, GameOwner, Transition,
    WindowIdentity,
};
use std::time::Instant;

#[cfg(windows)]
pub mod native;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Error {
    UnsupportedHost,
    InvalidWindow,
    ProcessUnavailable,
    ProcessChanged,
    WindowChanged,
    Hidden,
    Minimized,
    Cloaked,
    CloakingUnknown,
    NotOwnSurface,
    WrongThread,
    LifecycleUnavailable,
    UnsupportedGameLifetime,
    StaleEffect,
    FocusDenied,
    IdentityExhausted,
}

pub struct CheckedGame {
    pub observation: GameObservation,
    // True requires a synchronous/cooperative lifetime fence. Polling and
    // queued out-of-context WinEvents cannot manufacture this proof.
    pub lifetime_supported: bool,
}

pub trait FocusDriver {
    fn now(&self) -> Instant;
    fn check_game(&mut self) -> Result<CheckedGame, Error>;
    fn check_own_overlay(&mut self, expected: WindowIdentity) -> Result<(), Error>;
    fn focus_once(&mut self, expected: WindowIdentity) -> Result<(), Error>;
    fn own_foreground(&mut self) -> Result<WindowIdentity, Error>;
}

#[derive(Debug)]
pub struct Failure {
    pub error: Error,
    pub transition: Transition,
}

/// The only effect executor. No retry, synthetic input, general window focus or
/// user-supplied HWND. Unknown external lifetime fails before native focus.
pub fn execute_focus(
    owner: &mut GameOwner,
    request: FocusRequest,
    driver: &mut impl FocusDriver,
) -> Result<FocusCompletion, Failure> {
    let fail = |owner: &mut GameOwner, error| Failure {
        error,
        transition: owner.invalidate(),
    };
    if !owner.focus_request_current(request, driver.now()) {
        return Err(Failure {
            error: Error::StaleEffect,
            transition: owner.tick(driver.now()),
        });
    }
    if request.kind() != FocusKind::OpenOverlay {
        return Err(fail(owner, Error::UnsupportedGameLifetime));
    }
    let game = driver.check_game().map_err(|error| fail(owner, error))?;
    if !game.lifetime_supported {
        return Err(fail(owner, Error::UnsupportedGameLifetime));
    }
    driver
        .check_own_overlay(request.target())
        .map_err(|error| fail(owner, error))?;
    // Validation itself may have crossed the deadline; recheck at execution.
    if !owner.focus_request_current(request, driver.now()) {
        return Err(fail(owner, Error::StaleEffect));
    }
    driver
        .focus_once(request.target())
        .map_err(|error| fail(owner, error))?;
    let after = driver.check_game().map_err(|error| fail(owner, error))?;
    if !after.lifetime_supported {
        return Err(fail(owner, Error::UnsupportedGameLifetime));
    }
    driver
        .check_own_overlay(request.target())
        .map_err(|error| fail(owner, error))?;
    let actual = driver
        .own_foreground()
        .map_err(|error| fail(owner, error))?;
    Ok(owner.complete_focus(request, Some(actual), after.observation, driver.now()))
}

#[cfg(test)]
mod tests {
    use super::*;
    use policy::{Generation, ProcessIdentity, SessionGrant, SurfaceRole};
    use std::time::Duration;

    fn id(handle: usize, pid: u32) -> WindowIdentity {
        WindowIdentity::from_native(
            handle,
            ProcessIdentity::from_native(pid, 100).unwrap(),
            Generation::new(1).unwrap(),
        )
        .unwrap()
    }
    fn setup() -> (GameOwner, FocusRequest, Driver) {
        let now = Instant::now();
        let mut owner =
            GameOwner::new(ProcessIdentity::from_native(2, 100).unwrap(), true).unwrap();
        owner.apply_session(SessionGrant::new(
            Generation::new(1).unwrap(),
            Generation::new(1).unwrap(),
            true,
            true,
            false,
        ));
        owner
            .register_surface(SurfaceRole::Overlay, id(200, 2))
            .unwrap();
        owner.observe_foreground(
            Some(id(100, 1)),
            Some(GameObservation::from_native(id(100, 1), true)),
        );
        let request = owner.request_toggle(false, now).unwrap().focus.unwrap();
        (
            owner,
            request,
            Driver {
                now,
                calls: 0,
                lifetime: true,
                target_valid: true,
                after_game_valid: true,
                actual: id(200, 2),
                delay: Duration::ZERO,
                denied: false,
            },
        )
    }
    struct Driver {
        now: Instant,
        calls: usize,
        lifetime: bool,
        target_valid: bool,
        after_game_valid: bool,
        actual: WindowIdentity,
        delay: Duration,
        denied: bool,
    }
    impl FocusDriver for Driver {
        fn now(&self) -> Instant {
            self.now
        }
        fn check_game(&mut self) -> Result<CheckedGame, Error> {
            Ok(CheckedGame {
                observation: GameObservation::from_native(
                    id(100, 1),
                    self.calls == 0 || self.after_game_valid,
                ),
                lifetime_supported: self.lifetime,
            })
        }
        fn check_own_overlay(&mut self, expected: WindowIdentity) -> Result<(), Error> {
            if self.target_valid && expected == id(200, 2) {
                Ok(())
            } else {
                Err(Error::NotOwnSurface)
            }
        }
        fn focus_once(&mut self, _expected: WindowIdentity) -> Result<(), Error> {
            self.calls += 1;
            self.now += self.delay;
            if self.denied {
                Err(Error::FocusDenied)
            } else {
                Ok(())
            }
        }
        fn own_foreground(&mut self) -> Result<WindowIdentity, Error> {
            Ok(self.actual)
        }
    }
    #[test]
    fn unsupported_external_lifetime_never_executes_focus() {
        let (mut owner, request, mut driver) = setup();
        driver.lifetime = false;
        let failure = execute_focus(&mut owner, request, &mut driver).unwrap_err();
        assert_eq!(failure.error, Error::UnsupportedGameLifetime);
        assert!(failure.transition.clear_views && failure.transition.release_ptt);
        assert_eq!(driver.calls, 0);
        assert!(!owner.game_context_active());
    }
    #[test]
    fn stale_request_does_not_focus_or_revoke_new_context() {
        let (mut owner, request, mut driver) = setup();
        owner.invalidate();
        owner.observe_foreground(
            Some(id(100, 1)),
            Some(GameObservation::from_native(id(100, 1), true)),
        );
        assert_eq!(
            execute_focus(&mut owner, request, &mut driver)
                .unwrap_err()
                .error,
            Error::StaleEffect
        );
        assert!(owner.game_context_active());
        assert_eq!(driver.calls, 0);
    }
    #[test]
    fn wrong_surface_and_denied_focus_are_never_retried() {
        let (mut owner, request, mut driver) = setup();
        driver.target_valid = false;
        assert_eq!(
            execute_focus(&mut owner, request, &mut driver)
                .unwrap_err()
                .error,
            Error::NotOwnSurface
        );
        assert_eq!(driver.calls, 0);
        let (mut owner, request, mut driver) = setup();
        driver.denied = true;
        assert_eq!(
            execute_focus(&mut owner, request, &mut driver)
                .unwrap_err()
                .error,
            Error::FocusDenied
        );
        assert_eq!(driver.calls, 1);
        assert!(!owner.game_context_active());
    }
    #[test]
    fn actual_foreign_focus_and_game_loss_after_call_cannot_commit() {
        let (mut owner, request, mut driver) = setup();
        driver.actual = id(300, 3);
        assert!(matches!(
            execute_focus(&mut owner, request, &mut driver).unwrap(),
            FocusCompletion::Rejected(_)
        ));
        assert_eq!(driver.calls, 1);
        assert!(!owner.game_context_active());
        let (mut owner, request, mut driver) = setup();
        driver.after_game_valid = false;
        assert!(matches!(
            execute_focus(&mut owner, request, &mut driver).unwrap(),
            FocusCompletion::Rejected(_)
        ));
        assert!(!owner.game_context_active());
    }
    #[test]
    fn long_native_call_cannot_commit_after_deadline() {
        let (mut owner, request, mut driver) = setup();
        driver.delay = Duration::from_millis(300);
        assert!(matches!(
            execute_focus(&mut owner, request, &mut driver).unwrap(),
            FocusCompletion::Rejected(_)
        ));
        assert_eq!(driver.calls, 1);
    }
    #[test]
    fn proven_mock_lifetime_can_commit_only_exact_overlay_once() {
        let (mut owner, request, mut driver) = setup();
        assert!(matches!(
            execute_focus(&mut owner, request, &mut driver).unwrap(),
            FocusCompletion::Applied(_)
        ));
        assert_eq!(driver.calls, 1);
        assert!(owner.presentation().overlay);
        let returning = owner
            .request_toggle(false, driver.now)
            .unwrap()
            .focus
            .unwrap();
        assert_eq!(
            execute_focus(&mut owner, returning, &mut driver)
                .unwrap_err()
                .error,
            Error::UnsupportedGameLifetime
        );
        assert_eq!(driver.calls, 1);
    }
}
