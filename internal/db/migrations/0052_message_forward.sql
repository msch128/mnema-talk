-- Forwarding (1.1): a forwarded message stores a reference to the root of what
-- was forwarded and the forwarder's own comment (the content), never a copy.
-- The quote is read from the source when a member asks for it, so it follows
-- edits and deletes and is shown only to members who may see the source.
-- forward_of becomes NULL when the source is deleted; forwarded stays TRUE so
-- the message keeps showing as a forward whose original is gone.
ALTER TABLE messages ADD COLUMN forward_of UUID REFERENCES messages(id) ON DELETE SET NULL;
ALTER TABLE messages ADD COLUMN forwarded BOOLEAN NOT NULL DEFAULT FALSE;

-- Deleting a source looks its forwards up.
CREATE INDEX messages_forward_of_idx ON messages (forward_of) WHERE forward_of IS NOT NULL;
