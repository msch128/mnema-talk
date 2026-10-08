//! Invite creation acknowledges only an account, never a native session.
use super::*;
pub(super) struct RegistrationDelivery {
    operation: Operation,
    value: User,
}
impl fmt::Debug for RegistrationDelivery {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("RegistrationDelivery(REDACTED)")
    }
}
impl Broker {
    pub(super) async fn registration_delivery(
        &self,
        owner: WindowOwner,
        username: String,
        display_name: String,
        password: Password,
        invite_code: Zeroizing<String>,
    ) -> Result<RegistrationDelivery, Error> {
        let username = username.trim();
        let display_name = display_name.trim();
        let invite_code = invite_code.trim();
        if username.len() < 3
            || username.len() > 32
            || !username
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_.-".contains(&b))
            || display_name.chars().count() > 24
            || display_name.contains('\0')
            || password.text().chars().count() < 10
            || invite_code.is_empty()
            || invite_code.len() > 64
        {
            return Err(Error::InvalidInput);
        }
        let op = {
            let mut state = self.inner.state.lock().map_err(|_| Error::Internal)?;
            self.consume_invocation(&mut state, owner, InvocationKind::Register)?;
            state.main(self.inner.owner, owner)?;
            let active = state.active.as_ref().ok_or(Error::NoProfile)?;
            if active.session.is_some()
                || !matches!(active.status, Status::Selected | Status::ReauthRequired)
            {
                return Err(Error::Denied);
            }
            state.cancel_requests(None);
            let active = state.active.as_mut().unwrap();
            active.cancel.cancel();
            active.cancel = CancellationToken::new();
            active.epoch = active.epoch.checked_add(1).ok_or(Error::Exhausted)?;
            let op = self.reserve_checked(&mut state, owner, RequestPhase::Running)?;
            state.active.as_mut().unwrap().status = Status::Registering;
            op
        };
        let mut completion = LoginGuard {
            inner: self.inner.clone(),
            profile: op.profile,
            epoch: op.epoch,
            armed: true,
        };
        self.drain(Some(op.id)).await?;
        self.verify(&op)?;
        #[derive(serde::Serialize)]
        struct RegisterWire<'a> {
            username: &'a str,
            display_name: &'a str,
            password: &'a str,
            invite_code: &'a str,
        }
        let bytes = Zeroizing::new(
            serde_json::to_vec(&RegisterWire {
                username,
                display_name,
                password: password.text(),
                invite_code,
            })
            .map_err(|_| Error::Internal)?,
        );
        drop(password);
        let response = self
            .send(&op, Endpoint::Register, None, Some(bytes))
            .await?;
        #[derive(serde::Deserialize)]
        #[serde(deny_unknown_fields)]
        struct Created {
            user: User,
        }
        let Created { user } = response.decode()?;
        user.validate()?;
        if user.username != username {
            return Err(Error::Protocol);
        }
        let mut state = self.inner.state.lock().map_err(|_| Error::Internal)?;
        state.current(self.inner.owner, &op)?;
        let active = state.active.as_mut().unwrap();
        if active.session.is_some() {
            return Err(Error::Stale);
        }
        active.status = Status::Selected;
        // Completed account metadata must not keep HTTP drain custody alive
        // while its IPC delivery waits. Final commit still checks original epoch.
        state.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Delivered;
        completion.armed = false;
        Ok(RegistrationDelivery {
            operation: op,
            value: user,
        })
    }
    pub(super) fn commit_registration(
        &self,
        owner: WindowOwner,
        delivery: RegistrationDelivery,
    ) -> Result<User, Error> {
        let state = self.inner.state.lock().map_err(|_| Error::Internal)?;
        if delivery.operation.window != owner {
            return Err(Error::Denied);
        }
        state.current(self.inner.owner, &delivery.operation)?;
        let active = state.active.as_ref().ok_or(Error::Stale)?;
        if active.status != Status::Selected
            || active.session.is_some()
            || active.pending_invocation.is_some()
        {
            return Err(Error::Stale);
        }
        Ok(delivery.value)
    }
}
