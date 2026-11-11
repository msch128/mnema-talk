-- Which key a recovery code's hash is under: the fingerprint of the key that
-- was current when the code was made (not secret, see internal/auth). A hash
-- cannot be moved to a new key, so a code stops verifying when its key is
-- retired; with the fingerprint such codes are no longer counted as remaining.
-- NULL is a code from before this column: its key is not known.
ALTER TABLE recovery_codes ADD COLUMN key_fingerprint BIGINT;
