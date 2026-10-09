//! Typed encrypted chat events, never commands parsed from ordinary chat text.
//! Verified here means authenticated domain/sender, NOT authorized mutation.
//! The native owner must check current account/role, channel/history, references,
//! revision/replay, reaction limits and deletion state before applying an event.
//! Delivery timestamps, message numbers and display profiles come from actual
//! native delivery/history receipts; this schema invents none of those fields.
use super::{Error, NativeProtectedEventScope, Result};
use coset::cbor::value::Value;

pub const CHAT_EVENT_DOMAIN: &str = "MnemaTalk ProtectedChatEvent";
pub const CHAT_EVENT_VERSION: u64 = 1;
pub const MAX_CHAT_SCALARS: usize = 4000;
pub const MAX_CHAT_BYTES: usize = MAX_CHAT_SCALARS * 4;
pub const MAX_REACTION_BYTES: usize = 32;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ChatKind {
    Create,
    Edit,
    Delete,
    Reply,
    Reaction,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ReactionAction {
    Add,
    Remove,
}
impl ReactionAction {
    fn name(self) -> &'static str {
        match self {
            Self::Add => "add",
            Self::Remove => "remove",
        }
    }
}

/// Producer operations are CLAIMS. No author, device, admin/permission flag,
/// channel override, display profile, timestamp or server message number.
/// Thread parent and quoted target remain separate, matching existing Mnema.
/// Create/Reply.message_id is authenticated client event UUID; mutation target,
/// parent, quoted target and expected_revision are actual global relay RECEIPT
/// UUIDs, resolved by native history. No reference carries author permission.
#[derive(Clone)]
pub enum ChatOperation {
    Create {
        message_id: String,
        body: String,
        parent_id: Option<String>,
    },
    Reply {
        message_id: String,
        body: String,
        parent_id: Option<String>,
        reply_to_id: String,
    },
    Edit {
        message_id: String,
        expected_revision: String,
        body: String,
    },
    Delete {
        message_id: String,
        expected_revision: String,
    },
    Reaction {
        message_id: String,
        emoji: String,
        action: ReactionAction,
    },
}
impl ChatOperation {
    pub fn kind(&self) -> ChatKind {
        match self {
            Self::Create { .. } => ChatKind::Create,
            Self::Reply { .. } => ChatKind::Reply,
            Self::Edit { .. } => ChatKind::Edit,
            Self::Delete { .. } => ChatKind::Delete,
            Self::Reaction { .. } => ChatKind::Reaction,
        }
    }
    pub fn message_id(&self) -> &str {
        match self {
            Self::Create { message_id, .. }
            | Self::Reply { message_id, .. }
            | Self::Edit { message_id, .. }
            | Self::Delete { message_id, .. }
            | Self::Reaction { message_id, .. } => message_id,
        }
    }
    pub fn body(&self) -> Option<&str> {
        match self {
            Self::Create { body, .. } | Self::Reply { body, .. } | Self::Edit { body, .. } => {
                Some(body)
            }
            _ => None,
        }
    }
    pub fn parent_id(&self) -> Option<&str> {
        match self {
            Self::Create { parent_id, .. } | Self::Reply { parent_id, .. } => parent_id.as_deref(),
            _ => None,
        }
    }
    pub fn reply_to_id(&self) -> Option<&str> {
        match self {
            Self::Reply { reply_to_id, .. } => Some(reply_to_id),
            _ => None,
        }
    }
    pub fn expected_revision(&self) -> Option<&str> {
        match self {
            Self::Edit {
                expected_revision, ..
            }
            | Self::Delete {
                expected_revision, ..
            } => Some(expected_revision),
            _ => None,
        }
    }
}

/// Shape-checked producer CLAIM. Validation is not an authorship/role grant.
pub struct ChatEventClaim {
    operation: ChatOperation,
}
impl ChatEventClaim {
    pub fn claim(operation: ChatOperation) -> Result<Self> {
        super::uuid(operation.message_id())?;
        if let Some(body) = operation.body() {
            checked_body(body)?;
        }
        if let Some(parent) = operation.parent_id() {
            super::uuid(parent)?;
            if parent == operation.message_id() {
                return Err(Error::Invalid);
            }
        }
        if let Some(reply) = operation.reply_to_id() {
            super::uuid(reply)?;
            if reply == operation.message_id() {
                return Err(Error::Invalid);
            }
        }
        if let Some(revision) = operation.expected_revision() {
            super::uuid(revision)?;
        }
        if let ChatOperation::Reaction { emoji, .. } = &operation {
            checked_emoji(emoji)?;
        }
        Ok(Self { operation })
    }
    pub fn operation(&self) -> &ChatOperation {
        &self.operation
    }
    /// Flat payload, at most six scalar fields, within bounded SDK CBOR scanner.
    /// Empty TEXT in the optional parent slot means no thread parent. Nil/empty
    /// UUID is never a valid actual reference. No null/map/deep array encoding.
    pub(crate) fn payload(&self) -> Value {
        let mut fields = vec![Value::Integer(CHAT_EVENT_VERSION.into())];
        let text = |s: &str| Value::Text(s.into());
        match &self.operation {
            ChatOperation::Create {
                message_id,
                body,
                parent_id,
            } => fields.extend([
                text("create"),
                text(message_id),
                text(parent_id.as_deref().unwrap_or("")),
                text(body),
            ]),
            ChatOperation::Reply {
                message_id,
                body,
                parent_id,
                reply_to_id,
            } => fields.extend([
                text("reply"),
                text(message_id),
                text(parent_id.as_deref().unwrap_or("")),
                text(reply_to_id),
                text(body),
            ]),
            ChatOperation::Edit {
                message_id,
                expected_revision,
                body,
            } => fields.extend([
                text("edit"),
                text(message_id),
                text(expected_revision),
                text(body),
            ]),
            ChatOperation::Delete {
                message_id,
                expected_revision,
            } => fields.extend([text("delete"), text(message_id), text(expected_revision)]),
            ChatOperation::Reaction {
                message_id,
                emoji,
                action,
            } => fields.extend([
                text("reaction"),
                text(message_id),
                text(emoji),
                text(action.name()),
            ]),
        }
        Value::Array(fields)
    }
}

/// Only the native one-decrypt authenticated dispatcher may construct this.
/// Account/device are actual MLS receipt + signed roster facts, never payload.
/// A valid event still MUST pass current native history/authorization admission.
pub struct VerifiedChatEvent {
    scope: NativeProtectedEventScope,
    event_id: String,
    account: String,
    device: String,
    claim: ChatEventClaim,
}
impl VerifiedChatEvent {
    pub(crate) fn from_authenticated_payload(
        scope: NativeProtectedEventScope,
        event_id: &str,
        account: &str,
        device: &str,
        payload: &Value,
    ) -> Result<Self> {
        super::id(account)?;
        super::id(device)?;
        let claim = ChatEventClaim::from_authenticated_payload(event_id, payload)?;
        Ok(Self {
            scope,
            event_id: event_id.into(),
            account: account.into(),
            device: device.into(),
            claim,
        })
    }
    pub fn scope(&self) -> &NativeProtectedEventScope {
        &self.scope
    }
    pub fn event_id(&self) -> &str {
        &self.event_id
    }
    pub fn account(&self) -> &str {
        &self.account
    }
    pub fn device(&self) -> &str {
        &self.device
    }
    pub fn operation(&self) -> &ChatOperation {
        self.claim.operation()
    }
}

fn checked_optional_reference(value: &str) -> Result<Option<String>> {
    if value.is_empty() {
        return Ok(None);
    }
    super::uuid(value)?;
    Ok(Some(value.into()))
}
fn checked_body(value: &str) -> Result<()> {
    // Same 4000-Unicode-scalar and NUL policy as Go CleanText. Native composer
    // must trim BEFORE claim; receive rejects padding instead of changing text.
    // Empty attachment messages need separately qualified encrypted media refs.
    if value.is_empty()
        || value != value.trim()
        || value.contains('\0')
        || value.len() > MAX_CHAT_BYTES
        || value.chars().count() > MAX_CHAT_SCALARS
    {
        return Err(Error::Invalid);
    }
    Ok(())
}
fn checked_emoji(value: &str) -> Result<()> {
    if value.is_empty()
        || value.len() > MAX_REACTION_BYTES
        || value
            .chars()
            .any(|c| c.is_whitespace() || c.is_control() || c == '<' || c == '>')
    {
        return Err(Error::Invalid);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    fn scope() -> NativeProtectedEventScope {
        super::super::chat_event_projection::fixture_scope()
    }
    const MESSAGE: &str = "11111111-1111-4111-8111-111111111111";
    const MUTATION: &str = "22222222-2222-4222-8222-222222222222";
    const TARGET: &str = "33333333-3333-4333-8333-333333333333";
    fn create(body: &str) -> ChatOperation {
        ChatOperation::Create {
            message_id: MESSAGE.into(),
            body: body.into(),
            parent_id: None,
        }
    }
    fn decoded(operation: ChatOperation, event: &str) -> Result<VerifiedChatEvent> {
        VerifiedChatEvent::from_authenticated_payload(
            scope(),
            event,
            "native-account",
            "native-device",
            &ChatEventClaim::claim(operation)?.payload(),
        )
    }
    #[test]
    fn all_operations_actual_cbor_roundtrip_keep_sender_and_distinct_references() {
        let cases = [
            create("text"),
            ChatOperation::Reply {
                message_id: MESSAGE.into(),
                body: "quote".into(),
                parent_id: Some(TARGET.into()),
                reply_to_id: TARGET.into(),
            },
            ChatOperation::Edit {
                message_id: MESSAGE.into(),
                expected_revision: MESSAGE.into(),
                body: "edit".into(),
            },
            ChatOperation::Delete {
                message_id: MESSAGE.into(),
                expected_revision: MESSAGE.into(),
            },
            ChatOperation::Reaction {
                message_id: MESSAGE.into(),
                emoji: "👩‍💻".into(),
                action: ReactionAction::Add,
            },
            ChatOperation::Reaction {
                message_id: MESSAGE.into(),
                emoji: "ok".into(),
                action: ReactionAction::Remove,
            },
        ];
        for operation in cases {
            let kind = operation.kind();
            let payload = ChatEventClaim::claim(operation).unwrap().payload();
            let mut wire = Vec::new();
            coset::cbor::ser::into_writer(&payload, &mut wire).unwrap();
            let actual = coset::cbor::de::from_reader(wire.as_slice()).unwrap();
            assert_eq!(payload, actual);
            let event = if matches!(kind, ChatKind::Create | ChatKind::Reply) {
                MESSAGE
            } else {
                MUTATION
            };
            let output = VerifiedChatEvent::from_authenticated_payload(
                scope(),
                event,
                "native-account",
                "native-device",
                &actual,
            )
            .unwrap();
            assert_eq!(output.event_id(), event);
            assert_eq!(output.account(), "native-account");
            assert_eq!(output.device(), "native-device");
            assert_eq!(output.operation().kind(), kind);
            assert_eq!(output.operation().message_id(), MESSAGE);
            if kind == ChatKind::Reply {
                assert_eq!(output.operation().parent_id(), Some(TARGET));
                assert_eq!(output.operation().reply_to_id(), Some(TARGET));
            }
            if matches!(kind, ChatKind::Edit | ChatKind::Delete) {
                assert_eq!(output.operation().expected_revision(), Some(MESSAGE));
            }
        }
    }
    #[test]
    fn unicode_scalar_byte_bound_and_text_are_preserved_without_json_command_parsing() {
        for body in [
            "🦀".repeat(MAX_CHAT_SCALARS),
            "e\u{301}".repeat(2000),
            "first\nsecond\tthird".into(),
            "{\"type\":\"delete\",\"admin\":true}".into(),
        ] {
            let event = decoded(create(&body), MESSAGE).unwrap();
            assert_eq!(event.operation().body(), Some(body.as_str()));
            assert_eq!(event.operation().kind(), ChatKind::Create);
        }
        for body in [
            "".into(),
            " ".into(),
            " text".into(),
            "text\n".into(),
            "x\0y".into(),
            "x".repeat(MAX_CHAT_SCALARS + 1),
            "🦀".repeat(MAX_CHAT_SCALARS + 1),
        ] {
            assert!(ChatEventClaim::claim(create(&body)).is_err());
        }
    }
    #[test]
    fn canonical_refs_and_no_self_reply_or_thread_parent() {
        for invalid in [
            "",
            "00000000-0000-0000-0000-000000000000",
            "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA",
            "not-id",
        ] {
            assert!(
                ChatEventClaim::claim(ChatOperation::Edit {
                    message_id: invalid.into(),
                    expected_revision: TARGET.into(),
                    body: "text".into()
                })
                .is_err()
            );
            assert!(
                ChatEventClaim::claim(ChatOperation::Delete {
                    message_id: MESSAGE.into(),
                    expected_revision: invalid.into()
                })
                .is_err()
            );
            assert!(
                ChatEventClaim::claim(ChatOperation::Reply {
                    message_id: MESSAGE.into(),
                    body: "text".into(),
                    parent_id: Some(invalid.into()),
                    reply_to_id: TARGET.into()
                })
                .is_err()
            );
            assert!(
                ChatEventClaim::claim(ChatOperation::Reply {
                    message_id: MESSAGE.into(),
                    body: "text".into(),
                    parent_id: None,
                    reply_to_id: invalid.into()
                })
                .is_err()
            );
        }
        assert!(
            ChatEventClaim::claim(ChatOperation::Create {
                message_id: MESSAGE.into(),
                body: "text".into(),
                parent_id: Some(MESSAGE.into())
            })
            .is_err()
        );
        assert!(
            ChatEventClaim::claim(ChatOperation::Reply {
                message_id: MESSAGE.into(),
                body: "text".into(),
                parent_id: None,
                reply_to_id: MESSAGE.into()
            })
            .is_err()
        );
        assert!(
            decoded(
                ChatOperation::Reply {
                    message_id: MESSAGE.into(),
                    body: "text".into(),
                    parent_id: None,
                    reply_to_id: TARGET.into()
                },
                MESSAGE
            )
            .is_ok()
        );
    }
    #[test]
    fn create_identity_and_mutation_event_revision_are_bound_to_authenticated_envelope() {
        assert!(matches!(
            decoded(create("text"), MUTATION),
            Err(Error::Unauthorized)
        ));
        assert!(matches!(
            decoded(
                ChatOperation::Delete {
                    message_id: MESSAGE.into(),
                    expected_revision: TARGET.into()
                },
                MESSAGE
            ),
            Err(Error::Unauthorized)
        ));
        assert!(matches!(
            decoded(
                ChatOperation::Edit {
                    message_id: MESSAGE.into(),
                    expected_revision: MUTATION.into(),
                    body: "text".into()
                },
                MUTATION
            ),
            Err(Error::Unauthorized)
        ));
        let payload = ChatEventClaim::claim(create("text")).unwrap().payload();
        for (account, device) in [
            ("", "device"),
            ("account", ""),
            ("acct\n", "device"),
            ("account", "device "),
        ] {
            assert!(
                VerifiedChatEvent::from_authenticated_payload(
                    scope(),
                    MESSAGE,
                    account,
                    device,
                    &payload
                )
                .is_err()
            );
        }
    }
    #[test]
    fn reaction_uses_explicit_idempotent_action_and_existing_unicode_token_bounds() {
        for emoji in ["x".repeat(MAX_REACTION_BYTES), "👨‍👩‍👧‍👦".into(), "👍🏽".into()]
        {
            assert!(
                decoded(
                    ChatOperation::Reaction {
                        message_id: MESSAGE.into(),
                        emoji,
                        action: ReactionAction::Add
                    },
                    MUTATION
                )
                .is_ok()
            );
        }
        for emoji in [
            "".into(),
            "x".repeat(MAX_REACTION_BYTES + 1),
            "a b".into(),
            "a\u{a0}b".into(),
            "ok\0".into(),
            "ok\u{7f}".into(),
            "<ok>".into(),
            "ok\n".into(),
        ] {
            assert!(
                ChatEventClaim::claim(ChatOperation::Reaction {
                    message_id: MESSAGE.into(),
                    emoji,
                    action: ReactionAction::Remove
                })
                .is_err()
            );
        }
        let claim = ChatEventClaim::claim(ChatOperation::Reaction {
            message_id: MESSAGE.into(),
            emoji: "ok".into(),
            action: ReactionAction::Remove,
        })
        .unwrap();
        let Value::Array(mut fields) = claim.payload() else {
            panic!()
        };
        fields[4] = Value::Text("toggle".into());
        assert!(
            VerifiedChatEvent::from_authenticated_payload(
                scope(),
                MUTATION,
                "account",
                "device",
                &Value::Array(fields)
            )
            .is_err()
        );
    }
    #[test]
    fn exact_schema_arity_version_scalar_types_and_no_extra_author_or_role_fields() {
        let Value::Array(good) = ChatEventClaim::claim(create("text")).unwrap().payload() else {
            panic!()
        };
        let reject = |fields: Vec<Value>| {
            assert!(
                VerifiedChatEvent::from_authenticated_payload(
                    scope(),
                    MESSAGE,
                    "account",
                    "device",
                    &Value::Array(fields)
                )
                .is_err()
            )
        };
        for length in 0..good.len() {
            reject(good[..length].to_vec());
        }
        let mut extra = good.clone();
        extra.push(Value::Text("admin".into()));
        reject(extra);
        for index in 0..good.len() {
            for value in [
                Value::Null,
                Value::Bool(true),
                Value::Bytes(vec![]),
                Value::Array(vec![]),
                Value::Map(vec![]),
            ] {
                let mut fields = good.clone();
                fields[index] = value;
                reject(fields);
            }
        }
        for value in [
            Value::Integer(0.into()),
            Value::Integer(2.into()),
            Value::Text("1".into()),
            Value::Float(1.0),
        ] {
            let mut fields = good.clone();
            fields[0] = value;
            reject(fields);
        }
        for kind in ["Create", "chat", "message_create", "delete ", "unknown"] {
            let mut fields = good.clone();
            fields[1] = Value::Text(kind.into());
            reject(fields);
        }
        assert!(
            VerifiedChatEvent::from_authenticated_payload(
                scope(),
                MESSAGE,
                "account",
                "device",
                &Value::Text("{\"type\":\"delete\"}".into())
            )
            .is_err()
        );
    }
}

impl ChatEventClaim {
    // Shared shape/event parser. This returns a CLAIM, never a live SDK scope
    // or authentication/permission. Callers must already own the evidence.
    pub(crate) fn from_authenticated_payload(event_id: &str, payload: &Value) -> Result<Self> {
        super::uuid(event_id)?;
        let Value::Array(fields) = payload else {
            return Err(Error::Invalid);
        };
        if !matches!(fields.first(), Some(Value::Integer(version))
            if u64::try_from(*version) == Ok(CHAT_EVENT_VERSION))
        {
            return Err(Error::Invalid);
        }
        let operation = match fields.as_slice() {
            [
                _,
                Value::Text(kind),
                Value::Text(message),
                Value::Text(parent),
                Value::Text(body),
            ] if kind == "create" => ChatOperation::Create {
                message_id: message.clone(),
                parent_id: checked_optional_reference(parent)?,
                body: body.clone(),
            },
            [
                _,
                Value::Text(kind),
                Value::Text(message),
                Value::Text(parent),
                Value::Text(reply),
                Value::Text(body),
            ] if kind == "reply" => ChatOperation::Reply {
                message_id: message.clone(),
                parent_id: checked_optional_reference(parent)?,
                reply_to_id: reply.clone(),
                body: body.clone(),
            },
            [
                _,
                Value::Text(kind),
                Value::Text(message),
                Value::Text(revision),
                Value::Text(body),
            ] if kind == "edit" => ChatOperation::Edit {
                message_id: message.clone(),
                expected_revision: revision.clone(),
                body: body.clone(),
            },
            [
                _,
                Value::Text(kind),
                Value::Text(message),
                Value::Text(revision),
            ] if kind == "delete" => ChatOperation::Delete {
                message_id: message.clone(),
                expected_revision: revision.clone(),
            },
            [
                _,
                Value::Text(kind),
                Value::Text(message),
                Value::Text(emoji),
                Value::Text(action),
            ] if kind == "reaction" => ChatOperation::Reaction {
                message_id: message.clone(),
                emoji: emoji.clone(),
                action: match action.as_str() {
                    "add" => ReactionAction::Add,
                    "remove" => ReactionAction::Remove,
                    _ => return Err(Error::Invalid),
                },
            },
            _ => return Err(Error::Invalid),
        };
        let claim = ChatEventClaim::claim(operation)?;
        match claim.operation().kind() {
            ChatKind::Create | ChatKind::Reply if claim.operation().message_id() != event_id => {
                return Err(Error::Unauthorized);
            }
            ChatKind::Edit | ChatKind::Delete | ChatKind::Reaction
                if claim.operation().message_id() == event_id =>
            {
                return Err(Error::Unauthorized);
            }
            _ => {}
        }
        if claim.operation().expected_revision() == Some(event_id) {
            return Err(Error::Unauthorized);
        }
        Ok(claim)
    }
}
