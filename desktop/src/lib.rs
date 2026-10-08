pub mod discovery;
pub mod game_owner;
pub mod gaming;
pub mod profiles;
pub mod shortcuts;
pub mod windows;

#[cfg(feature = "gaming-fixture")]
pub mod gaming_fixture;
#[cfg(feature = "gaming-fixture")]
pub mod native_input;
#[cfg(feature = "gaming-fixture")]
pub mod native_window_owner;

#[cfg(feature = "owned-d3d11-fixture")]
pub mod owned_d3d11;
