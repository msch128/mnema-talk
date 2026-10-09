//! One real MLS receive, followed by authenticated protected-domain dispatch.
//! These facts authenticate a cryptographic source; they do not prove voice
//! membership, purpose/MID/offer attribution, or grant an RTC attachment.
use super::*;

pub enum ProtectedReceived {
    Chat(ChatMessage),
    ChatEvent(Box<VerifiedChatEvent>),
    Source(Box<VerifiedSource>),
}
/// Native-only non-Serde facts obtained from actual MLS sender + signed roster.
/// Missing transport/purpose binding keeps this legacy announcement unattachable.
pub struct VerifiedSourceFacts {
    origin: String,
    community: String,
    channel: String,
    group: Vec<u8>,
    event: String,
    account: String,
    device: String,
    source: String,
    epoch: u64,
    generation: u64,
    sender: u32,
    context: u16,
    codec: SourceKind,
    voice: Option<ProtectedSourceBinding>,
}
impl VerifiedSourceFacts {
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
    pub fn event_id(&self) -> &str {
        &self.event
    }
    pub fn account(&self) -> &str {
        &self.account
    }
    pub fn device(&self) -> &str {
        &self.device
    }
    pub fn source_id(&self) -> &str {
        &self.source
    }
    pub fn epoch(&self) -> u64 {
        self.epoch
    }
    pub fn roster_generation(&self) -> u64 {
        self.generation
    }
    pub fn sender_index(&self) -> u32 {
        self.sender
    }
    pub fn context(&self) -> u16 {
        self.context
    }
    pub fn codec(&self) -> SourceKind {
        self.codec
    }
    pub fn voice_binding(&self) -> Option<&ProtectedSourceBinding> {
        self.voice.as_ref()
    }
}
pub struct VerifiedSource {
    facts: VerifiedSourceFacts,
    lease: SourceLease,
}
impl VerifiedSource {
    pub fn facts(&self) -> &VerifiedSourceFacts {
        &self.facts
    }
    /// Trusted native owner must first bind actual voice/channel/room/offer facts.
    /// This does not add transport approval to the cryptographic lease.
    pub fn into_native_cryptographic_lease(self) -> SourceLease {
        self.lease
    }
}
impl Sdk {
    /// No routing hint or alternate decrypt fallback. Every private wire reaches
    /// real provider exactly once; any unknown/authenticated-invalid domain fails
    /// closed and retires the owner, matching current conservative SDK policy.
    pub fn receive_protected(
        &mut self,
        event: &str,
        wire: &[u8],
        now: u64,
    ) -> Result<ProtectedReceived> {
        uuid(event)?;
        let (epoch, generation, _, _, _) = self.current(now)?;
        // Capture the exact live provider owner before the sole stateful receive.
        // Core receive verifies the private wire belongs to this current epoch;
        // caller labels or a second Core cannot supply this event context.
        let native_scope = self.native_protected_event_scope(now)?;
        let archive_scope = self.native_protected_event_scope(now)?;
        let result = (|| {
            let receipt = self.core.receive_inner_checked(wire, now, |receipt, tx| {
                crate::chat_archive::record_if_typed_chat(tx, archive_scope, event, wire, receipt)
            })?;
            scan(&receipt.plaintext)?;
            let value: Value = coset::cbor::de::from_reader(receipt.plaintext.as_slice())
                .map_err(|_| Error::Invalid)?;
            let Value::Array(fields) = &value else {
                return Err(Error::Invalid);
            };
            let domain = match fields.first() {
                Some(Value::Text(domain))
                    if domain == CHAT_DOMAIN
                        || domain == CHAT_EVENT_DOMAIN
                        || domain == SOURCE_DOMAIN
                        || domain == VOICE_SOURCE_DOMAIN =>
                {
                    domain.as_str()
                }
                _ => return Err(Error::Unauthorized),
            };
            // Exact version/origin/community/channel/group/event and actual
            // authenticated MLS account/device are checked before payload use.
            let payload = self.checked_envelope(domain, event, &receipt)?;
            if domain == CHAT_EVENT_DOMAIN {
                return VerifiedChatEvent::from_authenticated_payload(
                    native_scope,
                    event,
                    &receipt.account,
                    &receipt.device,
                    &payload,
                )
                .map(|event| ProtectedReceived::ChatEvent(Box::new(event)));
            }
            if domain == CHAT_DOMAIN {
                let Value::Text(body) = payload else {
                    return Err(Error::Invalid);
                };
                if body.is_empty() || body.len() > MAX_BODY {
                    return Err(Error::Invalid);
                }
                return Ok(ProtectedReceived::Chat(ChatMessage {
                    event_id: event.into(),
                    account: receipt.account,
                    device: receipt.device,
                    body,
                }));
            }
            let Value::Array(fields) = payload else {
                return Err(Error::Invalid);
            };
            if fields.len() != if domain == VOICE_SOURCE_DOMAIN { 11 } else { 5 } {
                return Err(Error::Invalid);
            }
            let ep = integer(&fields[0])?;
            let sender = u32::try_from(integer(&fields[1])?).map_err(|_| Error::Invalid)?;
            let context = u16::try_from(integer(&fields[2])?).map_err(|_| Error::Invalid)?;
            let Value::Text(source) = &fields[3] else {
                return Err(Error::Invalid);
            };
            uuid(source)?;
            let codec = match &fields[4] {
                Value::Text(s) if s == "opus" => SourceKind::Opus,
                Value::Text(s) if s == "vp8" => SourceKind::Vp8,
                _ => return Err(Error::Invalid),
            };
            let voice = if domain == VOICE_SOURCE_DOMAIN {
                let binding =
                    ProtectedSourceBinding::from_authenticated_fields(codec, &fields[5..])?;
                if binding.voice_channel() != self.binding.channel {
                    return Err(Error::Unauthorized);
                }
                Some(binding)
            } else {
                None
            };
            if ep != epoch
                || ep > u32::MAX as u64
                || sender != receipt.sender
                || sender > u16::MAX as u32
                || context == 0
                || self.received.len() >= 4096
                || !self.received.insert((epoch, sender, context))
            {
                return Err(Error::Unauthorized);
            }
            let facts = VerifiedSourceFacts {
                origin: self.core.pin.scope.origin().into(),
                community: self.core.pin.scope.community().into(),
                channel: self.binding.channel.clone(),
                group: self.core.pin.group.clone(),
                event: event.into(),
                account: receipt.account,
                device: receipt.device,
                source: source.clone(),
                epoch,
                generation,
                sender,
                context,
                codec,
                voice,
            };
            Ok(ProtectedReceived::Source(Box::new(VerifiedSource {
                facts,
                lease: SourceLease {
                    owner: self.core.owner,
                    epoch,
                    generation,
                    sender,
                    context,
                    source: source.clone(),
                    kind: codec,
                    sending: false,
                    announcement: Vec::new(),
                },
            })))
        })();
        if result.is_err() {
            self.retire_native();
        }
        result
    }
}
