-- 1.2: anonymous polls that keep no link between a member and a choice, and
-- per-channel auto-delete.
--
-- Anonymous polls. Until 1.1 an anonymous poll stored (option, member) rows
-- like any poll and only hid them in the API. From now on an anonymous poll
-- stores two unlinked facts: THAT a member voted (poll_voters: it prevents a
-- second vote) and HOW MANY votes each option has (poll_options.anon_votes).
-- Nothing connects the two, and poll_voters has no timestamp, so insert order
-- or time cannot be matched against the counters either.
ALTER TABLE poll_options ADD COLUMN anon_votes INT NOT NULL DEFAULT 0 CHECK (anon_votes >= 0);
ALTER TABLE polls ADD COLUMN anon_voters INT NOT NULL DEFAULT 0 CHECK (anon_voters >= 0);

CREATE TABLE poll_voters (
    poll_id UUID NOT NULL REFERENCES polls(message_id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (poll_id, user_id)
);
CREATE INDEX poll_voters_user ON poll_voters (user_id);

-- Convert the anonymous polls that exist: keep the tallies and who took part,
-- drop who chose what. Members can no longer change a vote in them.
UPDATE poll_options o
   SET anon_votes = (SELECT COUNT(*) FROM poll_votes v WHERE v.option_id = o.id)
 WHERE o.poll_id IN (SELECT message_id FROM polls WHERE anonymous);

INSERT INTO poll_voters (poll_id, user_id)
SELECT DISTINCT o.poll_id, v.user_id
  FROM poll_votes v
  JOIN poll_options o ON o.id = v.option_id
  JOIN polls p ON p.message_id = o.poll_id
 WHERE p.anonymous;

UPDATE polls p
   SET anon_voters = (SELECT COUNT(*) FROM poll_voters w WHERE w.poll_id = p.message_id)
 WHERE p.anonymous;

DELETE FROM poll_votes
 WHERE option_id IN (SELECT o.id FROM poll_options o JOIN polls p ON p.message_id = o.poll_id WHERE p.anonymous);

-- An anonymous poll never holds named votes.
CREATE FUNCTION poll_votes_not_anonymous() RETURNS trigger AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM poll_options o JOIN polls p ON p.message_id = o.poll_id
               WHERE o.id = NEW.option_id AND p.anonymous) THEN
        RAISE EXCEPTION 'anonymous polls keep no named votes';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER poll_votes_not_anonymous BEFORE INSERT OR UPDATE ON poll_votes
    FOR EACH ROW EXECUTE FUNCTION poll_votes_not_anonymous();

-- Per-channel auto-delete: messages older than this many days are removed by
-- a sweeper. 0 = off (the default; always an explicit opt-in). Direct
-- messages never take part.
ALTER TABLE channels ADD COLUMN auto_delete_days INT NOT NULL DEFAULT 0
    CHECK (auto_delete_days IN (0, 1, 7, 30, 90));
ALTER TABLE channels ADD CONSTRAINT channels_auto_delete_not_dm CHECK (auto_delete_days = 0 OR type <> 'dm');
