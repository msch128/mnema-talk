//! Typed shared-client commands on the existing native crypto-owner thread.
//! The original authenticated allocation survives queueing, metadata and output.
use super::*;
use mnema_private_native_crypto_owner::{
    ChatEventClaim, ChatOperation, NativeChatOwner, ReactionAction,
};
use serde::Deserialize;

#[derive(Clone, PartialEq, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case", deny_unknown_fields)]
pub(crate) enum Input {
    Create {
        body: String,
    },
    Reply {
        body: String,
        parent_receipt_id: Option<String>,
        quoted_receipt_id: Option<String>,
    },
    Edit {
        target_receipt_id: String,
        expected_revision: String,
        body: String,
    },
    Delete {
        target_receipt_id: String,
        expected_revision: String,
    },
    Reaction {
        target_receipt_id: String,
        emoji: String,
        action: Toggle,
    },
}
#[derive(Clone, PartialEq, Deserialize)]
#[serde(rename_all = "snake_case")]
pub(crate) enum Toggle {
    Toggle,
}

#[derive(Default)]
pub(super) struct Claims(std::collections::HashMap<Uuid, (Input, ChatOperation)>);

impl Input {
    fn validate(&self) -> Result<(), Error> {
        let body = match self {
            Self::Create { body } | Self::Reply { body, .. } | Self::Edit { body, .. } => {
                Some(body)
            }
            _ => None,
        };
        if body.is_some_and(|body| body.trim().is_empty() || body.encode_utf16().count() > 4000) {
            return Err(Error::InvalidInput);
        }
        if let Self::Reaction { emoji, .. } = self
            && (emoji.is_empty() || emoji.len() > 128)
        {
            return Err(Error::InvalidInput);
        }
        Ok(())
    }
}

fn claim(
    chat: &mut NativeChatOwner,
    scope: Arc<NativeAuthenticatedScope>,
    event: Uuid,
    input: Input,
) -> Result<ChatEventClaim, Error> {
    let receipt = |id: String| canonical(&id).map(|id| id.to_string());
    let operation = match input {
        Input::Create { body } => ChatOperation::Create {
            message_id: event.to_string(),
            body,
            parent_id: None,
        },
        Input::Reply {
            body,
            parent_receipt_id,
            quoted_receipt_id,
        } => {
            let parent_id = parent_receipt_id.map(receipt).transpose()?;
            match quoted_receipt_id {
                Some(id) => ChatOperation::Reply {
                    message_id: event.to_string(),
                    body,
                    parent_id,
                    reply_to_id: receipt(id)?,
                },
                None if parent_id.is_some() => ChatOperation::Create {
                    message_id: event.to_string(),
                    body,
                    parent_id,
                },
                None => return Err(Error::InvalidInput),
            }
        }
        Input::Edit {
            target_receipt_id,
            expected_revision,
            body,
        } => ChatOperation::Edit {
            message_id: receipt(target_receipt_id)?,
            expected_revision: receipt(expected_revision)?,
            body,
        },
        Input::Delete {
            target_receipt_id,
            expected_revision,
        } => ChatOperation::Delete {
            message_id: receipt(target_receipt_id)?,
            expected_revision: receipt(expected_revision)?,
        },
        Input::Reaction {
            target_receipt_id,
            emoji,
            action: Toggle::Toggle,
        } => {
            let target = canonical(&target_receipt_id)?;
            let removed = chat
                .publish_cached_chat_event_message_retained(scope.clone(), target, |row| {
                    if row.deleted {
                        return Err(mnema_private_native_crypto_owner::Error::Invalid);
                    }
                    Ok(row
                        .reactions
                        .get(&emoji)
                        .is_some_and(|users| users.contains(&scope.account_id())))
                })
                .map_err(crypto_error)?
                .ok_or(Error::Denied)?;
            ChatOperation::Reaction {
                message_id: target.to_string(),
                emoji,
                action: if removed {
                    ReactionAction::Remove
                } else {
                    ReactionAction::Add
                },
            }
        }
    };
    ChatEventClaim::claim(operation).map_err(|_| Error::InvalidInput)
}

fn history(
    chat: &mut NativeChatOwner,
    scope: Arc<NativeAuthenticatedScope>,
    parent: Option<Uuid>,
) -> Result<Vec<serde_json::Value>, Error> {
    let mut rows = Vec::new();
    let mut before = None;
    loop {
        let (mut page, older, next) = chat
            .publish_cached_chat_event_history_page_retained(
                scope.clone(),
                parent,
                before,
                25,
                |page| {
                    let rows = page
                        .messages
                        .iter()
                        .map(serde_json::to_value)
                        .collect::<Result<Vec<_>, _>>()
                        .map_err(|_| mnema_private_native_crypto_owner::Error::Invalid)?;
                    Ok((rows, page.has_older, page.next_before))
                },
            )
            .map_err(crypto_error)?;
        rows.append(&mut page);
        if rows.len() > 1024 {
            return Err(Error::BodyLimit);
        }
        if !older {
            return Ok(rows);
        }
        let next = next.ok_or(Error::Protocol)?;
        if before.is_some_and(|old| next >= old) {
            return Err(Error::Protocol);
        }
        before = Some(next);
    }
}

#[allow(clippy::too_many_arguments)]
pub(super) async fn process(
    chat: &mut NativeChatOwner,
    claims: &mut Claims,
    app: &tauri::AppHandle,
    client: &NativeClient,
    lease: &NativeWindowLease,
    control: &Arc<Control>,
    scope: Arc<NativeAuthenticatedScope>,
    event: Option<Uuid>,
    input: Option<Input>,
    output: Channel<serde_json::Value>,
) -> Result<Option<Uuid>, Error> {
    client.check_authenticated_scope(lease, &scope)?;
    let affected = match (event, input) {
        (Some(event), Some(input)) => {
            let original = input.clone();
            let claim = if let Some((previous, operation)) = claims.0.get(&event) {
                if previous != &input {
                    return Err(Error::Denied);
                }
                // An unknown ACK may already have toggled the native history.
                // Retry this exact admitted operation, never its inverse.
                ChatEventClaim::claim(operation.clone()).map_err(|_| Error::InvalidInput)?
            } else {
                if claims.0.len() >= 256 {
                    return Err(Error::Busy);
                }
                claim(chat, scope.clone(), event, input)?
            };
            let prepared = chat
                .prepare_chat_event_retained(scope.clone(), event, &claim)
                .map_err(crypto_error)?;
            claims
                .0
                .entry(event)
                .or_insert_with(|| (original, claim.operation().clone()));
            Some(
                chat.publish_chat_event(prepared, |publication| {
                    let matching = publication
                        .receipts
                        .iter()
                        .filter(|row| {
                            row.client_event_id == event && row.account_id == scope.account_id()
                        })
                        .collect::<Vec<_>>();
                    if matching.len() != 1 {
                        return Err(mnema_private_native_crypto_owner::Error::Binding);
                    }
                    Ok(matching[0].message_id)
                })
                .await
                .map_err(crypto_error)?,
            )
        }
        (None, None) => {
            // Bounded catch-up of actual event pages. No mutation is retried.
            let mut received = 0;
            for _ in 0..=1024 {
                let count = chat
                    .receive_chat_event_page_retained(scope.clone(), |publication| {
                        Ok(publication.receipts.len())
                    })
                    .await
                    .map_err(crypto_error)?;
                if count == 0 {
                    break;
                }
                received += count;
                if received > 1024 {
                    return Err(Error::BodyLimit);
                }
            }
            None
        }
        _ => return Err(Error::InvalidInput),
    };
    let mut rows = history(chat, scope.clone(), None)?;
    let roots = rows
        .iter()
        .map(|row| {
            row.get("id")
                .and_then(serde_json::Value::as_str)
                .ok_or(Error::Protocol)
                .and_then(canonical)
        })
        .collect::<Result<Vec<_>, _>>()?;
    for root in roots {
        rows.extend(history(chat, scope.clone(), Some(root))?);
        if rows.len() > 1024 {
            return Err(Error::BodyLimit);
        }
    }
    // Author display data comes from this instance's real members response.
    // Missing profiles fail closed; no usernames, dates or revisions are made up.
    let publication = client
        .request_retained_authenticated(
            lease,
            scope.clone(),
            Uuid::new_v4(),
            NativeRequest::Metadata {
                resource: MetadataResource::Members,
            },
        )
        .await?;
    let members = client.commit(lease, publication)?;
    if members.status != 200 {
        return Err(Error::Protocol);
    }
    let members = members.body.as_array().ok_or(Error::Protocol)?;
    for row in &mut rows {
        let account = row
            .get("account_id")
            .and_then(serde_json::Value::as_str)
            .ok_or(Error::Protocol)?;
        let author = members
            .iter()
            .find(|member| member.get("id").and_then(serde_json::Value::as_str) == Some(account))
            .ok_or(Error::Protocol)?;
        let mut author_fields = serde_json::Map::new();
        for field in ["id", "username", "display_name", "avatar_url"] {
            let value = author
                .get(field)
                .filter(|value| value.is_string())
                .ok_or(Error::Protocol)?;
            author_fields.insert(field.into(), value.clone());
        }
        let row = row.as_object_mut().ok_or(Error::Protocol)?;
        row.insert("channel_id".into(), serde_json::json!(control.channel));
        row.insert("author".into(), serde_json::Value::Object(author_fields));
        // This native version admits text only; attachments/pins/mentions have
        // no events in its closed schema. These values assert no extra grants.
        row.insert("attachments".into(), serde_json::json!([]));
        row.insert("is_pinned".into(), serde_json::json!(false));
        row.insert("mentions".into(), serde_json::json!([]));
    }
    let snapshot = serde_json::json!({"channel_id":control.channel,"messages":rows});
    if serde_json::to_vec(&snapshot)
        .map_err(|_| Error::Protocol)?
        .len()
        > 512 * 1024
    {
        return Err(Error::BodyLimit);
    }
    client.with_authenticated_publication(lease, &scope, || {
        let state = app.state::<RuntimeState>();
        let registry = state.registry.try_lock().map_err(|_| Error::Stale)?;
        let entry = registry.as_ref().ok_or(Error::Stale)?;
        if control.stop.load(Ordering::Acquire)
            || entry.document != NativeDocument::App
            || entry.lease.context_nonce() != lease.context_nonce()
        {
            return Err(Error::Stale);
        }
        output.send(snapshot).map_err(|_| Error::Stale)
    })??;
    Ok(affected)
}

#[allow(clippy::too_many_arguments)]
async fn command(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: String,
    channel_id: String,
    event: Option<String>,
    input: Option<Input>,
    output: Channel<serde_json::Value>,
) -> NativeReply {
    let result = async {
        let lease = require(&window, &state, Some(&context))?;
        let profile = canonical(&profile_intent)?;
        state.client.check_profile_intent(&lease, profile)?;
        let channel = canonical(&channel_id)?;
        let event = event.map(|id| canonical(&id)).transpose()?;
        if let Some(input) = &input {
            input.validate()?;
        }
        let control = state.core.current_profile(&lease, None, profile)?;
        let scope = Arc::new(
            state
                .client
                .authenticated_scope_with_intent(&lease, canonical(&authentication_intent)?)?,
        );
        if control.channel != channel || !control.same_family(&scope) {
            return Err(Error::Denied);
        }
        if control
            .status
            .lock()
            .map_err(|_| Error::Internal)?
            .as_ref()
            .is_none_or(|status| status.state != "root_saved")
        {
            return Err(Error::QualificationRequired);
        }
        let (reply, answer) = oneshot::channel();
        state
            .client
            .with_authenticated_publication(&lease, &scope, || {
                control
                    .jobs
                    .try_send(Job::TypedChat {
                        scope: scope.clone(),
                        event,
                        input,
                        output,
                        reply,
                    })
                    .map_err(|_| Error::Busy)
            })??;
        let affected = answer.await.map_err(|_| Error::Stale)??;
        require(&window, &state, Some(&context))?;
        state.client.check_authenticated_scope(&lease, &scope)?;
        if control.stop.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        Ok::<_, Error>(match affected {
            Some(id) => serde_json::json!({"state":"completed","message_id":id}),
            None => serde_json::json!({"state":"completed"}),
        })
    }
    .await;
    match result {
        Ok(body) => reply(context, body),
        Err(error) => rejected(context, error),
    }
}

#[tauri::command]
#[allow(clippy::too_many_arguments)]
pub(crate) async fn native_chat_snapshot(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: String,
    channel_id: String,
    on_messages: Channel<serde_json::Value>,
) -> Result<NativeReply, ()> {
    Ok(command(
        window,
        state,
        context,
        profile_intent,
        authentication_intent,
        channel_id,
        None,
        None,
        on_messages,
    )
    .await)
}

#[tauri::command]
#[allow(clippy::too_many_arguments)]
pub(crate) async fn native_chat_mutate(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    authentication_intent: String,
    channel_id: String,
    client_event_id: String,
    input: Input,
    on_messages: Channel<serde_json::Value>,
) -> Result<NativeReply, ()> {
    Ok(command(
        window,
        state,
        context,
        profile_intent,
        authentication_intent,
        channel_id,
        Some(client_event_id),
        Some(input),
        on_messages,
    )
    .await)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn renderer_cannot_supply_authority_or_unknown_operation_fields() {
        assert!(
            serde_json::from_value::<Input>(serde_json::json!({"kind":"create","body":"hello"}))
                .is_ok()
        );
        for extra in [
            "account_id",
            "device_id",
            "created_at",
            "admin",
            "channel_id",
            "grant",
        ] {
            let mut value = serde_json::json!({"kind":"create","body":"hello"});
            value[extra] = serde_json::json!("untrusted");
            assert!(serde_json::from_value::<Input>(value).is_err());
        }
        assert!(serde_json::from_value::<Input>(serde_json::json!({"kind":"reaction","target_receipt_id":Uuid::new_v4(),"emoji":"a","action":"add"})).is_err());
    }

    #[test]
    fn typed_body_budget_matches_shared_utf16_decoder_before_queueing() {
        assert!(
            Input::Create {
                body: "a".repeat(4000)
            }
            .validate()
            .is_ok()
        );
        assert!(
            Input::Create {
                body: "a".repeat(4001)
            }
            .validate()
            .is_err()
        );
        assert!(
            Input::Create {
                body: "🔒".repeat(2000)
            }
            .validate()
            .is_ok()
        );
        assert!(
            Input::Create {
                body: "🔒".repeat(2001)
            }
            .validate()
            .is_err()
        );
        assert!(
            Input::Create {
                body: " \t\n".into()
            }
            .validate()
            .is_err()
        );
    }
}
