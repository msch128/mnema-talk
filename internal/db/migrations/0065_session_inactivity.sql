-- Days without an authenticated request after which a session is signed out
-- (the Owner and Administrators change it; 1 to 365, default 7). Inactivity,
-- not age: the session lifetime (SESSION_EXPIRY_HOURS) still caps the total.
ALTER TABLE instance_settings
    ADD COLUMN session_inactivity_days INT NOT NULL DEFAULT 7
    CHECK (session_inactivity_days BETWEEN 1 AND 365);
