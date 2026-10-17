-- Owner-editable upload limits. MAX_UPLOAD_SIZE_MB stays the operator's hard
-- ceiling and the initial default; everything stored here is capped by it when
-- read, so lowering the env value later never needs a data fix.
--
-- instance_settings is a single row. max_upload_mb NULL = "use the env value".
CREATE TABLE instance_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    max_upload_mb INT CHECK (max_upload_mb IS NULL OR max_upload_mb >= 1),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO instance_settings (singleton) VALUES (TRUE);

-- A role may raise or lower the upload limit of its members; NULL = inherit.
ALTER TABLE roles ADD COLUMN max_upload_mb INT CHECK (max_upload_mb IS NULL OR max_upload_mb >= 1);
