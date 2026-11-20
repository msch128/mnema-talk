-- Display names are cleaned like bot and webhook names from now on
-- (httpx.CleanName): characters that render differently from what they
-- spell, such as direction overrides and zero-width spaces, are dropped when a
-- name is set. This applies the same to the names stored before. A name with
-- nothing left falls back to the username, as an empty name does when it is
-- set. ZWJ and ZWNJ stay: scripts and emoji sequences need them.
UPDATE users
SET display_name = COALESCE(NULLIF(btrim(regexp_replace(display_name,
        '[­؜᠎​‎‏‪-‮⁠-⁤⁦-⁯﻿]', '', 'g')), ''),
    LEFT(username, 24))
WHERE display_name ~ '[­؜᠎​‎‏‪-‮⁠-⁤⁦-⁯﻿]';
