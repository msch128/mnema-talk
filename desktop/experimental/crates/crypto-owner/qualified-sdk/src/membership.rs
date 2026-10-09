//! Actual MLS removal observations, committed with the provider transition.
//! An archive entry is historical evidence, never current membership authority.
use super::*;
use sha2::{Digest, Sha256};

const DOMAIN: &str = "MnemaTalk NativeDeviceRemoval/v1";
pub(super) fn authenticated_context(
    pin: &NativeBootstrap,
    channel: &str,
    event: &str,
    roster: &[u8],
) -> Result<Vec<u8>> {
    protected::uuid(channel)?;
    protected::uuid(event)?;
    let value = Value::Array(vec![
        Value::Text("MnemaTalk NativeDeviceRemovalContext/v1".into()),
        Value::Text(pin.scope.origin().into()),
        Value::Text(pin.scope.community().into()),
        Value::Bytes(pin.group.clone()),
        Value::Text(channel.into()),
        Value::Text(event.into()),
        Value::Bytes(Sha256::digest(roster).to_vec()),
    ]);
    let mut aad = Vec::new();
    coset::cbor::ser::into_writer(&value, &mut aad).map_err(|_| Error::Invalid)?;
    Ok(aad)
}

/// Encoding only: this function cannot approve a device or authenticate a wire.
pub fn encode_native_device_removal(roster: &[u8], commit: &[u8]) -> Result<Vec<u8>> {
    if roster.is_empty() || commit.is_empty() {
        return Err(Error::Invalid);
    }
    if roster.len() > 65536 || commit.len() > 65536 {
        return Err(Error::Limit);
    }
    let value = Value::Array(vec![
        Value::Text(DOMAIN.into()),
        Value::Bytes(roster.into()),
        Value::Bytes(commit.into()),
    ]);
    let mut wire = Vec::new();
    coset::cbor::ser::into_writer(&value, &mut wire).map_err(|_| Error::Invalid)?;
    if wire.len() > 65536 {
        return Err(Error::Limit);
    }
    Ok(wire)
}
pub(super) fn is_control(wire: &[u8]) -> bool {
    // Only a format discriminator. Authentication happens in the real MLS and
    // pinned-root verifier; this prefix cannot create an observation or grant.
    let mut prefix = vec![0x83, 0x78, DOMAIN.len() as u8];
    prefix.extend_from_slice(DOMAIN.as_bytes());
    wire.starts_with(&prefix)
}
pub(super) fn decode(wire: &[u8]) -> Result<(Vec<u8>, Vec<u8>)> {
    protected::scan_control(wire)?;
    let value: Value = coset::cbor::de::from_reader(wire).map_err(|_| Error::Invalid)?;
    let Value::Array(fields) = value else {
        return Err(Error::Invalid);
    };
    let [
        Value::Text(domain),
        Value::Bytes(roster),
        Value::Bytes(commit),
    ] = fields.as_slice()
    else {
        return Err(Error::Invalid);
    };
    if domain != DOMAIN || encode_native_device_removal(roster, commit)? != wire {
        return Err(Error::Invalid);
    }
    Ok((roster.clone(), commit.clone()))
}
pub(super) fn schema(conn: &Connection) -> Result<()> {
    conn.execute_batch("CREATE TABLE core_device_removals(account TEXT NOT NULL,event TEXT NOT NULL,device TEXT NOT NULL,channel TEXT NOT NULL,epoch BLOB NOT NULL CHECK(length(epoch)=8),generation BLOB NOT NULL CHECK(length(generation)=8),wire BLOB NOT NULL CHECK(length(wire) BETWEEN 1 AND 65536),removed_self INTEGER NOT NULL CHECK(removed_self IN(0,1)),PRIMARY KEY(account,event));").map_err(|_| Error::Database)
}
pub(super) fn record(
    conn: &Connection,
    account: &str,
    device: &str,
    event: &str,
    channel: &str,
    epoch: u64,
    generation: u64,
    wire: &[u8],
    removed_self: bool,
) -> Result<()> {
    id(account)?;
    id(device)?;
    protected::uuid(event)?;
    protected::uuid(channel)?;
    decode(wire)?;
    let (rows, bytes): (i64, i64) = conn
        .query_row(
            "SELECT COUNT(*),COALESCE(SUM(length(wire)),0) FROM core_device_removals",
            [],
            |r| Ok((r.get(0)?, r.get(1)?)),
        )
        .map_err(|_| Error::Database)?;
    if rows >= 1024 || bytes.checked_add(wire.len() as i64).ok_or(Error::Limit)? > 16 * 1024 * 1024
    {
        return Err(Error::Limit);
    }
    require_one(
        conn.execute(
            "INSERT INTO core_device_removals VALUES(?,?,?,?,?,?,?,?)",
            params![
                account,
                event,
                device,
                channel,
                epoch.to_be_bytes().as_slice(),
                generation.to_be_bytes().as_slice(),
                wire,
                removed_self
            ],
        )
        .map_err(|_| Error::Database)?,
    )
}

/// Only the real Core can create this exact, durable authenticated observation.
/// No Clone, Serde, public constructor or conversion to sending authority.
pub struct ArchivedDeviceRemoval {
    owner: u64,
    origin: String,
    community: String,
    group: Vec<u8>,
    account: String,
    event: String,
    device: String,
    channel: String,
    epoch: u64,
    generation: u64,
    removed_self: bool,
}
impl ArchivedDeviceRemoval {
    pub fn account(&self) -> &str {
        &self.account
    }
    pub fn event_id(&self) -> &str {
        &self.event
    }
    pub fn device(&self) -> &str {
        &self.device
    }
    pub fn channel(&self) -> &str {
        &self.channel
    }
    pub fn epoch(&self) -> u64 {
        self.epoch
    }
    pub fn generation(&self) -> u64 {
        self.generation
    }
    pub fn removed_self(&self) -> bool {
        self.removed_self
    }
    pub fn belongs_to_native_owner(&self, scope: &NativeProtectedEventScope) -> bool {
        scope.belongs_to_core(self.owner)
            && scope.origin() == self.origin
            && scope.community() == self.community
            && scope.group() == self.group
            && scope.channel() == self.channel
    }
}
impl Core {
    pub fn archived_device_removal(
        &self,
        account: &str,
        event: &str,
        wire: &[u8],
    ) -> Result<Option<ArchivedDeviceRemoval>> {
        id(account)?;
        protected::uuid(event)?;
        bound_wire(wire)?;
        check_pin(&self.connection, &self.pin)?;
        check_revision(&self.connection, self.revision)?;
        type Row = (String, String, Vec<u8>, Vec<u8>, Vec<u8>, bool);
        let row: Option<Row> = self.connection.query_row("SELECT device,channel,epoch,generation,wire,removed_self FROM core_device_removals WHERE account=? AND event=?", params![account,event], |r| Ok((r.get(0)?,r.get(1)?,r.get(2)?,r.get(3)?,r.get(4)?,r.get(5)?))).optional().map_err(|_| Error::Database)?;
        let Some((device, channel, epoch, generation, actual, removed_self)) = row else {
            return Ok(None);
        };
        if actual != wire {
            return Err(Error::Unauthorized);
        }
        decode(&actual)?;
        Ok(Some(ArchivedDeviceRemoval {
            owner: self.owner,
            origin: self.pin.scope.origin().into(),
            community: self.pin.scope.community().into(),
            group: self.pin.group.clone(),
            account: account.into(),
            event: event.into(),
            device,
            channel,
            epoch: u64::from_be_bytes(epoch.try_into().map_err(|_| Error::Database)?),
            generation: u64::from_be_bytes(generation.try_into().map_err(|_| Error::Database)?),
            removed_self,
        }))
    }
    pub(super) fn receive_device_removal_inner(
        &mut self,
        account: &str,
        event: &str,
        channel: &str,
        wire: &[u8],
        now: u64,
    ) -> Result<()> {
        id(account)?;
        protected::uuid(event)?;
        protected::uuid(channel)?;
        self.mutable()?;
        if self.phase != Phase::Live || self.staged.is_some() {
            return Err(Error::WrongPhase);
        }
        let (roster_wire, commit_wire) = decode(wire)?;
        let group = self.group.as_mut().ok_or(Error::Quarantined)?;
        if group.pending_commit().is_some() {
            return Err(Error::Busy);
        }
        let message = parse_for(group, &commit_wire, ContentType::Commit)?;
        let tx = self
            .connection
            .transaction_with_behavior(TransactionBehavior::Immediate)
            .map_err(|_| Error::Database)?;
        check_revision(&tx, self.revision)?;
        budget(&tx)?;
        let (floor, last) = stored_floor(&tx)?;
        if now < last {
            return Err(Error::Replay);
        }
        let mut trust = verifier(&self.pin, floor, last)?;
        let roster = trust
            .verify_roster(&roster_wire, now)
            .map_err(|_| Error::Trust)?;
        let generation = verified_generation(&roster_wire)?;
        if generation <= floor {
            return Err(Error::Replay);
        }
        let provider = Provider::new(&tx, &self.crypto);
        let processed = group
            .process_message(&provider, message)
            .map_err(|_| Error::Provider)?;
        if processed.aad() != authenticated_context(&self.pin, channel, event, &roster_wire)? {
            return Err(Error::Unauthorized);
        }
        let Sender::Member(sender) = processed.sender() else {
            return Err(Error::Unauthorized);
        };
        let members: Vec<_> = group.members().collect();
        let root = members
            .iter()
            .find(|m| m.index == *sender)
            .ok_or(Error::Unauthorized)?;
        if root.credential.serialized_content() != self.pin.peer_identity
            || root.signature_key != self.pin.peer_key
            || processed.credential().serialized_content() != root.credential.serialized_content()
        {
            return Err(Error::Unauthorized);
        }
        let approved = trust
            .verify_device(
                &roster,
                &self.pin.group,
                root.credential.serialized_content(),
                &root.signature_key,
                now,
            )
            .map_err(|_| Error::Trust)?;
        if approved.account() != account {
            return Err(Error::Unauthorized);
        }
        let root_device = approved.device().to_owned();
        let ProcessedMessageContent::StagedCommitMessage(commit) = processed.into_content() else {
            return Err(Error::Invalid);
        };
        let removals: Vec<_> = commit
            .remove_proposals()
            .map(|p| p.remove_proposal().removed())
            .collect();
        if commit.add_proposals().count() != 0
            || commit.update_proposals().count() != 0
            || removals.len() != 1
            || removals[0] == root.index
        {
            return Err(Error::Unauthorized);
        }
        let target = members
            .iter()
            .find(|m| m.index == removals[0])
            .ok_or(Error::Unauthorized)?;
        if trust
            .verify_device(
                &roster,
                &self.pin.group,
                target.credential.serialized_content(),
                &target.signature_key,
                now,
            )
            .is_ok()
        {
            return Err(Error::Unauthorized);
        }
        authorize(
            &mut trust,
            &roster,
            &self.pin,
            group,
            &commit,
            root.credential.serialized_content(),
            now,
        )?;
        let from_epoch = group.epoch().as_u64();
        let next_epoch = commit.epoch().as_u64();
        if next_epoch != from_epoch.checked_add(1).ok_or(Error::Limit)? {
            return Err(Error::Database);
        }
        let removed_self = target.index == group.own_leaf_index();
        group
            .merge_staged_commit(&provider, *commit)
            .map_err(|_| Error::Provider)?;
        if !removed_self {
            if !group.is_active()
                || group.epoch().as_u64() != next_epoch
                || group.members().count() != members.len() - 1
                || group.members().any(|m| m.index == target.index)
            {
                return Err(Error::Database);
            }
            for member in members.iter().filter(|m| m.index != target.index) {
                if !group.members().any(|m| {
                    m.index == member.index
                        && m.credential.serialized_content()
                            == member.credential.serialized_content()
                        && m.signature_key == member.signature_key
                }) {
                    return Err(Error::Database);
                }
            }
            super::host::verify_merged_host_state(
                &tx,
                &provider,
                group,
                self.signer.as_ref().ok_or(Error::Quarantined)?,
            )?;
        } else {
            let persisted = MlsGroup::load(provider.storage(), group.group_id())
                .map_err(|_| Error::Database)?
                .ok_or(Error::Database)?;
            if group.is_active()
                || persisted.is_active()
                || persisted.epoch() != group.epoch()
                || persisted.own_leaf_index() != group.own_leaf_index()
            {
                return Err(Error::Database);
            }
        }
        require_one(
            tx.execute(
                "UPDATE core_state SET roster=?,generation=?,observed_time=? WHERE id=1",
                params![
                    roster_wire,
                    generation.to_be_bytes().as_slice(),
                    clock(now)?
                ],
            )
            .map_err(|_| Error::Database)?,
        )?;
        current_trust(&tx, &self.pin, now)?;
        record(
            &tx,
            account,
            &root_device,
            event,
            channel,
            next_epoch,
            generation,
            wire,
            removed_self,
        )?;
        let next = advance(&tx, self.revision, now)?;
        #[cfg(test)]
        receiver_crash("before");
        tx.commit().map_err(|_| Error::UnknownCommit)?;
        #[cfg(test)]
        receiver_crash("after");
        self.revision = next;
        if removed_self {
            self.group = None;
            self.signer = None;
            self.phase = Phase::Quarantined;
        }
        Ok(())
    }
}

#[cfg(test)]
fn receiver_crash(point: &str) {
    if std::env::var("MNEMA_CONTROL_RECEIVER_CRASH").as_deref() == Ok(point) {
        std::process::exit(73);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn canonical_control_bounds_and_trailing_indefinite_or_domain_changes_are_rejected() {
        let roster = vec![3; 64000];
        let commit = vec![9; 1000];
        let wire = encode_native_device_removal(&roster, &commit).unwrap();
        assert!(wire.len() <= 65536);
        assert!(is_control(&wire));
        assert_eq!(decode(&wire).unwrap(), (roster, commit));
        let mut trailing = wire.clone();
        trailing.push(0);
        assert!(decode(&trailing).is_err());
        let mut indefinite = wire.clone();
        indefinite[0] = 0x9f;
        assert!(decode(&indefinite).is_err());
        let mut wrong = wire;
        wrong[3] = b'X';
        assert!(!is_control(&wrong));
        assert!(decode(&wrong).is_err());
        assert_eq!(
            encode_native_device_removal(&vec![0; 32768], &vec![0; 32768]),
            Err(Error::Limit)
        );
        assert_eq!(
            encode_native_device_removal(&vec![0; 65536], &[1]),
            Err(Error::Limit)
        );
        assert_eq!(encode_native_device_removal(&[], &[1]), Err(Error::Invalid));
    }
}
