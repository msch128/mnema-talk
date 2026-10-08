//! Exact native SDK context and own reserved typed-outbox projections.
//! These opaque facts are NOT bearer/Voice/role grants. Native caller must also
//! validate actual broker/window lineage before HTTP and through final enqueue.
//! The operation snapshot is RAM-only: no restart/durable-history claim.
use super::{ChatEventClaim, Error, Result, Sdk, VerifiedChatEvent};
use zeroize::Zeroizing;

/// Constructed from actual current live SDK only, not caller context labels.
/// No Clone/Serde/Debug or public constructor; group is an ID, never MLS keys.
pub struct NativeProtectedEventScope {
    core_owner: u64,
    origin: String,
    community: String,
    channel: String,
    group: Vec<u8>,
    profile: String,
    window: String,
    session: String,
    epoch: u64,
    root_generation: u64,
}
impl NativeProtectedEventScope {
    pub fn origin(&self) -> &str {
        &self.origin
    }
    pub fn community(&self) -> &str {
        &self.community
    }
    pub fn channel(&self) -> &str {
        &self.channel
    }
    pub fn group(&self) -> &[u8] {
        &self.group
    }
    pub fn profile(&self) -> &str {
        &self.profile
    }
    pub fn window(&self) -> &str {
        &self.window
    }
    pub fn session(&self) -> &str {
        &self.session
    }
    pub fn epoch(&self) -> u64 {
        self.epoch
    }
    pub fn root_generation(&self) -> u64 {
        self.root_generation
    }
    /// A context comparator only; no caller gets authority from equality.
    /// History identity may survive a qualified same-owner epoch update, but
    /// every new event still requires exact current epoch/root-generation.
    pub fn same_native_context(&self, other: &Self) -> bool {
        self.core_owner == other.core_owner
            && self.origin == other.origin
            && self.community == other.community
            && self.channel == other.channel
            && self.group == other.group
            && self.profile == other.profile
            && self.window == other.window
            && self.session == other.session
    }
    pub fn matches_exact(&self, other: &Self) -> bool {
        self.same_native_context(other)
            && self.epoch == other.epoch
            && self.root_generation == other.root_generation
    }
}

/// Exact actual reservation. A retry retains THIS object and ciphertext; it
/// never re-reserves/encrypts the event. This is not deserializable or clonable.
pub struct NativeReservedChatEvent {
    verified: VerifiedChatEvent,
    wire: Zeroizing<Vec<u8>>,
}
impl NativeReservedChatEvent {
    pub fn event_id(&self) -> &str {
        self.verified.event_id()
    }
    /// Native-only byte view; HTTP admission additionally needs actual native
    /// broker scope AND pending_native_chat_event_projection immediately before
    /// send/final publication. This byte getter does not authorize transmission.
    pub fn wire_for_native_relay(&self) -> &[u8] {
        self.wire.as_slice()
    }
}

impl Sdk {
    /// Native cryptographic owner facts, not renderer/broker auth authority.
    pub fn native_protected_event_scope(&self, now: u64) -> Result<NativeProtectedEventScope> {
        let (epoch, root_generation, _, _, _) = self.current(now)?;
        Ok(NativeProtectedEventScope {
            core_owner: self.core.owner,
            origin: self.core.pin.scope.origin().into(),
            community: self.core.pin.scope.community().into(),
            channel: self.binding.channel.clone(),
            group: self.core.pin.group.clone(),
            profile: self.binding.profile.clone(),
            window: self.binding.window.clone(),
            session: self.binding.session.clone(),
            epoch,
            root_generation,
        })
    }
    /// Native owner must admit the producer operation from actual current
    /// history/role BEFORE reservation. This method issues no such permission.
    /// Invalid shape is checked before provider mutation. Calling this again
    /// for an already reserved event is NOT retry: existing Core rejects replay.
    pub fn reserve_native_chat_event(
        &mut self,
        event: &str,
        claim: &ChatEventClaim,
        now: u64,
    ) -> Result<NativeReservedChatEvent> {
        let scope = self.native_protected_event_scope(now)?;
        let (_, _, _, account, device) = self.current(now)?;
        let verified = VerifiedChatEvent::from_authenticated_payload(
            scope,
            event,
            &account,
            &device,
            &claim.payload(),
        )?;
        let wire = self.send_chat_event(event, claim, now)?;
        let pending = match self.pending_chat_for_native_publish(event, now) {
            Ok(pending) => pending,
            Err(error) => {
                // Encryption is already durably reserved. A failed subsequent
                // validation cannot leave this SDK issuing further outputs.
                self.retire_native();
                return Err(error);
            }
        };
        if pending.as_deref() != Some(wire.as_slice()) {
            self.retire_native();
            return Err(Error::Stale);
        }
        let reservation = NativeReservedChatEvent {
            verified,
            wire: Zeroizing::new(wire),
        };
        // Recheck actual current own leaf and exact scope after durable reserve.
        if let Err(error) = self.pending_native_chat_event_projection(&reservation, now) {
            self.retire_native();
            return Err(error);
        }
        Ok(reservation)
    }
    /// Checked own plaintext projection for exact committed outbox bytes.
    /// No decrypt, no encrypt, no nonce/counter reservation, no wire replacement.
    /// Stale/foreign handles never borrow a newer context. Actual broker/window
    /// and final publication fences remain mandatory in enclosing native owner.
    pub fn pending_native_chat_event_projection(
        &self,
        reserved: &NativeReservedChatEvent,
        now: u64,
    ) -> Result<VerifiedChatEvent> {
        let scope = self.native_protected_event_scope(now)?;
        if !reserved.verified.scope().matches_exact(&scope) {
            return Err(Error::Stale);
        }
        let (_, _, _, account, device) = self.current(now)?;
        if reserved.verified.account() != account || reserved.verified.device() != device {
            return Err(Error::Unauthorized);
        }
        let pending = self.pending_chat_for_native_publish(reserved.event_id(), now)?;
        if pending.as_deref() != Some(reserved.wire.as_slice()) {
            return Err(Error::Stale);
        }
        let claim = ChatEventClaim::claim(reserved.verified.operation().clone())?;
        VerifiedChatEvent::from_authenticated_payload(
            scope,
            reserved.event_id(),
            &account,
            &device,
            &claim.payload(),
        )
    }
}

/// PRIVATE unit fixture for shape/comparator tests only; never a real SDK,
/// grant, actual native owner proof or product constructor.
#[cfg(test)]
pub(super) fn fixture_scope() -> NativeProtectedEventScope {
    NativeProtectedEventScope {
        core_owner: 1,
        origin: "https://fixture.invalid".into(),
        community: "fixture-community".into(),
        channel: "11111111-1111-4111-8111-111111111111".into(),
        group: b"fixture-group".to_vec(),
        profile: "fixture-profile".into(),
        window: "fixture-window".into(),
        session: "fixture-session".into(),
        epoch: 7,
        root_generation: 8,
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn every_native_context_field_and_owner_identity_participates_in_comparison() {
        let expected = fixture_scope();
        assert!(expected.matches_exact(&fixture_scope()));
        for field in 0..8 {
            let mut other = fixture_scope();
            match field {
                0 => other.core_owner += 1,
                1 => other.origin.push_str("/different"),
                2 => other.community.push_str("-different"),
                3 => other.channel.push_str("-different"),
                4 => other.group.push(1),
                5 => other.profile.push_str("-different"),
                6 => other.window.push_str("-different"),
                7 => other.session.push_str("-different"),
                _ => unreachable!(),
            }
            assert!(!expected.same_native_context(&other));
            assert!(!expected.matches_exact(&other));
        }
        assert_eq!(expected.origin(), "https://fixture.invalid");
        assert_eq!(expected.community(), "fixture-community");
        assert_eq!(expected.channel(), "11111111-1111-4111-8111-111111111111");
        assert_eq!(expected.group(), b"fixture-group");
        assert_eq!(expected.profile(), "fixture-profile");
        assert_eq!(expected.window(), "fixture-window");
        assert_eq!(expected.session(), "fixture-session");
    }
    #[test]
    fn epoch_or_root_change_never_matches_exact_while_same_owner_context_is_retained() {
        let expected = fixture_scope();
        for (epoch, generation) in [(8, 8), (7, 9), (8, 9)] {
            let mut other = fixture_scope();
            other.epoch = epoch;
            other.root_generation = generation;
            assert!(expected.same_native_context(&other));
            assert!(!expected.matches_exact(&other));
        }
        assert_eq!(expected.epoch(), 7);
        assert_eq!(expected.root_generation(), 8);
    }
}
