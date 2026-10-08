//! Private synthetic native authenticated media actor; never production MLS authorization.
//! Default feature refuses fixture startup. Native Tauri lifecycle owns window identity.
pub use mnema_private_synthetic_media_policy::{BoundMedia, NativeMediaEvent, Role};
#[cfg(feature = "synthetic-media-fixture")]
mod actor;
#[cfg(feature = "synthetic-media-fixture")]
pub use actor::{EventSink, MediaActor, NativeMediaWindow};
