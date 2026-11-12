-- Nothing new may name an erased account (H-L10-05).
--
-- The erasure (internal/auth/account/account_erase.go) holds the person's
-- users row FOR UPDATE while it removes what refers to them. A write of theirs
-- that was authenticated before the erasure committed waits for that lock in
-- its foreign key check (or in its update of the row) and used to go through
-- afterwards: the foreign key still finds the tombstone, which keeps its id.
-- The guards below make the database refuse such late rows, for every table
-- at once, instead of each route checking the author again.
--
-- erased_person_guard runs before a row is written that names a person in one
-- of the columns given as arguments ("column:mode"). It locks the named users
-- row FOR KEY SHARE, the lock the foreign key check takes anyway, so it waits
-- for an erasure in progress and then sees its outcome; a write that took the
-- lock first makes the erasure wait until it has committed, and the erasure
-- then removes the row as usual. On an erased account the mode decides:
--   refuse: the write fails (SQLSTATE 23514): the row is the person's own or
--           records an act of theirs;
--   drop:   the row is not written: someone else's row about the person that
--           the erasure deletes (a mention, a follow, a block, a membership);
--   clear:  the column is written NULL: someone else's row that only names
--           the person, as the erasure clears it.
-- An update checks a column only when its value changes, so rows that keep
-- naming a tombstone by design (its messages) stay writable.
CREATE FUNCTION erased_person_guard() RETURNS trigger AS $$
DECLARE
    spec text;
    col  text;
    v    uuid;
    gone timestamptz;
    fresh jsonb := to_jsonb(NEW);
    prior jsonb := CASE WHEN TG_OP = 'UPDATE' THEN to_jsonb(OLD) END;
BEGIN
    FOREACH spec IN ARRAY TG_ARGV LOOP
        col := split_part(spec, ':', 1);
        v := (fresh ->> col)::uuid;
        CONTINUE WHEN v IS NULL OR v IS NOT DISTINCT FROM (prior ->> col)::uuid;
        SELECT deleted_at INTO gone FROM users WHERE id = v FOR KEY SHARE;
        CONTINUE WHEN gone IS NULL;
        CASE split_part(spec, ':', 2)
            WHEN 'drop' THEN
                RETURN NULL;
            WHEN 'clear' THEN
                NEW := jsonb_populate_record(NEW, jsonb_build_object(col, NULL));
            ELSE
                RAISE EXCEPTION 'the account named in %.% was deleted', TG_TABLE_NAME, col
                    USING ERRCODE = '23514';
        END CASE;
    END LOOP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- One trigger per table over every foreign key to users. The default is
-- refuse; the listed columns are rows of other members about the person. The
-- columns of bot accounts are left out: a bot is never a person, and the
-- erasure refuses bot accounts. A new foreign key to users needs its own
-- CREATE TRIGGER in its migration (the schema test fails without one).
DO $$
DECLARE
    t record;
BEGIN
    FOR t IN
        SELECT k.conrelid::regclass AS tbl,
               string_agg(quote_ident(a.attname), ', ' ORDER BY a.attname) AS cols,
               string_agg(quote_literal(a.attname || ':' || COALESCE(m.mode, 'refuse')), ', ' ORDER BY a.attname) AS args
        FROM pg_constraint k
        JOIN pg_attribute a ON a.attrelid = k.conrelid AND a.attnum = k.conkey[1]
        LEFT JOIN (VALUES
            ('message_mentions', 'user_id', 'drop'),
            ('thread_follows', 'user_id', 'drop'),
            ('user_blocks', 'blocked_id', 'drop'),
            ('dm_members', 'user_id', 'drop'),
            ('user_roles', 'user_id', 'drop'),
            ('permission_overwrites', 'user_id', 'drop'),
            ('message_reports', 'reported_user_id', 'clear'),
            ('channels', 'leader_id', 'clear')
        ) AS m(tbl, col, mode) ON m.tbl = k.conrelid::regclass::text AND m.col = a.attname
        WHERE k.contype = 'f' AND k.confrelid = 'users'::regclass
          AND (k.conrelid::regclass::text, a.attname) NOT IN (('bots', 'user_id'), ('webhooks', 'bot_user_id'))
        GROUP BY k.conrelid
    LOOP
        EXECUTE format('CREATE TRIGGER erased_person_guard BEFORE INSERT OR UPDATE OF %s ON %s
            FOR EACH ROW EXECUTE FUNCTION erased_person_guard(%s)', t.cols, t.tbl, t.args);
    END LOOP;
END;
$$;

-- The rows the erasure keeps and empties stay empty. A deleted member's
-- message cannot get text, a title, link cards or an author name again (an
-- edit that waited for the erasure's lock on the message). No lock here: the
-- statement that writes the row has waited for the erasure's lock on that
-- row already, and an edit that came first is emptied by the erasure.
CREATE FUNCTION erased_message_stays_empty() RETURNS trigger AS $$
BEGIN
    IF (NEW.content <> '' OR NEW.title IS NOT NULL OR NEW.embeds <> '[]'::jsonb OR NEW.author_name IS NOT NULL)
       AND (NEW.content, NEW.title, NEW.embeds, NEW.author_name) IS DISTINCT FROM (OLD.content, OLD.title, OLD.embeds, OLD.author_name)
       AND EXISTS (SELECT 1 FROM users WHERE id = NEW.user_id AND deleted_at IS NOT NULL) THEN
        RAISE EXCEPTION 'a deleted member''s message cannot be written again' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER erased_message_stays_empty BEFORE UPDATE OF content, title, embeds, author_name ON messages
    FOR EACH ROW EXECUTE FUNCTION erased_message_stays_empty();

-- A deleted account is final (see 0043, 0057), and now also keeps the
-- profile the erasure gave it: no display name, bio, status, locale,
-- onboarding answers or rules acceptance of the person's can be written back
-- (a profile update that waited for the erasure's lock on the row).
CREATE OR REPLACE FUNCTION users_deleted_is_final() RETURNS trigger AS $$
BEGIN
    IF OLD.deleted_at IS NOT NULL AND (
           NEW.deleted_at IS DISTINCT FROM OLD.deleted_at
        OR NEW.disabled_at IS NULL
        OR NEW.password_hash IS DISTINCT FROM OLD.password_hash
        OR NEW.username IS DISTINCT FROM OLD.username
        OR NEW.role IS DISTINCT FROM OLD.role
        OR NEW.email IS NOT NULL
        OR NEW.avatar_s3_key IS NOT NULL
        OR NEW.banner_s3_key IS NOT NULL
        OR NEW.banned_at IS NOT NULL
        OR NEW.display_name IS DISTINCT FROM OLD.display_name
        OR NEW.bio IS DISTINCT FROM OLD.bio
        OR NEW.status_text IS DISTINCT FROM OLD.status_text
        OR NEW.status_expires_at IS DISTINCT FROM OLD.status_expires_at
        OR NEW.locale IS DISTINCT FROM OLD.locale
        OR NEW.onboarding IS DISTINCT FROM OLD.onboarding
        OR NEW.rules_accepted_version IS DISTINCT FROM OLD.rules_accepted_version) THEN
        RAISE EXCEPTION 'a deleted account cannot be changed' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- The audit log has no foreign key to users (an entry outlives the account).
-- An entry written about an erased account, or by one, is stored the way the
-- erasure scrubs it: without names and details. It waits for an erasure in
-- progress like erased_person_guard.
CREATE FUNCTION audit_log_erased_person() RETURNS trigger AS $$
DECLARE
    actor_gone  boolean := FALSE;
    target_gone boolean := FALSE;
BEGIN
    IF NEW.actor_id IS NOT NULL THEN
        SELECT deleted_at IS NOT NULL INTO actor_gone FROM users WHERE id = NEW.actor_id FOR KEY SHARE;
    END IF;
    IF NEW.target_type = 'user' AND NEW.target_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        SELECT deleted_at IS NOT NULL INTO target_gone FROM users WHERE id = NEW.target_id::uuid FOR KEY SHARE;
    END IF;
    IF COALESCE(actor_gone, FALSE) THEN
        NEW.actor_name := '';
    END IF;
    IF COALESCE(target_gone, FALSE) THEN
        NEW.target_name := '';
    END IF;
    IF COALESCE(actor_gone, FALSE) OR COALESCE(target_gone, FALSE) THEN
        NEW.details := '{}'::jsonb;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_log_erased_person BEFORE INSERT ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_erased_person();
