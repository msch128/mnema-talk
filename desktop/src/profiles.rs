//! In-memory target selection owned by the native broker, not the renderer.
//!
//! Selection records a user-approved HTTPS target and discovered community ID.
//! It grants neither authentication nor E2EE/device trust. Every selection
//! attempt and teardown invalidates older operations, including on bad input.
use crate::discovery::normalize_address;
use std::sync::atomic::{AtomicU64, Ordering};
use url::Url;

const MAX_COMMUNITY_ID: usize = 128;
static NEXT_OWNER: AtomicU64 = AtomicU64::new(1);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ProfileError {
    InvalidAddress,
    InvalidCommunity,
    StaleHandle,
    Exhausted,
}

// These handles are native values, not serializable renderer authority.
// Future IPC may map an opaque UI identifier to them inside the broker, but
// must never reconstruct either handle from user-provided generation numbers.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ProfileHandle {
    owner: u64,
    generation: u64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct OperationHandle {
    profile: ProfileHandle,
}

/// Public profile metadata; contains no credentials, keys or readiness flags.
#[derive(Debug)]
pub struct SelectedProfile {
    origin: Url,
    community_id: String,
}

impl SelectedProfile {
    pub fn origin(&self) -> &Url {
        &self.origin
    }

    pub fn community_id(&self) -> &str {
        &self.community_id
    }
}

#[derive(Debug)]
pub struct Profiles {
    owner: u64,
    generation: u64,
    selected: Option<SelectedProfile>,
}

impl Profiles {
    pub fn new() -> Result<Self, ProfileError> {
        let owner = NEXT_OWNER
            .try_update(Ordering::Relaxed, Ordering::Relaxed, |next| {
                next.checked_add(1)
            })
            .map_err(|_| ProfileError::Exhausted)?;
        Ok(Self {
            owner,
            generation: 0,
            selected: None,
        })
    }

    /// Called only after native discovery and explicit target confirmation.
    /// Inputs are revalidated; failure still invalidates the previous target.
    pub fn select(
        &mut self,
        address: &str,
        community_id: &str,
    ) -> Result<ProfileHandle, ProfileError> {
        self.invalidate()?;
        let origin = normalize_address(address).map_err(|_| ProfileError::InvalidAddress)?;
        if community_id.is_empty()
            || community_id.len() > MAX_COMMUNITY_ID
            || !community_id
                .bytes()
                .all(|byte| byte.is_ascii_alphanumeric() || b"-_".contains(&byte))
        {
            return Err(ProfileError::InvalidCommunity);
        }
        self.selected = Some(SelectedProfile {
            origin,
            community_id: community_id.to_owned(),
        });
        Ok(ProfileHandle {
            owner: self.owner,
            generation: self.generation,
        })
    }

    /// Obtain the selected target exclusively from native state, never from an
    /// operation's caller-supplied URL. Check again before committing results.
    pub fn profile(&self, handle: ProfileHandle) -> Result<&SelectedProfile, ProfileError> {
        if handle.owner != self.owner || handle.generation != self.generation {
            return Err(ProfileError::StaleHandle);
        }
        self.selected.as_ref().ok_or(ProfileError::StaleHandle)
    }

    pub fn begin_operation(&self, profile: ProfileHandle) -> Result<OperationHandle, ProfileError> {
        self.profile(profile)?;
        Ok(OperationHandle { profile })
    }

    /// Re-resolve the current target before dispatching an operation.
    /// The caller must also cancel old transport/capture work on switching;
    /// fencing cannot retract bytes already sent before a selection changed.
    pub fn operation_profile(
        &self,
        operation: OperationHandle,
    ) -> Result<&SelectedProfile, ProfileError> {
        self.profile(operation.profile)
    }

    /// Fence an asynchronous completion before any state/cache/vault write.
    pub fn is_current(&self, operation: OperationHandle) -> bool {
        self.operation_profile(operation).is_ok()
    }

    /// Invalidates all handles even if no target is currently selected.
    pub fn teardown(&mut self) -> Result<(), ProfileError> {
        self.invalidate()
    }

    pub fn generation(&self) -> u64 {
        self.generation
    }

    fn invalidate(&mut self) -> Result<(), ProfileError> {
        self.selected = None;
        self.generation = self
            .generation
            .checked_add(1)
            .ok_or(ProfileError::Exhausted)?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn selection_uses_canonical_https_and_bounded_community() {
        let mut profiles = Profiles::new().unwrap();
        let handle = profiles
            .select("  EXAMPLE.com:443  ", "community-1_A")
            .unwrap();
        let selected = profiles.profile(handle).unwrap();
        assert_eq!(selected.origin().as_str(), "https://example.com/");
        assert_eq!(selected.community_id(), "community-1_A");
        assert_eq!(profiles.generation(), 1);
    }

    #[test]
    fn origin_port_and_idn_are_not_renderer_replacement_targets() {
        let mut profiles = Profiles::new().unwrap();
        let handle = profiles
            .select("https://bücher.example:8443", "shared-id")
            .unwrap();
        let operation = profiles.begin_operation(handle).unwrap();
        assert_eq!(
            profiles
                .operation_profile(operation)
                .unwrap()
                .origin()
                .as_str(),
            "https://xn--bcher-kva.example:8443/"
        );
    }

    #[test]
    fn switching_origins_with_same_community_invalidates_old_handles() {
        let mut profiles = Profiles::new().unwrap();
        let first = profiles.select("https://first.example", "same-id").unwrap();
        let old_operation = profiles.begin_operation(first).unwrap();
        let second = profiles
            .select("https://second.example", "same-id")
            .unwrap();
        assert_eq!(
            profiles.profile(first).unwrap_err(),
            ProfileError::StaleHandle
        );
        assert!(!profiles.is_current(old_operation));
        assert_eq!(
            profiles.operation_profile(old_operation).unwrap_err(),
            ProfileError::StaleHandle
        );
        assert_eq!(
            profiles.profile(second).unwrap().origin().as_str(),
            "https://second.example/"
        );
        assert_eq!(profiles.generation(), 2);
    }

    #[test]
    fn selecting_same_origin_again_also_fences_old_completions() {
        let mut profiles = Profiles::new().unwrap();
        let first = profiles.select("example.com", "one").unwrap();
        let operation = profiles.begin_operation(first).unwrap();
        assert!(profiles.is_current(operation));
        let second = profiles.select("example.com", "two").unwrap();
        assert!(!profiles.is_current(operation));
        assert_eq!(profiles.profile(second).unwrap().community_id(), "two");
        let newer = profiles.begin_operation(second).unwrap();
        assert!(profiles.is_current(newer));
        profiles.select("example.com", "two").unwrap();
        assert!(!profiles.is_current(newer));
    }

    #[test]
    fn separate_brokers_never_accept_each_others_handles() {
        let mut first = Profiles::new().unwrap();
        let mut second = Profiles::new().unwrap();
        let first_handle = first.select("example.com", "same").unwrap();
        let first_operation = first.begin_operation(first_handle).unwrap();
        let second_handle = second.select("example.com", "same").unwrap();
        assert_eq!(first.generation(), second.generation());
        assert_eq!(
            second.profile(first_handle).unwrap_err(),
            ProfileError::StaleHandle
        );
        assert_eq!(
            first.profile(second_handle).unwrap_err(),
            ProfileError::StaleHandle
        );
        assert!(!second.is_current(first_operation));
    }

    #[test]
    fn teardown_and_reselection_do_not_resurrect_operations() {
        let mut profiles = Profiles::new().unwrap();
        let first = profiles.select("example.com", "same").unwrap();
        let operation = profiles.begin_operation(first).unwrap();
        profiles.teardown().unwrap();
        assert_eq!(
            profiles.begin_operation(first).unwrap_err(),
            ProfileError::StaleHandle
        );
        assert!(!profiles.is_current(operation));
        profiles.teardown().unwrap();
        let second = profiles.select("example.com", "same").unwrap();
        assert!(!profiles.is_current(operation));
        assert!(profiles.is_current(profiles.begin_operation(second).unwrap()));
        assert_eq!(profiles.generation(), 4);
    }

    #[test]
    fn invalid_selection_clears_existing_target_without_leaking_input() {
        let addresses = [
            "http://example.com",
            "https://name:secret@example.com",
            "https://example.com/api",
            "https://example.com?token=secret",
            "https://example.com#fragment",
            "https://example.com/../",
            "https://example.com\\other.example",
            "",
            "https://example.com\n",
        ];
        for address in addresses {
            let mut profiles = Profiles::new().unwrap();
            let old = profiles.select("example.com", "approved").unwrap();
            let operation = profiles.begin_operation(old).unwrap();
            assert_eq!(
                profiles.select(address, "other").unwrap_err(),
                ProfileError::InvalidAddress
            );
            assert!(!profiles.is_current(operation));
            assert!(profiles.selected.is_none());
        }
        let mut profiles = Profiles::new().unwrap();
        assert_eq!(
            profiles.select(&"a".repeat(2049), "ok").unwrap_err(),
            ProfileError::InvalidAddress
        );
    }

    #[test]
    fn community_id_is_bounded_ascii_and_never_a_url_or_credential() {
        for community_id in [
            "",
            "a/b",
            "a b",
            "a\n",
            "å",
            "https://example.com",
            "user@host",
        ] {
            let mut profiles = Profiles::new().unwrap();
            let old = profiles.select("example.com", "approved").unwrap();
            assert_eq!(
                profiles.select("example.com", community_id).unwrap_err(),
                ProfileError::InvalidCommunity
            );
            assert_eq!(
                profiles.profile(old).unwrap_err(),
                ProfileError::StaleHandle
            );
        }
        let mut profiles = Profiles::new().unwrap();
        profiles
            .select("example.com", &"a".repeat(MAX_COMMUNITY_ID))
            .unwrap();
        assert_eq!(
            profiles
                .select("example.com", &"a".repeat(MAX_COMMUNITY_ID + 1))
                .unwrap_err(),
            ProfileError::InvalidCommunity
        );
    }

    #[test]
    fn generation_exhaustion_is_permanent_and_fail_closed() {
        let mut profiles = Profiles::new().unwrap();
        let old = profiles.select("example.com", "approved").unwrap();
        let operation = profiles.begin_operation(old).unwrap();
        profiles.generation = u64::MAX;
        assert_eq!(
            profiles.select("other.example", "other").unwrap_err(),
            ProfileError::Exhausted
        );
        assert!(!profiles.is_current(operation));
        assert_eq!(profiles.teardown().unwrap_err(), ProfileError::Exhausted);
        assert!(profiles.selected.is_none());
        assert_eq!(profiles.generation(), u64::MAX);
    }
}
