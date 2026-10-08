//! Distinct irreversible password transition; never a refresh or replay.
use super::*;
impl Broker {
    pub(super) async fn password_delivery(
        &self,
        owner: WindowOwner,
        current_password: Password,
        new_password: Password,
    ) -> Result<LoginDelivery, Error> {
        if new_password.text().chars().count() < 10 {
            return Err(Error::InvalidInput);
        }
        let (op, old) = {
            let mut state = self.inner.state.lock().map_err(|_| Error::Internal)?;
            self.consume_invocation(&mut state, owner, InvocationKind::Password)?;
            state.main(self.inner.owner, owner)?;
            let prepared = (|| {
                let active = state.active.as_ref().ok_or(Error::NoProfile)?;
                if active.status != Status::Authenticated {
                    return Err(Error::ReauthRequired);
                }
                let old = active
                    .session
                    .as_ref()
                    .ok_or(Error::ReauthRequired)?
                    .grant
                    .clone();
                if old.access_expires_at <= Utc::now() {
                    return Err(Error::Expired);
                }
                Ok(old)
            })();
            let old = match prepared {
                Ok(old) => old,
                Err(error) => {
                    // Admission already sealed old custody and consuming the
                    // ticket removed its pending marker. A prepare error must
                    // retire the descriptor synchronously, before LoginGuard
                    // exists; never leave a cancelled family marked live.
                    if let Some(active) = state.active.as_mut() {
                        active.session = None;
                        active.status = Status::ReauthRequired;
                        active.cancel.cancel();
                    }
                    return Err(error);
                }
            };
            state.cancel_requests(None);
            let active = state.active.as_mut().unwrap();
            active.cancel.cancel();
            active.cancel = CancellationToken::new();
            active.epoch = active.epoch.checked_add(1).ok_or(Error::Exhausted)?;
            // Reserve the captured old access under this state lock, then remove
            // all ordinary authenticated authority BEFORE credentials dispatch.
            let op = match self.reserve_checked(&mut state, owner, RequestPhase::Running) {
                Ok(op) => op,
                Err(error) => {
                    let active = state.active.as_mut().unwrap();
                    active.session = None;
                    active.status = Status::ReauthRequired;
                    active.cancel.cancel();
                    return Err(error);
                }
            };
            let active = state.active.as_mut().unwrap();
            active.session = None;
            active.status = Status::ChangingCredentials;
            (op, old)
        };
        let mut completion = LoginGuard {
            inner: self.inner.clone(),
            profile: op.profile,
            epoch: op.epoch,
            armed: true,
        };
        self.drain(Some(op.id)).await?;
        self.verify(&op)?;
        self.memory.clear().map_err(|_| Error::Vault)?;
        #[derive(serde::Serialize)]
        struct PasswordWire<'a> {
            current_password: &'a str,
            new_password: &'a str,
        }
        let bytes = Zeroizing::new(
            serde_json::to_vec(&PasswordWire {
                current_password: current_password.text(),
                new_password: new_password.text(),
            })
            .map_err(|_| Error::Internal)?,
        );
        drop(current_password);
        drop(new_password);
        // Exactly one attempt. Any timeout/uncertain ACK leaves reauthentication
        // required; no token/password retry and no old family resurrection.
        let body = self
            .send(
                &op,
                Endpoint::Password,
                Some(&old.access_token),
                Some(bytes),
            )
            .await?;
        let grant: Grant = body.decode()?;
        grant.validate(old.client_instance_id.0)?;
        if grant.user.id != old.user.id
            || grant.family_id == old.family_id
            || grant.refresh_sequence != 0
            || grant.access_token.same(&old.access_token)
            || grant.refresh_token.same(&old.refresh_token)
        {
            return Err(Error::Protocol);
        }
        let access_deadline = authenticated_scope::admission_deadline(&grant)?;
        let user = grant.user.clone();
        let mut state = self.inner.state.lock().map_err(|_| Error::Internal)?;
        state.current(self.inner.owner, &op)?;
        let community = state
            .profiles
            .operation_profile(op.profile)
            .map_err(|_| Error::Stale)?
            .community_id();
        let namespace = Namespace::checked(
            op.origin.as_str(),
            "/",
            community,
            *grant.user.id.0.as_bytes(),
            *grant.client_instance_id.0.as_bytes(),
        )
        .map_err(|_| Error::Protocol)?;
        let record = Record::fresh(
            &namespace,
            *grant.family_id.0.as_bytes(),
            0,
            Refresh::from_native_bytes(grant.refresh_token.bytes()).map_err(|_| Error::Protocol)?,
        )
        .map_err(|_| Error::Vault)?;
        self.journal
            .own(namespace.clone())
            .map_err(|_| Error::Vault)?
            .install_new(record)
            .map_err(|_| Error::Vault)?;
        let active = state.active.as_mut().unwrap();
        let authentication_identity = Uuid::new_v4();
        active.last_authentication_identity = Some(authentication_identity);
        active.session = Some(Session {
            authentication_identity,
            accepted_role: grant.user.role.clone(),
            native_identity: Uuid::new_v4(),
            access_deadline,
            grant: Arc::new(grant),
            namespace,
        });
        active.status = Status::Authenticated;
        completion.armed = false;
        Ok(LoginDelivery {
            authentication_identity,
            scope: Self::scope_from_operation(&op),
            value: user,
        })
    }
}
