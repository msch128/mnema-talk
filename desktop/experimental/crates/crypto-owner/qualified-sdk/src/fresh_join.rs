//! Necessary native-only fixed fresh-join reply; never an arbitrary plaintext
//! renderer send surface. Original Core challenge/confirm algorithm is unchanged.
use super::*;

impl Sdk {
    /// Answer only a canonical existing Core FreshJoin transcript for a current
    /// root-approved, non-own member of this exact fresh host group/epoch.
    /// Native peer/OOB workflow supplies recipient identity/key, never renderer.
    pub fn answer_native_fresh_join(
        &mut self,
        event: &str,
        peer_identity: &[u8],
        peer_key: &[u8],
        transcript: &[u8],
        now: u64,
    ) -> Result<Vec<u8>> {
        super::uuid(event)?;
        if peer_identity.is_empty()
            || peer_identity.len() > 256
            || peer_key.len() != 32
            || transcript.is_empty()
            || transcript.len() > 4096
        {
            return Err(Error::Invalid);
        }
        super::scan(transcript)?;
        let value: Value = coset::cbor::de::from_reader(transcript).map_err(|_| Error::Invalid)?;
        let Value::Array(fields) = &value else {
            return Err(Error::Invalid);
        };
        if fields.len() != 7 {
            return Err(Error::Invalid);
        }
        let mut canonical = Vec::new();
        coset::cbor::ser::into_writer(&value, &mut canonical).map_err(|_| Error::Invalid)?;
        if canonical != transcript {
            return Err(Error::Invalid);
        }
        let (epoch, generation, _, _, _) = self.current(now)?;
        // Only the sealed fresh-root host owns this local issuer/channel binding.
        let is_host:bool=self.core.connection.query_row("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='native_host_channel')",[],|r|r.get(0)).map_err(|_|Error::Database)?;
        if !is_host {
            return Err(Error::Unauthorized);
        }
        let group = self.core.group.as_ref().ok_or(Error::Quarantined)?;
        let own = group.own_leaf_index();
        if !group.members().any(|m| {
            m.index != own
                && m.credential.serialized_content() == peer_identity
                && m.signature_key == peer_key
        }) {
            return Err(Error::Unauthorized);
        }
        let (mut trust, roster, _) = current_trust(&self.core.connection, &self.core.pin, now)?;
        approve(
            &mut trust,
            &roster,
            &self.core.pin,
            peer_identity,
            peer_key,
            now,
        )?;
        let expected = [
            Value::Text("MnemaTalk FreshJoin/v1".into()),
            Value::Text(self.core.pin.scope.origin().into()),
            Value::Text(self.core.pin.scope.community().into()),
            Value::Bytes(self.core.pin.group.clone()),
            Value::Integer(epoch.into()),
            Value::Bytes(group.epoch_authenticator().as_slice().into()),
        ];
        if fields[..6] != expected {
            return Err(Error::Unauthorized);
        }
        if !matches!(&fields[6],Value::Bytes(nonce) if nonce.len()==32) {
            return Err(Error::Invalid);
        }
        let channel = self.binding.channel.clone();
        let result = self.core.send_inner_kind(
            event,
            transcript,
            now,
            |_| Ok(()),
            Some((&channel, epoch, generation)),
            "peer_confirmation",
        );
        let result = self.core.quarantine(result);
        if result.is_err() {
            self.retire_native()
        }
        result
    }
    /// Explicit native retry reads identical committed proof. It never encrypts
    /// again and cannot be mistaken for application chat outbox bytes.
    pub fn pending_native_fresh_join(&self, event: &str, now: u64) -> Result<Option<Vec<u8>>> {
        super::uuid(event)?;
        let (epoch, generation, _, _, _) = self.current(now)?;
        self.core.connection.query_row("SELECT o.wire FROM core_outbox o JOIN sdk_events e ON e.event=o.event_id WHERE o.event_id=? AND o.kind='peer_confirmation' AND e.channel=? AND e.epoch=? AND e.generation=?",params![event,self.binding.channel_for_native_host(),epoch.to_be_bytes().as_slice(),generation.to_be_bytes().as_slice()],|r|r.get(0)).optional().map_err(|_|Error::Database)
    }
}
