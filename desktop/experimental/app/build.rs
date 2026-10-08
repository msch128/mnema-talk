fn main() {
    #[cfg(feature = "shell")]
    {
        #[cfg(not(any(feature = "synthetic-media-fixture", feature = "native-crypto")))]
        const COMMANDS: &[&str] = &[
            "native_context",
            "native_connect",
            "native_metadata_request",
            "native_personal_metadata_request",
            "native_public_metadata_request",
            "native_auth_login",
            "native_auth_password",
            "native_auth_register",
            "native_admin_request",
            "native_auth_me",
            "native_auth_refresh",
            "native_auth_logout",
            "native_disconnect",
            "native_request_cancel",
            "native_socket_open",
            "native_socket_send",
            "native_socket_close",
        ];
        #[cfg(feature = "native-crypto")]
        const COMMANDS: &[&str] = &[
            "native_context",
            "native_connect",
            "native_metadata_request",
            "native_personal_metadata_request",
            "native_public_metadata_request",
            "native_auth_login",
            "native_auth_password",
            "native_auth_register",
            "native_admin_request",
            "native_auth_me",
            "native_auth_refresh",
            "native_auth_logout",
            "native_disconnect",
            "native_request_cancel",
            "native_socket_open",
            "native_socket_send",
            "native_socket_close",
            "native_trust_begin_first_root",
            "native_trust_request_confirmation",
            "native_trust_cancel",
            "native_trust_read_status",
            "native_chat_publish",
            "native_chat_receive",
            "native_chat_mutate",
            "native_chat_snapshot",
        ];
        #[cfg(feature = "synthetic-media-fixture")]
        const COMMANDS: &[&str] = &[
            "native_context",
            "native_connect",
            "native_metadata_request",
            "native_personal_metadata_request",
            "native_public_metadata_request",
            "native_auth_login",
            "native_auth_password",
            "native_auth_register",
            "native_admin_request",
            "native_auth_me",
            "native_auth_refresh",
            "native_auth_logout",
            "native_disconnect",
            "native_request_cancel",
            "native_socket_open",
            "native_socket_send",
            "native_socket_close",
            "native_fixture_media_open",
            "native_fixture_media_send",
            "native_fixture_media_close",
        ];
        tauri_build::try_build(
            tauri_build::Attributes::new()
                .app_manifest(tauri_build::AppManifest::new().commands(COMMANDS)),
        )
        .expect("native preview capabilities could not be validated");
    }
}
