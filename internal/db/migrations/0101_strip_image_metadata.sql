-- Migration 0101: metadata of picture attachments.
--
-- A JPEG, PNG or WebP sent as an attachment is stored without its metadata
-- (EXIF with the GPS position, XMP, IPTC, comments); the picture itself is not
-- re-encoded. This is the Owner's switch for it, on by default. Files stored
-- before stay as they are.
ALTER TABLE instance_settings ADD COLUMN strip_image_metadata BOOLEAN NOT NULL DEFAULT TRUE;
