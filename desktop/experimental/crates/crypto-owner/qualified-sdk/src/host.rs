//! Sealed fresh factory adoption; current native auth/OOB ceremony remains
//! caller-owned. No arbitrary restored group, bool or server root input.
use super::*;
use mnema_private_first_community_bootstrap::FreshGroupTransfer;

pub struct NativeOwnerFacts {
    pub(super) origin: String,
    pub(super) community: String,
    pub(super) channel: String,
    pub(super) group: Vec<u8>,
    pub(super) account: String,
    pub(super) device: String,
    pub(super) identity: Vec<u8>,
    pub(super) signature_key: Vec<u8>,
    pub(super) epoch: u64,
    pub(super) generation: u64,
}
impl NativeOwnerFacts {
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
    pub fn account(&self) -> &str {
        &self.account
    }
    pub fn device(&self) -> &str {
        &self.device
    }
    pub fn identity(&self) -> &[u8] {
        &self.identity
    }
    pub fn signature_key(&self) -> &[u8] {
        &self.signature_key
    }
    pub fn epoch(&self) -> u64 {
        self.epoch
    }
    pub fn generation(&self) -> u64 {
        self.generation
    }
}
impl Core {
    /// Consumes the original newly created, known-committed factory group once.
    /// Native caller must have actual current auth/admin/channel scope and
    /// native-modal root approval. No constructor boolean is accepted here.
    pub fn adopt_fresh_native_host(
        transfer: FreshGroupTransfer,
        channel: &str,
        now: u64,
    ) -> Result<Self> {
        protected::uuid(channel)?;
        clock(now)?;
        let parts = transfer.consume_for_native_provider();
        let mut connection = parts.connection;
        let crypto = parts.crypto;
        let group = parts.group;
        let signer = parts.signer;
        let public = parts.public;
        let scope = Scope::new(&public.origin, &public.community).map_err(|_| Error::Trust)?;
        let pin = NativeBootstrap::from_native_pin(
            scope,
            &public.group,
            public.authority,
            &public.identity,
            public.device_key,
        )?;
        if group.group_id().as_slice() != public.group
            || group.epoch().as_u64() != 0
            || group.members().count() != 1
            || signer.to_public_vec() != public.device_key
        {
            return Err(Error::Trust);
        }
        let own = group.own_leaf().ok_or(Error::Trust)?;
        if own.credential().serialized_content() != public.identity
            || own.signature_key().as_slice() != public.device_key
        {
            return Err(Error::Trust);
        }
        let mut trust = verifier(&pin, 0, now)?;
        let roster = trust
            .verify_roster(&public.signed_roster, now)
            .map_err(|_| Error::Trust)?;
        let approved = trust
            .verify_device(
                &roster,
                &pin.group,
                &public.identity,
                &public.device_key,
                now,
            )
            .map_err(|_| Error::Trust)?;
        if approved.account() != public.account
            || approved.device() != public.device
            || verified_generation(&public.signed_roster)? != 1
        {
            return Err(Error::Trust);
        }
        let tx = connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        let current:bool=tx.query_row("SELECT COUNT(*)=1 FROM issuer_state WHERE id=1 AND generation=1 AND observed_time<=? AND origin=? AND community=? AND group_id=? AND authority=?",params![clock(now)?,pin.scope.origin(),pin.scope.community(),pin.group,pin.authority.as_slice()],|r|r.get(0)).map_err(|_|Error::Database)?;
        let durable:bool=tx.query_row("SELECT COUNT(*)=1 FROM native_first_group p JOIN native_bootstrap_outbox o ON p.id=o.id WHERE p.id=1 AND p.origin=? AND p.community=? AND p.group_id=? AND p.authority=? AND p.account=? AND p.device=? AND p.identity=? AND p.device_key=? AND o.signed_roster=?",params![pin.scope.origin(),pin.scope.community(),pin.group,pin.authority.as_slice(),public.account,public.device,public.identity,public.device_key.as_slice(),public.signed_roster],|r|r.get(0)).map_err(|_|Error::Database)?;
        if !current || !durable {
            return Err(Error::Trust);
        }
        require_one(tx.execute("UPDATE issuer_state SET observed_time=? WHERE id=1 AND generation=1 AND observed_time<=?",params![clock(now)?,clock(now)?]).map_err(|_|Error::Database)?)?;
        schema(&tx, &pin)?;
        tx.execute_batch("CREATE TABLE sdk_sources(epoch BLOB NOT NULL CHECK(length(epoch)=8),sender INTEGER NOT NULL,context INTEGER NOT NULL CHECK(context BETWEEN 1 AND 65535),source TEXT NOT NULL,PRIMARY KEY(epoch,sender,context));CREATE TABLE sdk_events(event TEXT PRIMARY KEY,channel TEXT NOT NULL,epoch BLOB NOT NULL CHECK(length(epoch)=8),generation BLOB NOT NULL CHECK(length(generation)=8));CREATE TABLE native_host_channel(id INTEGER PRIMARY KEY CHECK(id=1),channel TEXT NOT NULL);").map_err(|_|Error::Database)?;
        #[cfg(test)]
        fixture_fault(&tx, "adopt")?;
        require_one(
            tx.execute("INSERT INTO native_host_channel VALUES(1,?)", [channel])
                .map_err(|_| Error::Database)?,
        )?;
        require_one(tx.execute("UPDATE core_state SET generation=?,observed_time=?,roster=?,fresh_join_epoch=? WHERE id=1",params![1u64.to_be_bytes().as_slice(),clock(now)?,public.signed_roster,0u64.to_be_bytes().as_slice()]).map_err(|_|Error::Database)?)?;
        #[cfg(test)]
        fixture_exit("adopt-before");
        tx.commit().map_err(|_| Error::UnknownCommit)?;
        #[cfg(test)]
        fixture_exit("adopt-after");
        Ok(Self {
            connection,
            crypto,
            pin,
            group: Some(group),
            signer: Some(signer),
            challenge: None,
            phase: Phase::Live,
            revision: 0,
            owner: owner()?,
            staged: None,
        })
    }
}
impl Sdk {
    pub fn from_fresh_native_host(
        transfer: FreshGroupTransfer,
        binding: NativeBinding,
        now: u64,
    ) -> Result<Self> {
        let channel = binding.channel_for_native_host().to_owned();
        Self::bind_native(
            Core::adopt_fresh_native_host(transfer, &channel, now)?,
            binding,
            now,
        )
    }
    /// Native metadata from actual current MLS ownleaf and verified complete
    /// signed roster, never a server label or renderer request. Not an auth grant.
    pub fn native_owner_facts(&self, now: u64) -> Result<NativeOwnerFacts> {
        self.owner_facts_inner(now)
    }
}

/// Exact durable MLS publication bytes; inspection only. Publication must use
/// the SDK current-epoch/native-auth guarded pending outbox accessor.
pub struct HostAdmission {
    pub commit: Vec<u8>,
    pub welcome: Vec<u8>,
}
impl Core {
    pub(super) fn add_root_approved_native_peer(
        &mut self,
        commit_event: &str,
        welcome_event: &str,
        key_package: &[u8],
        now: u64,
    ) -> Result<HostAdmission> {
        self.mutable()?;
        if self.phase != Phase::Live {
            return Err(Error::WrongPhase);
        }
        let result = self.add_root_peer_inner(commit_event, welcome_event, key_package, now);
        self.quarantine(result)
    }
    fn add_root_peer_inner(
        &mut self,
        commit_event: &str,
        welcome_event: &str,
        key_package: &[u8],
        now: u64,
    ) -> Result<HostAdmission> {
        protected::uuid(commit_event)?;
        protected::uuid(welcome_event)?;
        if commit_event == welcome_event || key_package.is_empty() || key_package.len() > 32768 {
            return Err(Error::Invalid);
        }
        let group = self.group.as_mut().ok_or(Error::Quarantined)?;
        if group.pending_commit().is_some() || self.staged.is_some() {
            return Err(Error::Busy);
        }
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        budget(&tx)?;
        let channel: String = tx
            .query_row(
                "SELECT channel FROM native_host_channel WHERE id=1",
                [],
                |r| r.get(0),
            )
            .map_err(|_| Error::Unauthorized)?;
        let exists: i64 = tx
            .query_row(
                "SELECT COUNT(*) FROM core_outbox WHERE event_id IN (?,?)",
                params![commit_event, welcome_event],
                |r| r.get(0),
            )
            .map_err(|_| Error::Database)?;
        if exists != 0 {
            return Err(Error::Replay);
        }
        let (mut trust, roster, generation) = current_trust(&tx, &self.pin, now)?;
        #[cfg(test)]
        fixture_fault(&tx, "add")?;
        let provider = Provider::new(&tx, &self.crypto);
        let kp = KeyPackageIn::tls_deserialize_exact(key_package)
            .map_err(|_| Error::Invalid)?
            .validate(provider.crypto(), ProtocolVersion::Mls10)
            .map_err(|_| Error::Invalid)?;
        let leaf = kp.leaf_node();
        approve(
            &mut trust,
            &roster,
            &self.pin,
            leaf.credential().serialized_content(),
            leaf.signature_key().as_slice(),
            now,
        )?;
        for member in group.members() {
            approve(
                &mut trust,
                &roster,
                &self.pin,
                member.credential.serialized_content(),
                &member.signature_key,
                now,
            )?;
            if member.signature_key == leaf.signature_key().as_slice() {
                return Err(Error::Replay);
            }
        }
        let signer = self.signer.as_ref().ok_or(Error::Quarantined)?;
        let (commit, welcome, _) = group
            .add_members(&provider, signer, &[kp])
            .map_err(|_| Error::Provider)?;
        let commit = commit.to_bytes().map_err(|_| Error::Provider)?;
        let welcome = welcome.to_bytes().map_err(|_| Error::Provider)?;
        bound_wire(&commit)?;
        bound_wire(&welcome)?;
        group
            .merge_pending_commit(&provider)
            .map_err(|_| Error::Provider)?;
        verify_merged_host_state(&tx, &provider, group, signer)?;
        let next = advance(&tx, self.revision, now)?;
        for (event, kind, wire) in [
            (commit_event, "host_commit", &commit),
            (welcome_event, "host_welcome", &welcome),
        ] {
            require_one(
                tx.execute(
                    "INSERT INTO core_outbox VALUES(?,?,?,?)",
                    params![event, next, kind, wire],
                )
                .map_err(|_| Error::Database)?,
            )?;
            require_one(
                tx.execute(
                    "INSERT INTO sdk_events VALUES(?,?,?,?)",
                    params![
                        event,
                        channel,
                        group.epoch().as_u64().to_be_bytes().as_slice(),
                        generation.to_be_bytes().as_slice()
                    ],
                )
                .map_err(|_| Error::Database)?,
            )?;
        }
        #[cfg(test)]
        fixture_exit("add-before");
        tx.commit().map_err(|_| Error::UnknownCommit)?;
        #[cfg(test)]
        fixture_exit("add-after");
        self.revision = next;
        Ok(HostAdmission { commit, welcome })
    }
}

#[cfg(test)]
fn fixture_fault(tx: &Connection, point: &str) -> Result<()> {
    let Ok(f) = std::env::var("MNEMA_HOST_FAULT") else {
        return Ok(());
    };
    let sql = match (point, f.as_str()) {
        ("adopt", "adopt-ignore-channel") => {
            "CREATE TEMP TRIGGER fault_host BEFORE INSERT ON native_host_channel BEGIN SELECT RAISE(IGNORE);END;"
        }
        ("adopt", "adopt-ignore-state") => {
            "CREATE TEMP TRIGGER fault_host BEFORE UPDATE OF roster ON core_state BEGIN SELECT RAISE(IGNORE);END;"
        }
        ("add", "add-ignore-outbox") | ("remove", "remove-ignore-outbox") => {
            "CREATE TEMP TRIGGER fault_host BEFORE INSERT ON core_outbox BEGIN SELECT RAISE(IGNORE);END;"
        }
        ("add", "add-ignore-context") | ("remove", "remove-ignore-context") => {
            "CREATE TEMP TRIGGER fault_host BEFORE INSERT ON openmls_group_data WHEN NEW.data_type='context' BEGIN SELECT RAISE(IGNORE);END;"
        }
        ("add", "add-ignore-private-epoch") | ("remove", "remove-ignore-private-epoch") => {
            "CREATE TEMP TRIGGER fault_host BEFORE INSERT ON openmls_epoch_keys_pairs BEGIN SELECT RAISE(IGNORE);END;"
        }
        ("add", "add-ignore-resumption") | ("remove", "remove-ignore-resumption") => {
            "CREATE TEMP TRIGGER fault_host BEFORE INSERT ON openmls_group_data WHEN NEW.data_type='resumption_psk_store' BEGIN SELECT RAISE(IGNORE);END;"
        }
        ("add", "add-ignore-message-secrets") | ("remove", "remove-ignore-message-secrets") => {
            "CREATE TEMP TRIGGER fault_host BEFORE INSERT ON openmls_group_data WHEN NEW.data_type='message_secrets' BEGIN SELECT RAISE(IGNORE);END;"
        }
        ("remove", "remove-ignore-state") => {
            "CREATE TEMP TRIGGER fault_host BEFORE UPDATE OF roster ON core_state BEGIN SELECT RAISE(IGNORE); END;"
        }
        ("remove", "remove-ignore-events") => {
            "CREATE TEMP TRIGGER fault_host BEFORE INSERT ON sdk_events BEGIN SELECT RAISE(IGNORE); END;"
        }
        ("remove", "remove-ignore-control") => {
            "CREATE TEMP TRIGGER fault_host BEFORE INSERT ON core_device_removals BEGIN SELECT RAISE(IGNORE); END;"
        }
        _ => return Ok(()),
    };
    tx.execute_batch(sql).map_err(|_| Error::Database)
}
#[cfg(test)]
fn fixture_exit(point: &str) {
    if std::env::var("MNEMA_HOST_FAULT").as_deref() == Ok(point) {
        std::process::exit(73)
    }
}

/// Check the merged MLS state through the ordinary persisted provider before
/// publishing either admission or removal bytes. No resumed sender is created.
pub(super) fn verify_merged_host_state(
    tx: &Connection,
    provider: &Provider<'_>,
    group: &MlsGroup,
    signer: &SignatureKeyPair,
) -> Result<()> {
    let restored = MlsGroup::load(provider.storage(), group.group_id())
        .map_err(|_| Error::Database)?
        .ok_or(Error::Database)?;
    if restored.epoch() != group.epoch()
        || restored.own_leaf_index() != group.own_leaf_index()
        || restored.members().count() != group.members().count()
        || !restored.is_active()
    {
        return Err(Error::Database);
    }
    let expected_info = group
        .export_group_info(provider.crypto(), signer, true)
        .map_err(|_| Error::Database)?
        .to_bytes()
        .map_err(|_| Error::Database)?;
    let actual_info = restored
        .export_group_info(provider.crypto(), signer, true)
        .map_err(|_| Error::Database)?
        .to_bytes()
        .map_err(|_| Error::Database)?;
    if expected_info != actual_info {
        return Err(Error::Database);
    }
    let expected = Zeroizing::new(
        group
            .export_secret(
                provider.crypto(),
                "MnemaTalk host epoch persistence probe",
                b"",
                32,
            )
            .map_err(|_| Error::Database)?,
    );
    let actual = Zeroizing::new(
        restored
            .export_secret(
                provider.crypto(),
                "MnemaTalk host epoch persistence probe",
                b"",
                32,
            )
            .map_err(|_| Error::Database)?,
    );
    if expected.as_slice() != actual.as_slice() {
        return Err(Error::Database);
    }
    let group_id = serde_json::to_vec(group.group_id()).map_err(|_| Error::Database)?;
    let epoch = serde_json::to_vec(&group.epoch()).map_err(|_| Error::Database)?;
    let private_epoch:i64=tx.query_row("SELECT COUNT(*) FROM openmls_epoch_keys_pairs WHERE group_id=? AND epoch_id=? AND leaf_index=? AND provider_version=1 AND length(key_pairs) BETWEEN 1 AND 131072",params![group_id,epoch,group.own_leaf_index().u32()],|r|r.get(0)).map_err(|_|Error::Database)?;
    if private_epoch != 1 {
        return Err(Error::Database);
    }
    Ok(())
}

impl Core {
    pub(super) fn remove_root_revoked_native_peer(
        &mut self,
        event: &str,
        identity: &[u8],
        signature_key: &[u8],
        signed_roster: &[u8],
        now: u64,
    ) -> Result<Vec<u8>> {
        self.mutable()?;
        if self.phase != Phase::Live {
            return Err(Error::WrongPhase);
        }
        let result =
            self.remove_root_peer_inner(event, identity, signature_key, signed_roster, now);
        self.quarantine(result)
    }
    fn remove_root_peer_inner(
        &mut self,
        event: &str,
        identity: &[u8],
        signature_key: &[u8],
        signed_roster: &[u8],
        now: u64,
    ) -> Result<Vec<u8>> {
        protected::uuid(event)?;
        if identity.is_empty() || identity.len() > 256 || signature_key.len() != 32 {
            return Err(Error::Invalid);
        }
        let group = self.group.as_mut().ok_or(Error::Quarantined)?;
        if group.pending_commit().is_some() || self.staged.is_some() {
            return Err(Error::Busy);
        }
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        budget(&tx)?;
        let channel: String = tx
            .query_row(
                "SELECT channel FROM native_host_channel WHERE id=1",
                [],
                |r| r.get(0),
            )
            .map_err(|_| Error::Unauthorized)?;
        let exists: bool = tx
            .query_row(
                "SELECT EXISTS(SELECT 1 FROM core_outbox WHERE event_id=?)",
                [event],
                |r| r.get(0),
            )
            .map_err(|_| Error::Database)?;
        if exists {
            return Err(Error::Replay);
        }
        // The durable local issuer may already have advanced for this exact
        // withdrawal. Reverify the old Core roster only to identify the old
        // MLS members; it does not authorize sending or control publication.
        let (old_generation, last) = stored_floor(&tx)?;
        if old_generation == 0 || now < last {
            return Err(Error::Trust);
        }
        let old_wire: Vec<u8> = tx
            .query_row("SELECT roster FROM core_state WHERE id=1", [], |r| r.get(0))
            .map_err(|_| Error::Database)?;
        let mut old_trust = verifier(&self.pin, old_generation - 1, last)?;
        let old_roster = old_trust
            .verify_roster(&old_wire, last)
            .map_err(|_| Error::Trust)?;
        if verified_generation(&old_wire)? != old_generation {
            return Err(Error::Trust);
        }
        // Only an exact, previously approved MLS leaf can be withdrawn. No leaf
        // index or account label supplied by the renderer selects the target.
        let members: Vec<_> = group.members().collect();
        let target = members
            .iter()
            .find(|m| {
                m.credential.serialized_content() == identity && m.signature_key == signature_key
            })
            .ok_or(Error::Unauthorized)?;
        if target.index == group.own_leaf_index() {
            return Err(Error::Unauthorized);
        }
        for member in &members {
            approve(
                &mut old_trust,
                &old_roster,
                &self.pin,
                member.credential.serialized_content(),
                &member.signature_key,
                last,
            )?;
        }
        // New signed root evidence and removal are committed together. Never
        // install a revoked roster and continue with the old sending epoch.
        let mut new_trust = verifier(&self.pin, old_generation, now)?;
        let new_roster = new_trust
            .verify_roster(signed_roster, now)
            .map_err(|_| Error::Trust)?;
        let generation = verified_generation(signed_roster)?;
        if generation <= old_generation {
            return Err(Error::Replay);
        }
        let issued: i64 = tx
            .query_row(
                "SELECT COUNT(*) FROM roster_outbox WHERE generation=? AND wire=?",
                params![
                    i64::try_from(generation).map_err(|_| Error::Trust)?,
                    signed_roster
                ],
                |r| r.get(0),
            )
            .map_err(|_| Error::Database)?;
        let withdrawn: bool = tx.query_row(
            "SELECT NOT EXISTS(SELECT 1 FROM members WHERE identity=? OR key=?) AND EXISTS(SELECT 1 FROM issuer_state WHERE id=1 AND observed_time<=?)",
            params![identity, signature_key, clock(now)?], |r| r.get(0)
        ).map_err(|_| Error::Database)?;
        if issued != 1 || !withdrawn {
            return Err(Error::Unauthorized);
        }
        if new_trust
            .verify_device(&new_roster, &self.pin.group, identity, signature_key, now)
            .is_ok()
        {
            return Err(Error::Unauthorized);
        }
        for member in &members {
            if member.index != target.index {
                approve(
                    &mut new_trust,
                    &new_roster,
                    &self.pin,
                    member.credential.serialized_content(),
                    &member.signature_key,
                    now,
                )?;
            }
        }
        #[cfg(test)]
        fixture_fault(&tx, "remove")?;
        require_one(
            tx.execute(
                "UPDATE core_state SET roster=?,generation=?,observed_time=? WHERE id=1",
                params![
                    signed_roster,
                    generation.to_be_bytes().as_slice(),
                    clock(now)?
                ],
            )
            .map_err(|_| Error::Database)?,
        )?;
        // Ordinary current trust now also checks the actual local issuer ledger.
        // This tentative update rolls back with the MLS merge on every failure.
        current_trust(&tx, &self.pin, now)?;
        let provider = Provider::new(&tx, &self.crypto);
        let signer = self.signer.as_ref().ok_or(Error::Quarantined)?;
        let old_epoch = group.epoch().as_u64();
        group.set_aad(super::membership::authenticated_context(
            &self.pin,
            &channel,
            event,
            signed_roster,
        )?);
        let (commit, _, _) = group
            .remove_members(&provider, signer, &[target.index])
            .map_err(|_| Error::Provider)?;
        let wire = commit.to_bytes().map_err(|_| Error::Provider)?;
        bound_wire(&wire)?;
        let control = super::membership::encode_native_device_removal(signed_roster, &wire)?;
        group
            .merge_pending_commit(&provider)
            .map_err(|_| Error::Provider)?;
        if group.epoch().as_u64() != old_epoch.checked_add(1).ok_or(Error::Limit)?
            || group.members().count() != members.len() - 1
            || group.members().any(|m| {
                m.credential.serialized_content() == identity || m.signature_key == signature_key
            })
        {
            return Err(Error::Database);
        }
        for member in &members {
            if member.index != target.index
                && !group.members().any(|remaining| {
                    remaining.index == member.index
                        && remaining.credential.serialized_content()
                            == member.credential.serialized_content()
                        && remaining.signature_key == member.signature_key
                })
            {
                return Err(Error::Database);
            }
        }
        verify_merged_host_state(&tx, &provider, group, signer)?;
        let own = group.own_leaf().ok_or(Error::Trust)?;
        let approved = new_trust
            .verify_device(
                &new_roster,
                &self.pin.group,
                own.credential().serialized_content(),
                own.signature_key().as_slice(),
                now,
            )
            .map_err(|_| Error::Trust)?;
        super::membership::record(
            &tx,
            approved.account(),
            approved.device(),
            event,
            &channel,
            group.epoch().as_u64(),
            generation,
            &control,
            false,
        )?;
        let next = advance(&tx, self.revision, now)?;
        require_one(
            tx.execute(
                "INSERT INTO core_outbox VALUES(?,?,?,?)",
                params![event, next, "host_commit", wire],
            )
            .map_err(|_| Error::Database)?,
        )?;
        require_one(
            tx.execute(
                "INSERT INTO sdk_events VALUES(?,?,?,?)",
                params![
                    event,
                    channel,
                    group.epoch().as_u64().to_be_bytes().as_slice(),
                    generation.to_be_bytes().as_slice()
                ],
            )
            .map_err(|_| Error::Database)?,
        )?;
        #[cfg(test)]
        fixture_exit("remove-before");
        tx.commit().map_err(|_| Error::UnknownCommit)?;
        #[cfg(test)]
        fixture_exit("remove-after");
        self.revision = next;
        Ok(wire)
    }
}

/// Actual current MLS leaf and root-verified identity for native device UI.
/// This is display/selection evidence, not a removal or sending permission.
/// No public constructor, Clone or renderer serialization.
pub struct NativePeerFacts {
    owner: u64,
    epoch: u64,
    generation: u64,
    account: String,
    device: String,
    identity: Vec<u8>,
    signature_key: Vec<u8>,
    index: LeafNodeIndex,
}
impl NativePeerFacts {
    pub fn account(&self) -> &str {
        &self.account
    }
    pub fn device(&self) -> &str {
        &self.device
    }
    pub fn identity(&self) -> &[u8] {
        &self.identity
    }
    pub fn signature_key(&self) -> &[u8] {
        &self.signature_key
    }
}
impl Sdk {
    /// Select by actual signed account/device and actual MLS membership. Labels
    /// or public keys supplied by a renderer never populate the returned facts.
    pub fn native_peer_for_removal(
        &self,
        account: &str,
        device: &str,
        now: u64,
    ) -> Result<NativePeerFacts> {
        id(account)?;
        id(device)?;
        let (epoch, generation, _, _, _) = self.current(now)?;
        let (mut trust, roster, _) = current_trust(&self.core.connection, &self.core.pin, now)?;
        let group = self.core.group.as_ref().ok_or(Error::WrongPhase)?;
        for member in group.members() {
            let approved = trust
                .verify_device(
                    &roster,
                    &self.core.pin.group,
                    member.credential.serialized_content(),
                    &member.signature_key,
                    now,
                )
                .map_err(|_| Error::Unauthorized)?;
            if approved.account() == account && approved.device() == device {
                if member.index == group.own_leaf_index() {
                    return Err(Error::Unauthorized);
                }
                return Ok(NativePeerFacts {
                    owner: self.core.owner,
                    epoch,
                    generation,
                    account: approved.account().into(),
                    device: approved.device().into(),
                    identity: member.credential.serialized_content().into(),
                    signature_key: member.signature_key,
                    index: member.index,
                });
            }
        }
        Err(Error::Unauthorized)
    }
    /// Recheck a native selection after the OS dialog, before issuer mutation.
    pub fn check_native_peer_for_removal(&self, facts: &NativePeerFacts, now: u64) -> Result<()> {
        let (epoch, generation, _, _, _) = self.current(now)?;
        if facts.owner != self.core.owner || facts.epoch != epoch || facts.generation != generation
        {
            return Err(Error::Stale);
        }
        let current = self.native_peer_for_removal(&facts.account, &facts.device, now)?;
        if facts.identity != current.identity
            || facts.signature_key != current.signature_key
            || facts.index != current.index
        {
            return Err(Error::Stale);
        }
        Ok(())
    }
}
