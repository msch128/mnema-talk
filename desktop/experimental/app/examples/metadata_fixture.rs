//! Explicit ignored, known-dummy PG/TLS qualification; never a shipped client.
#[path = "../src/transport_fixture.rs"]
mod transport_fixture;
use mnema_private_native_client_broker::{MetadataResource, NativeRequest, Password};
use serde_json::json;
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let path = std::env::var("MNEMA_NATIVE_TRANSPORT_BOOTSTRAP")?;
    let client = transport_fixture::client()?;
    let bootstrap: serde_json::Value = serde_json::from_slice(&std::fs::read(path)?)?;
    let origin = bootstrap["origin"]
        .as_str()
        .ok_or("missing origin")?
        .to_owned();
    let window = client.attach_main_window()?;
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()?;
    runtime.block_on(async {
        let connected = client.request(&window, NativeRequest::Connect { address: origin }).await?;
        client.commit(&window, connected)?;
        let login = client.request(&window, NativeRequest::Login {
            username: "preview-user".to_owned(),
            password: Password::from_native_input("native-preview-fixture-password".to_owned())?,
        }).await?;
        client.commit(&window, login)?;
        let (channels, members, read_state) = tokio::join!(
            client.request(&window, NativeRequest::Metadata { resource: MetadataResource::Channels }),
            client.request(&window, NativeRequest::Metadata { resource: MetadataResource::Members }),
            client.request(&window, NativeRequest::Metadata { resource: MetadataResource::ReadState }),
        );
        let channels = client.commit(&window, channels?)?;
        let members = client.commit(&window, members?)?;
        let read_state = client.commit(&window, read_state?)?;
        let values = members.body.as_array().ok_or("invalid members projection")?;
        assert_eq!(values.len(), 2, "actual seeded PG fixture has two active members");
        assert!(values.iter().all(|m| m.get("presence").is_none()));
        assert!(values.iter().all(|m| m["locale"] == ""));
        if let Ok(output) = std::env::var("MNEMA_NATIVE_METADATA_RECEIPT") {
            std::fs::write(output, serde_json::to_vec_pretty(&json!({
                "schema":1,"qualification":"actual-PG18-CA-verified-HTTPS-three-parallel-native-requests",
                "statuses":[channels.status,members.status,read_state.status],
                "channels":channels.body,"members":members.body,"read_state":read_state.body,
                "credentials": "none", "protected_content": "unavailable"
            }))?)?;
        }
        let logout = client.request(&window, NativeRequest::Logout).await?;
        client.commit(&window, logout)?;
        println!("PASS: actual PG/TLS three concurrent native metadata requests; members=2; absent presence preserved; logout committed");
        Ok::<(), Box<dyn std::error::Error>>(())
    })
}
