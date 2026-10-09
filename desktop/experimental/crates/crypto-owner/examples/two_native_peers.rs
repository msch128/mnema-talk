//! Explicit owned research fixture: actual Go TLS/native auth, two actual owned
//! Mac Keychains, signed device proof, real MLS and native blind relay. Local
//! fixture OOB is NOT an OS root confirmation or a shipping first-run ceremony.
use mnema_crypto_adapter_candidate::Scope;
use mnema_crypto_enrollment_prototype::{Device, NativeAdmissionIntent, NativeSecrets};
use mnema_crypto_sdk_prototype::{Core, Error, NativeBootstrap, Result, Sdk};
use mnema_private_first_community_bootstrap::{FreshCommunity, NativeAdminSubject};
use mnema_private_native_client_broker::{MetadataResource, NativeClient, NativeRequest, Password};
use mnema_private_native_crypto_owner::{
    ChatEventClaim, ChatOperation, NativeChatOwner, NativeTypedChatChange, PendingFirstRoot,
    binding_for_current_native_scope,
};
use mnema_private_native_trust_dialog::DeniedNativeTrustDialog;
use openmls::prelude::tls_codec::Serialize as _;
use serde::Deserialize;
use std::{
    path::PathBuf,
    time::{SystemTime, UNIX_EPOCH},
};
use uuid::Uuid;
use zeroize::Zeroizing;
#[cfg(target_os = "macos")]
#[path = "../qualified-sdk/src/native_fixture.rs"]
mod native_fixture;
fn q<T, E>(
    result: std::result::Result<T, E>,
) -> std::result::Result<T, Box<dyn std::error::Error>> {
    result.map_err(|_| "owned native qualification operation rejected".into())
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Bootstrap {
    protocol: String,
    origin: String,
    community_id: String,
    public_ca_pem: String,
    username: String,
    password: String,
    user_id: String,
    content_authorization: String,
}
fn descriptor(
    path: &std::path::Path,
    username: &str,
) -> std::result::Result<Bootstrap, Box<dyn std::error::Error>> {
    let bytes = std::fs::read(path)?;
    if bytes.len() > 65536 {
        return Err("fixture descriptor bounds".into());
    }
    let d: Bootstrap = serde_json::from_slice(&bytes)?;
    if d.protocol != "mnema-native-loopback-fixture-v1"
        || d.community_id != "native-relay-fixture"
        || d.content_authorization != "unavailable"
        || d.username != username
        || d.password != "native-preview-fixture-password"
    {
        return Err("unrecognized owned fixture".into());
    }
    Ok(d)
}
#[cfg(not(target_os = "macos"))]
fn main() {
    eprintln!("native Mac custody qualification required");
    std::process::exit(1)
}
// Fixture-only process-global Keychain UI/metadata serialization must cover
// the entire current-thread run, including loopback HTTP awaits. No other task
// takes this guard; production owner/broker callbacks do not hold this mutex.
#[allow(clippy::await_holding_lock)]
#[cfg(target_os = "macos")]
fn main() -> std::result::Result<(), Box<dyn std::error::Error>> {
    let bootstrap = PathBuf::from(std::env::var("MNEMA_NATIVE_OWNER_FIXTURE")?);
    let root_desc = descriptor(&bootstrap.join("admin-bootstrap.json"), "relay-admin")?;
    let same_account = std::env::var("MNEMA_NATIVE_OWNER_SAME_ACCOUNT_FIXTURE")
        .ok()
        .as_deref()
        == Some("1");
    let peer_desc = if same_account {
        descriptor(&bootstrap.join("admin-bootstrap.json"), "relay-admin")?
    } else {
        descriptor(&bootstrap.join("bootstrap.json"), "relay-user")?
    };
    if root_desc.origin != peer_desc.origin
        || root_desc.community_id != peer_desc.community_id
        || root_desc.public_ca_pem != peer_desc.public_ca_pem
    {
        return Err("fixture realm mismatch".into());
    }
    let binding: serde_json::Value = serde_json::from_slice(&std::fs::read(
        bootstrap.join("bootstrap.json.binding.json"),
    )?)?;
    if binding["origin"] != root_desc.origin || binding["community_id"] != root_desc.community_id {
        return Err("fixture route mismatch".into());
    }
    let channel = Uuid::parse_str(
        binding["channel_id"]
            .as_str()
            .ok_or("missing fixture channel")?,
    )?;
    let root_client = q(NativeClient::qualification_fixture(
        root_desc.origin.clone(),
        root_desc.community_id.clone(),
        Uuid::parse_str(&root_desc.user_id)?,
        root_desc.public_ca_pem.as_bytes().to_vec(),
    ))?;
    let peer_client = q(NativeClient::qualification_fixture(
        peer_desc.origin.clone(),
        peer_desc.community_id.clone(),
        Uuid::parse_str(&peer_desc.user_id)?,
        peer_desc.public_ca_pem.as_bytes().to_vec(),
    ))?;
    let root_window = q(root_client.attach_main_window())?;
    let peer_window = q(peer_client.attach_main_window())?;
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()?;
    runtime.block_on(async{
        for (client,window,d) in [(&root_client,&root_window,&root_desc),(&peer_client,&peer_window,&peer_desc)]{
            let connect=q(client.request(window,NativeRequest::Connect{address:d.origin.clone()}).await)?;q(client.commit(window,connect))?;
            let login=q(client.request(window,NativeRequest::Login{username:d.username.clone(),password:q(Password::from_native_input(d.password.clone()))?}).await)?;q(client.commit(window,login))?;
            let me=q(client.request(window,NativeRequest::Me).await)?;q(client.commit(window,me))?;
            let hierarchy=q(client.request(window,NativeRequest::Metadata{resource:MetadataResource::Channels}).await)?;
            let current=q(client.commit(window,hierarchy))?;
            let channels=current.body["uncategorized"].as_array().ok_or("actual native channel hierarchy")?;
            let categorized=current.body["categories"].as_array().ok_or("actual native channel categories")?;
            let matches=|v:&serde_json::Value|v["id"]==channel.to_string()&&v["type"]=="text";
            if !channels.iter().any(matches)&&!categorized.iter().any(|category|category["channels"].as_array().is_some_and(|a|a.iter().any(matches))){return Err("selected channel lacks actual native metadata authorization".into())}

        }
        let root_scope=q(root_client.authenticated_scope(&root_window))?;
        let peer_scope=q(peer_client.authenticated_scope(&peer_window))?;
        if root_scope.role()!="admin"||root_scope.account_id().to_string()!=root_desc.user_id||peer_scope.account_id().to_string()!=peer_desc.user_id||root_scope.family_id()==peer_scope.family_id()||root_scope.native_context()==peer_scope.native_context(){return Err("actual native account/family/window evidence mismatch".into())}
        let _guard=q(native_fixture::serial())?;let _no_ui=q(native_fixture::NoUi::begin())?;
        let before=q(native_fixture::snapshot())?;
        let runroot=PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("evidence/runs");std::fs::create_dir_all(&runroot)?;
        let rootdir=tempfile::Builder::new().prefix("actual-owner-root-").tempdir_in(&runroot)?;
        let peerdir=tempfile::Builder::new().prefix("actual-owner-peer-").tempdir_in(&runroot)?;
        // Real native custody + current Go metadata negative ceremony paths.
        // Denied service has no private decision constructor or approval boolean.
        for cancelled in [false,true] {
            let dir=tempfile::Builder::new().prefix("actual-pending-root-").tempdir_in(&runroot)?;
            let mut keys=q(native_fixture::OwnedFixture::create(dir.path()))?;
            if PendingFirstRoot::begin(root_client.clone(),root_window.clone(),&dir.path().join("invalid.sqlite"),&keys.vault,Uuid::new_v4()).await.is_ok(){return Err("invisible root channel accepted".into())}
            if !same_account && PendingFirstRoot::begin(peer_client.clone(),peer_window.clone(),&dir.path().join("nonadmin.sqlite"),&keys.vault,channel).await.is_ok(){return Err("nonadmin root workflow accepted".into())}
            let (pending,cancel)=q(PendingFirstRoot::begin(root_client.clone(),root_window.clone(),&dir.path().join("pending.sqlite"),&keys.vault,channel).await)?;
            let preview=pending.public_preview();
            assert_eq!(preview.account_id,root_desc.user_id);
            assert_eq!(preview.channel_id,channel.to_string());
            assert_eq!(preview.operation_id,cancel.operation_id().to_string());
            assert!(pending.native_deadline()>std::time::Instant::now());
            q(root_client.check_authenticated_scope(&root_window,pending.authenticated_scope()))?;
            if cancelled {q(cancel.cancel())?;assert!(cancel.cancel().is_err());}
            if pending.confirm(&DeniedNativeTrustDialog).await.is_ok(){return Err("denied/cancelled root ceremony adopted owner".into())}
            assert!(cancel.cancel().is_err());
            q(keys.delete())?;
        }
        let mut root_keys=q(native_fixture::OwnedFixture::create(rootdir.path()))?;
        let mut peer_keys=q(native_fixture::OwnedFixture::create(peerdir.path()))?;
        assert!(root_keys.path.exists() && peer_keys.path.exists());
        let root_device=Uuid::new_v4();let peer_device=Uuid::new_v4();
        let when=SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
        let native_scope=q(Scope::new(root_scope.origin(),root_scope.community_id()))?;
        let root_identity=format!("{}.{}",root_scope.account_id(),root_device);
        let subject=q(NativeAdminSubject::from_native_admin_workflow(&root_scope.account_id().to_string(),&root_device.to_string(),root_identity.as_bytes()))?;
        let fresh=q(FreshCommunity::provision_first_group(&rootdir.path().join("root.sqlite"),&root_keys.vault,native_scope.clone(),subject,when))?;
        let (mut issuer,transfer)=fresh.into_native_host();
        q(root_keys.lock())?;
        if root_keys.vault.read_seed("device-key").is_ok(){return Err("locked native signer was readable".into())}
        q(root_keys.unlock())?;

        let root_binding=q(binding_for_current_native_scope(&root_client,&root_window,channel,root_device))?;
        let mut host=q(Sdk::from_fresh_native_host(transfer,root_binding,when))?;
        // Explicit synthetic local OOB pin transferred directly from own native
        // issuer. No server-selected key or role/JSON approval is used.
        let native_pin=issuer.pin_for_native_out_of_band_transfer();
        let device=q(Device::provision_native(&peer_keys.vault,native_pin.clone()))?;
        q(peer_keys.vault.create_seed("database-key"))?;
        let host_facts=q(host.native_owner_facts(when))?;
        let pin=q(NativeBootstrap::from_native_pin(native_scope.clone(),host_facts.group(),native_pin.authority_key(),host_facts.identity(),host_facts.signature_key().try_into().map_err(|_|"native key bounds")?))?;
        let peer_identity=format!("{}.{}",peer_scope.account_id(),peer_device);
        let (mut peer,offer)=q(Core::begin_native_enrollment(&peerdir.path().join("peer.sqlite"),&peer_keys.vault,pin,peer_identity.as_bytes()))?;
        if offer.signature_key!=device.public_key(){return Err("native signer/MLS credential fusion mismatch".into())}
        let invitation=q(issuer.invite_native_reviewed_device(q(NativeAdmissionIntent::from_native_out_of_band_pin(&peer_scope.account_id().to_string(),&peer_device.to_string(),&offer.identity,offer.signature_key.clone().try_into().map_err(|_|"native key bounds")?))?,when))?;
        let roster=q(issuer.admit_proven_device(&q(device.respond(&invitation,when))?,when))?;
        q(host.install_native_roster(&roster,when))?;q(peer.install_roster(&roster,when))?;
        let commit=Uuid::new_v4();let welcome=Uuid::new_v4();let proof_event=Uuid::new_v4();
        let admitted=q(host.add_root_approved_native_peer(&commit.to_string(),&welcome.to_string(),&q(offer.key_package.tls_serialize_detached())?,when))?;
        q(peer.accept_welcome(&admitted.welcome,when))?;
        let challenge=q(peer.peer_challenge_transcript())?;
        let proof=q(host.answer_native_fresh_join(&proof_event.to_string(),&offer.identity,&offer.signature_key,&challenge,when))?;
        q(peer.confirm_peer(&proof,when))?;
        let peer_binding=q(binding_for_current_native_scope(&peer_client,&peer_window,channel,peer_device))?;
        let peer=q(Sdk::bind_native(peer,peer_binding,when))?;
        if std::env::var("MNEMA_NATIVE_OWNER_MIXED_PAGE_FIXTURE").ok().as_deref()==Some("1") {
            // Existing real fixture, with a third independently held device.
            // No synthetic proof or native UI approval is manufactured here.
            let thirddir=tempfile::Builder::new().prefix("actual-owner-removed-").tempdir_in(&runroot)?;
            let mut third_keys=q(native_fixture::OwnedFixture::create(thirddir.path()))?;
            let third_device=Uuid::new_v4();
            let device=q(Device::provision_native(&third_keys.vault,native_pin.clone()))?;
            q(third_keys.vault.create_seed("database-key"))?;
            let facts=q(host.native_owner_facts(when))?;
            let pin=q(NativeBootstrap::from_native_pin(native_scope.clone(),facts.group(),native_pin.authority_key(),facts.identity(),facts.signature_key().try_into().map_err(|_|"native root key bounds")?))?;
            let identity=format!("{}.{}",root_scope.account_id(),third_device);
            let (mut third,offer)=q(Core::begin_native_enrollment(&thirddir.path().join("third.sqlite"),&third_keys.vault,pin,identity.as_bytes()))?;
            assert_eq!(offer.signature_key,device.public_key());
            let intent=|| q(NativeAdmissionIntent::from_native_out_of_band_pin(&root_scope.account_id().to_string(),&third_device.to_string(),&offer.identity,offer.signature_key.clone().try_into().map_err(|_|"native key bounds")?));
            let invitation=q(issuer.invite_native_reviewed_device(intent()?,when))?;
            let roster=q(issuer.admit_proven_device(&q(device.respond(&invitation,when))?,when))?;
            q(host.install_native_roster(&roster,when))?;
            let mut peer=peer;
            q(peer.install_native_roster(&roster,when))?;
            q(third.install_roster(&roster,when))?;
            let add_event=Uuid::new_v4();
            let added=q(host.add_root_approved_native_peer(&add_event.to_string(),&Uuid::new_v4().to_string(),&q(offer.key_package.tls_serialize_detached())?,when))?;
            let staged=q(peer.stage_native_commit(&added.commit,when))?;
            q(peer.inspect_native_commit(staged))?;
            let approved=q(peer.authorize_native_commit(staged,when))?;
            q(peer.merge_native_commit(approved,&add_event.to_string(),when))?;
            q(third.accept_welcome(&added.welcome,when))?;
            let challenge=q(third.peer_challenge_transcript())?;
            let proof=q(host.answer_native_fresh_join(&Uuid::new_v4().to_string(),&offer.identity,&offer.signature_key,&challenge,when))?;
            q(third.confirm_peer(&proof,when))?;
            let old_event=Uuid::new_v4();
            let new_event=Uuid::new_v4();
            let removal=Uuid::new_v4();
            let claim=|event:Uuid,body:&str| q(ChatEventClaim::claim(ChatOperation::Create{message_id:event.to_string(),parent_id:None,body:body.into()}));
            let old_wire=q(host.send_chat_event(&old_event.to_string(),&claim(old_event,"unread chat before actual removal")?,when))?;
            let roster=q(issuer.revoke_exact_native_device(intent()?,when))?;
            q(host.remove_root_revoked_native_peer(&removal.to_string(),&offer.identity,&offer.signature_key,&roster,when))?;
            let control=q(host.pending_native_device_removal(&removal.to_string(),when))?.ok_or("missing durable removal control")?;
            let new_wire=q(host.send_chat_event(&new_event.to_string(),&claim(new_event,"new chat after actual removal")?,when))?;
            let group=q(host.native_owner_facts(when))?.group().to_vec();
            let auth=std::sync::Arc::new(q(root_client.authenticated_scope(&root_window))?);
            let mut numbers=Vec::new();
            for (event,wire) in [(old_event,old_wire),(removal,control),(new_event,new_wire)] {
                use mnema_private_native_client_broker::{NativeOpaqueEvent,NativeOpaqueRelayOperation};
                let input=q(NativeOpaqueEvent::from_native_outbox(channel,event,&group,&wire))?;
                let response=q(root_client.request_opaque_relay_retained(&root_window,auth.clone(),NativeOpaqueRelayOperation::Publish(input)).await)?;
                let number=q(root_client.with_opaque_relay_publication(&root_window,response,|receipt|{
                    assert!(receipt.is_publish_ack());assert_eq!(receipt.records().len(),1);
                    let record=&receipt.records()[0];
                    assert_eq!(record.client_event_id(),event);assert_eq!(record.account_id(),root_scope.account_id());
                    assert_eq!(record.ciphertext(),wire);record.number()
                }))?;
                numbers.push(number);
            }
            assert!(numbers.windows(2).all(|n|n[0]<n[1]));
            let mut receiver=q(NativeChatOwner::from_proven_sdk(peer_client.clone(),peer_window.clone(),peer))?;
            q(receiver.receive_chat_event_page(|publication|{
                assert_eq!(publication.receipts.len(),2);
                assert_eq!(publication.receipts[0].client_event_id,old_event);
                assert_eq!(publication.receipts[1].client_event_id,new_event);
                let bodies=publication.changes.iter().flat_map(|change|match change{NativeTypedChatChange::Created(rows)=>rows.iter().map(|r|r.body.as_str()).collect::<Vec<_>>(),_=>Vec::new()}).collect::<Vec<_>>();
                assert_eq!(bodies,vec!["unread chat before actual removal","new chat after actual removal"]);
                Ok(())
            }).await)?;
            assert_eq!(receiver.native_receive_cursor(),numbers[2]);
            q(receiver.receive_chat_event_page(|publication|{assert!(publication.receipts.is_empty());assert!(publication.changes.is_empty());Ok(())}).await)?;
            receiver.retire();host.retire_native();drop(receiver);drop(host);drop(third);drop(device);drop(issuer);
            q(third_keys.delete())?;q(root_keys.delete())?;q(peer_keys.delete())?;
            assert_eq!(q(native_fixture::snapshot())?,before);
            for (client,window) in [(&root_client,&root_window),(&peer_client,&peer_window)] {let logout=q(client.request(window,NativeRequest::Logout).await)?;q(client.commit(window,logout))?;}
            println!("PASS: actual verified Go TLS/auth/relay and three independent native device vaults; one real mixed page old typed chat / authenticated MLS removal / new typed chat; exact archived history and receipts across epochs, control excluded from chat, complete cursor and empty next page. Local fixture OOB, not OS root approval or recovery qualification.");
            return Ok::<(),Box<dyn std::error::Error>>(());
        }
        let mut root=q(NativeChatOwner::from_proven_sdk(root_client.clone(),root_window.clone(),host))?;
        let mut receiver=q(NativeChatOwner::from_proven_sdk(peer_client.clone(),peer_window.clone(),peer))?;
        let claim = |event: Uuid, body: &str| q(ChatEventClaim::claim(ChatOperation::Create {
            message_id: event.to_string(), parent_id: None, body: body.into(),
        }));
        let created = |publication: &mnema_private_native_crypto_owner::NativeChatEventPublication| {
            publication.changes.iter().flat_map(|change| match change {
                NativeTypedChatChange::Created(rows) => rows.iter().collect::<Vec<_>>(),
                _ => Vec::new(),
            }).map(|row| (row.body.clone(), row.account_id, row.number)).collect::<Vec<_>>()
        };
        let event = Uuid::new_v4();
        let body = "actual native protected typed fixture body";
        let input = claim(event, body)?;
        let job = q(root.prepare_chat_event(event, &input))?;
        if receiver.validate_prepared_chat_event(&job).is_ok() { return Err("foreign native owner accepted pending typed job".into()); }
        q(root.publish_chat_event(job, |publication| {
            let rows = created(publication);
            assert_eq!(rows.len(), 1);
            assert_eq!(rows[0].0, body);
            assert_eq!(rows[0].1.to_string(), root_desc.user_id);
            Ok(())
        }).await)?;
        let duplicate = q(root.prepare_chat_event(event, &input))?;
        q(root.publish_chat_event(duplicate, |publication| {
            assert_eq!(publication.receipts.len(), 1);
            assert_eq!(publication.receipts[0].client_event_id, event);
            assert!(created(publication).is_empty());
            Ok(())
        }).await)?;
        if root.prepare_chat_event(event, &claim(event, "changed body")?).is_ok() { return Err("duplicate body replacement accepted".into()); }
        let received = q(receiver.receive_chat_event_page(|publication| {
            let rows = created(publication);
            assert_eq!(rows.len(), 1);
            assert_eq!(rows[0].0, body);
            assert_eq!(rows[0].1.to_string(), root_desc.user_id);
            Ok(rows[0].2)
        }).await)?;
        // Actual relay/account namespace: same event UUID across two accounts.
        let reply_event = if same_account { Uuid::new_v4() } else { event };
        let reply = q(receiver.prepare_chat_event(reply_event, &claim(reply_event, "actual native peer reply")?))?;
        q(receiver.publish_chat_event(reply, |publication| {
            assert_eq!(publication.receipts[0].account_id.to_string(), peer_desc.user_id);
            Ok(())
        }).await)?;
        q(root.receive_chat_event_page(|publication| {
            // The root's already-published event is re-associated using its
            // encrypted archive, without consuming its own ciphertext in MLS.
            assert_eq!(publication.receipts.len(), 2);
            let rows = created(publication);
            assert_eq!(rows.len(), 1);
            assert_eq!(rows[0].0, "actual native peer reply");
            assert_eq!(rows[0].1.to_string(), peer_desc.user_id);
            assert!(rows[0].2 > received);
            Ok(())
        }).await)?;
        let held_event = Uuid::new_v4();
        let held = q(root.prepare_chat_event(held_event, &claim(held_event, "cancelled native message")?))?;
        let before_cursor = root.native_receive_cursor();
        let refused_event = Uuid::new_v4();
        let refused = q(receiver.prepare_chat_event(refused_event, &claim(refused_event, "actual sink refusal message")?))?;
        q(receiver.publish_chat_event(refused, |_| Ok(())).await)?;
        let refusal = root.receive_chat_event_page::<()>(|publication| {
            let rows = created(publication);
            assert_eq!(rows.len(), 1);
            assert_eq!(rows[0].0, "actual sink refusal message");
            Err(mnema_private_native_crypto_owner::Error::Limit)
        }).await;
        assert_eq!(refusal, Err(mnema_private_native_crypto_owner::Error::Limit));
        assert_eq!(root.native_receive_cursor(), before_cursor);
        let retry = Uuid::new_v4();
        assert!(root.prepare_chat_event(retry, &claim(retry, "no retry after refusal")?).is_err());
        root.retire(); receiver.retire();
        if root.validate_prepared_chat_event(&held).is_ok() { return Err("retired native owner accepted job".into()); }
        drop(root);drop(receiver);drop(peer_scope);drop(root_scope);drop(issuer);
        q(root_keys.delete())?;q(peer_keys.delete())?;
        if q(native_fixture::snapshot())?!=before{return Err("native Keychain metadata changed".into())}
        for (client,window) in [(&root_client,&root_window),(&peer_client,&peer_window)]{let logout=q(client.request(window,NativeRequest::Logout).await)?;q(client.commit(window,logout))?;}
        println!("PASS: actual Go verifiedTLS/currentScope separate native families/windows (sameAccountMode={same_account}); actual Pending root denied/cancelled/invisible/nonadmin gates; two owned native Keychains/same-device signed proof; real checked-MLS Add/Welcome/fresh-peer; native typed exactoutbox POST/idempotent retry/peer receive/reply/accountEventNamespace/archiveOwnHistory/foreignjob/sinkRefusalCursorUnchanged+retire; keys neverJS. Local fixture OOB only: actual OS root modal remains separate.");
        Ok::<(),Box<dyn std::error::Error>>(())
    })
}
