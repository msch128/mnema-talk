//! Explicit cooperating fixture integration on Tauri's creating thread.
//! This module is NOT a production client or media authorization provider.
use crate::gaming_fixture::{
    Error, Frame, RenderOwner, Surfaces,
    fixture_child::ChildFixture,
    native_game::{NativeFocus, NativeGame},
    observed,
    policy::{GameOwner, SessionGrant},
    shell::GuiSurfaces,
};
use crate::native_input::{
    CLEAR_CONTEXT, Config, RELEASE_PTT, SafetyGate, StampedActions,
    native::{self, Command},
};
use std::{
    cell::RefCell,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
        mpsc::{self, Receiver, SyncSender},
    },
    thread,
    time::{Duration, Instant},
};
use tauri::Wry;

thread_local! {static CORE:RefCell<Option<FixtureCore>>=const {RefCell::new(None)};}
pub struct Scheduler {
    stop: Arc<AtomicBool>,
    thread: Option<thread::JoinHandle<()>>,
}
pub fn start(
    app: &tauri::AppHandle<Wry>,
    widget: bool,
    config: Config,
) -> Result<Scheduler, Error> {
    let core = FixtureCore::create(app, widget, config)?;
    CORE.with(|slot| *slot.borrow_mut() = Some(core));
    let stop = Arc::new(AtomicBool::new(false));
    let running = stop.clone();
    let queued = Arc::new(AtomicBool::new(false));
    let handle = app.clone();
    let thread = thread::Builder::new()
        .name("mnema-fixture-scheduler".into())
        .spawn(move || {
            while !running.load(Ordering::SeqCst) {
                if queued
                    .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
                    .is_ok()
                {
                    let complete = queued.clone();
                    if handle
                        .run_on_main_thread(move || {
                            CORE.with(|slot| {
                                if let Some(core) = slot.borrow_mut().as_mut() {
                                    core.tick();
                                }
                            });
                            complete.store(false, Ordering::SeqCst);
                        })
                        .is_err()
                    {
                        break;
                    }
                }
                thread::sleep(Duration::from_millis(16));
            }
        })
        .map_err(|_| {
            CORE.with(|slot| slot.borrow_mut().take());
            Error::Render
        })?;
    Ok(Scheduler {
        stop,
        thread: Some(thread),
    })
}
impl Drop for Scheduler {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::SeqCst);
        if let Some(thread) = self.thread.take() {
            let _ = thread.join();
        }
        CORE.with(|slot| slot.borrow_mut().take());
    }
}

pub struct FixtureCore {
    surfaces: GuiSurfaces<Wry>,
    game: NativeGame,
    fixture: ChildFixture,
    owner: RenderOwner,
    gate: Arc<SafetyGate>,
    commands: SyncSender<Command>,
    actions: Receiver<StampedActions>,
    input_thread: Option<thread::JoinHandle<()>>,
    last_grant: Option<SessionGrant>,
    ptt_fixture_latch: bool,
}
impl FixtureCore {
    pub fn create(
        app: &tauri::AppHandle<Wry>,
        widget: bool,
        config: Config,
    ) -> Result<Self, Error> {
        let mut surfaces = GuiSurfaces::create(app)?;
        surfaces.hide_all()?;
        let overlay_hwnd = surfaces.fixture_overlay_hwnd()?;
        let own_process = crate::gaming_fixture::native_game::current_process_identity()?;
        let mut owner =
            RenderOwner::new(GameOwner::new(own_process, widget).map_err(|_| Error::Owner)?);
        owner
            .register_overlay(surfaces.fixture_overlay_guard().identity())
            .map_err(|_| Error::Owner)?;
        let gate = Arc::new(SafetyGate::default());
        let fixture = ChildFixture::spawn(gate.clone(), config.overlay)?;
        let game = unsafe {
            NativeGame::cooperating_child(
                fixture.game_window()?,
                fixture.pid(),
                overlay_hwnd,
                gate.clone(),
            )
        }?;
        let (commands, receiver) = mpsc::sync_channel(32);
        let (sender, actions) = mpsc::sync_channel(32);
        let input_gate = gate.clone();
        let input_thread = thread::Builder::new()
            .name("mnema-fixture-input".into())
            .spawn(move || {
                if native::run(config, input_gate.clone(), receiver, sender).is_err() {
                    input_gate.revoke();
                }
            })
            .map_err(|_| Error::Render)?;
        Ok(Self {
            surfaces,
            game,
            fixture,
            owner,
            gate,
            commands,
            actions,
            input_thread: Some(input_thread),
            last_grant: None,
            ptt_fixture_latch: false,
        })
    }
    fn clear(&mut self) {
        self.owner.invalidate();
        self.ptt_fixture_latch = false;
        let _ = self.surfaces.hide_all();
        let _ = self.surfaces.publish(&Frame::default());
    }
    /// Native main-thread tick. Timestamp is taken HERE, not by the scheduling
    /// thread, so a queued tick cannot renew an expired GUI/core heartbeat.
    pub fn tick(&mut self) {
        let now = Instant::now();
        let effects = self.gate.take_effects();
        if effects & CLEAR_CONTEXT != 0 {
            self.clear();
        }
        if effects & RELEASE_PTT != 0 {
            self.ptt_fixture_latch = false;
        }
        if self.fixture.poll(now).is_err() {
            self.gate.revoke();
            self.clear();
            return;
        }
        let grant = self.fixture.session().grant(now);
        if self.last_grant != Some(grant) {
            self.gate.revoke();
            self.clear();
            self.owner.apply_session(grant);
            self.last_grant = Some(grant);
        }
        if !self.fixture.session().active(now) {
            self.clear();
            return;
        }
        self.owner
            .refresh_synthetic_projection(self.fixture.session(), now);
        let overlay = match self.surfaces.fixture_overlay_hwnd() {
            Ok(hwnd) => hwnd,
            Err(_) => {
                self.gate.revoke();
                self.clear();
                return;
            }
        };
        let observation = self
            .game
            .foreground(self.surfaces.fixture_overlay_guard(), overlay);
        let observation = match observation {
            Ok(observation) if observation.foreground.is_some() => observation,
            _ => {
                self.gate.revoke();
                self.clear();
                return;
            }
        };
        self.owner
            .observe(observation.foreground, Some(observation.game));
        let epoch = self.gate.epoch();
        // Native input availability decides whether this epoch can be granted.
        // Never fabricate its heartbeat or call grant from renderer/core itself.
        if self
            .commands
            .try_send(Command::CoreHeartbeat {
                epoch,
                observed_at: now,
            })
            .is_err()
            || self
                .commands
                .try_send(Command::Grant {
                    epoch,
                    overlay: true,
                    ptt: true,
                })
                .is_err()
        {
            self.gate.revoke();
            self.clear();
            return;
        }
        for _ in 0..32 {
            let action = match self.actions.try_recv() {
                Ok(action) => action,
                Err(_) => break,
            };
            if action.actions.release_ptt {
                self.ptt_fixture_latch = false;
            }
            if !self.gate.action_current(action, Instant::now()) {
                continue;
            }
            if action.actions.press_ptt {
                self.ptt_fixture_latch = true;
            }
            if action.actions.toggle_overlay {
                let request = match self.owner.toggle(Instant::now()) {
                    Ok(transition) => transition.focus,
                    Err(_) => None,
                };
                if let Some(request) = request {
                    self.ptt_fixture_latch = false;
                    // Clear the pending projection. On ReturnGame keep the exact
                    // foreground overlay HWND alive until direct focus resolves;
                    // hiding it first could redirect focus to an unrelated app.
                    if self.surfaces.publish(&Frame::default()).is_err() {
                        self.gate.revoke();
                        self.clear();
                        return;
                    }
                    let mut driver = NativeFocus {
                        game: &mut self.game,
                        overlay: self.surfaces.fixture_overlay_guard(),
                        overlay_hwnd: overlay,
                        gate: &self.gate,
                        epoch,
                    };
                    if observed::execute(&mut self.owner, &mut driver, request, &self.gate, epoch)
                        .is_err()
                    {
                        self.clear();
                        return;
                    }
                }
            }
        }
        if !self.gate.permissions(epoch, Instant::now()).1 {
            self.ptt_fixture_latch = false;
        }
        if !self.gate.permissions(epoch, Instant::now()).0 {
            // Initial grant may await the dedicated native input pump. Clearing
            // views here does not manufacture a new authorization epoch.
            self.clear();
            return;
        }
        let mut surfaces = BoundSurfaces {
            surfaces: &mut self.surfaces,
            game: &mut self.game,
            session: self.fixture.session(),
            gate: &self.gate,
            epoch,
            latch: &mut self.ptt_fixture_latch,
        };
        if self
            .owner
            .drive(&mut surfaces, &self.gate, epoch, Instant::now())
            .is_err()
        {
            self.gate.revoke();
        }
    }
}
impl Drop for FixtureCore {
    fn drop(&mut self) {
        self.gate.revoke();
        self.clear();
        let _ = self.commands.try_send(Command::Stop);
        // Do not join a possibly blocked native pump on the GUI thread. Sender
        // drops with core, input pump revokes/exits; independent watchdog keeps
        // the permission boundary closed even before that message is serviced.
        self.input_thread.take();
    }
}

struct BoundSurfaces<'a> {
    surfaces: &'a mut GuiSurfaces<Wry>,
    game: &'a mut NativeGame,
    session: &'a crate::gaming_fixture::fixture_session::SyntheticSession,
    gate: &'a SafetyGate,
    epoch: u64,
    latch: &'a mut bool,
}
impl Surfaces for BoundSurfaces<'_> {
    fn hide_all(&mut self) -> Result<(), Error> {
        self.surfaces.hide_all()
    }
    fn prepare_frame(&mut self, frame: &Frame) -> Result<(), Error> {
        if !self.session.active(Instant::now())
            || !self.gate.permissions(self.epoch, Instant::now()).0
        {
            return Err(Error::SafetyExpired);
        }
        let hwnd = self.surfaces.fixture_overlay_hwnd()?;
        if self
            .game
            .foreground(self.surfaces.fixture_overlay_guard(), hwnd)?
            .foreground
            .is_none()
        {
            return Err(Error::Owner);
        }
        self.surfaces.prepare_synthetic(frame)
    }
    fn release_ptt(&mut self) {
        *self.latch = false;
        self.surfaces.release_ptt();
    }
    fn publish(&mut self, frame: &Frame) -> Result<(), Error> {
        self.surfaces.publish(frame)
    }
    fn show_passive_widget(&mut self) -> Result<(), Error> {
        self.surfaces
            .show_synthetic(true, self.game, self.session, self.gate, self.epoch)
    }
    fn show_committed_overlay(&mut self) -> Result<(), Error> {
        self.surfaces
            .show_synthetic(false, self.game, self.session, self.gate, self.epoch)
    }
}
