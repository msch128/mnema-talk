//! Current owner -> native bearer-owned blind relay. No raw renderer IPC.
use super::*;
use mnema_private_native_client_broker::{NativeOpaqueEvent, NativeOpaqueRelayOperation};
use serde::Serialize;

/// Plaintext projection only after exact native cryptographic/auth checks.
#[derive(Serialize)]
pub struct NativeChatDisplay {
    pub id: Uuid,
    pub number: i64,
    pub channel_id: Uuid,
    pub client_event_id: Uuid,
    pub account_id: Uuid,
    pub device_id: String,
    pub body: String,
}
impl NativeChatOwner {
    /// Native hook must also keep actual Tauri window/document registry authority
    /// through `publish`; no asynchronous/reentrant work inside that callback.
    pub async fn publish_chat<T>(
        &mut self,
        job: NativePreparedChat,
        publish: impl FnOnce(&NativeChatDisplay) -> Result<T>,
    ) -> Result<T> {
        self.validate_prepared(&job)?;
        let input = NativeOpaqueEvent::from_native_outbox(
            job.channel,
            job.event,
            &job.group,
            &job.ciphertext,
        )
        .map_err(|_| Error::Invalid)?;
        let NativePreparedChat {
            auth,
            event,
            epoch,
            generation,
            ..
        } = job;
        let client = self.client.clone();
        let lease = self.lease.clone();
        // An uncertain POST preserves the exact SDK outbox/event/body. Caller
        // can prepare the same event/body and retry identical bytes only.
        let response = client
            .request_opaque_relay_retained(
                &lease,
                auth.clone(),
                NativeOpaqueRelayOperation::Publish(input),
            )
            .await
            .map_err(|_| Error::Auth)?;
        self.check_admitted_scope(&auth)?;
        if self.current(now()?)? != (epoch, generation) {
            self.retire();
            return Err(Error::Binding);
        }
        let receipt = response.opaque_relay_receipt().ok_or(Error::Binding)?;
        if !receipt.is_publish_ack() || receipt.records().len() != 1 {
            return Err(Error::Binding);
        }
        let record = &receipt.records()[0];
        let actual = self
            .sdk
            .pending_chat_for_native_publish(&event.to_string(), now()?)
            .map_err(|_| Error::Crypto)?
            .ok_or(Error::Crypto)?;
        if record.channel_id() != self.owner.channel
            || record.account_id() != self.owner.account
            || record.client_event_id() != event
            || record.group_id() != self.owner.group
            || record.ciphertext() != actual
        {
            self.retire();
            return Err(Error::Binding);
        }
        let pending = self.events.get(&event).ok_or(Error::Binding)?;
        let display = NativeChatDisplay {
            id: record.id(),
            number: record.number(),
            channel_id: self.owner.channel,
            client_event_id: event,
            account_id: self.owner.account,
            device_id: self.owner.device.clone(),
            body: pending.body.to_string(),
        };
        client
            .with_opaque_relay_publication(&lease, response, |_| publish(&display))
            .map_err(|_| Error::Auth)?
    }
    /// Each real received row is MLS-decrypted/authenticated once. Cursor moves
    /// only after fenced native enqueue. A failed final enqueue retires owner:
    /// never replay stateful received bytes or return unauthenticated plaintext.
    pub async fn receive_page<T>(
        &mut self,
        publish: impl FnOnce(&[NativeChatDisplay]) -> Result<T>,
    ) -> Result<T> {
        let admitted = Arc::new(self.scope()?);
        self.receive_page_retained(admitted, publish).await
    }
    pub async fn receive_page_retained<T>(
        &mut self,
        auth: Arc<NativeAuthenticatedScope>,
        publish: impl FnOnce(&[NativeChatDisplay]) -> Result<T>,
    ) -> Result<T> {
        self.check_admitted_scope(&auth)?;
        self.enter_chat_protocol(false)?;
        let when = now()?;
        self.current(when)?;
        let client = self.client.clone();
        let lease = self.lease.clone();
        let after = self.cursor;
        let response = client
            .request_opaque_relay_retained(
                &lease,
                auth.clone(),
                NativeOpaqueRelayOperation::Page {
                    channel: self.owner.channel,
                    after,
                },
            )
            .await
            .map_err(|_| Error::Auth)?;
        self.check_admitted_scope(&auth)?;
        let when = now()?;
        self.current(when)?;
        let receipt = response.opaque_relay_receipt().ok_or(Error::Binding)?;
        if receipt.is_publish_ack() {
            return Err(Error::Binding);
        }
        let mut rows = Vec::with_capacity(receipt.records().len());
        let mut next = after;
        for record in receipt.records() {
            if record.channel_id() != self.owner.channel
                || record.group_id() != self.owner.group
                || record.number() <= next
            {
                self.retire();
                return Err(Error::Binding);
            }
            next = record.number();
            if record.account_id() == self.owner.account
                && self.events.contains_key(&record.client_event_id())
            {
                let actual = self
                    .sdk
                    .pending_chat_for_native_publish(&record.client_event_id().to_string(), when)
                    .map_err(|_| Error::Crypto)?
                    .ok_or(Error::Crypto)?;
                if record.ciphertext() == actual {
                    // Bypass only exact locally committed native ciphertext.
                    // A different approved device of the SAME account still
                    // follows real MLS sender/roster/device authentication.
                    continue;
                }
            }
            let inner = match self.sdk.receive_chat(
                &record.client_event_id().to_string(),
                record.ciphertext(),
                when,
            ) {
                Ok(m) => m,
                Err(_) => {
                    self.retire();
                    return Err(Error::Crypto);
                }
            };
            if inner.account != record.account_id().to_string() {
                self.retire();
                return Err(Error::Binding);
            }
            rows.push(NativeChatDisplay {
                id: record.id(),
                number: record.number(),
                channel_id: self.owner.channel,
                client_event_id: record.client_event_id(),
                account_id: record.account_id(),
                device_id: inner.device,
                body: inner.body,
            });
        }
        if receipt.next_after() != next {
            self.retire();
            return Err(Error::Binding);
        }
        // These checks happen AFTER real stateful receive. If the original
        // admitted scope/epoch is lost now, retire rather than re-read consumed
        // ciphertext under a successor admission.
        if let Err(error) = now()
            .and_then(|when| self.current(when))
            .and_then(|_| self.check_admitted_scope(&auth))
        {
            self.retire();
            return Err(error);
        }
        match client.with_opaque_relay_publication(&lease, response, |_| publish(&rows)) {
            Ok(Ok(value)) => {
                self.cursor = next;
                Ok(value)
            }
            Ok(Err(error)) => {
                self.retire();
                Err(error)
            }
            Err(_) => {
                self.retire();
                Err(Error::Auth)
            }
        }
    }
}
