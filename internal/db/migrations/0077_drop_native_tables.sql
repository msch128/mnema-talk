-- The native-client session and ciphertext tables of migrations 0015 and 0016
-- belonged to a native client that was never mounted in a release. Nothing
-- writes them any more; the ciphertext relay contradicted the decision that
-- chat text is not end-to-end encrypted. Children first.
DROP TABLE IF EXISTS native_ciphertext_events;
DROP TABLE IF EXISTS native_refresh_tokens;
DROP TABLE IF EXISTS native_access_tokens;
DROP TABLE IF EXISTS native_session_families;
