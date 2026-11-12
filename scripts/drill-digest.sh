#!/usr/bin/env bash
# Sourced by backup-drill.sh (not executed): a digest of the whole database,
# one line per table with its row count and an MD5 over every row as text in
# a fixed order. The drill takes it while the app is stopped right before the
# backup and again after the restore, before the app starts: a restore must
# bring back every row, not only the seeded message the drill looks at.

# One SELECT per application table, run by psql's \gexec. pg_dump and a
# restore keep the text form of every value, so equal data gives equal lines.
DRILL_DIGEST_SQL="SELECT format('SELECT %L || '' '' || count(*) || '' '' || COALESCE(md5(string_agg(t::text, chr(10) ORDER BY t::text)), ''-'') FROM %I t', c.relname, c.relname)
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind = 'r' ORDER BY c.relname
\\gexec"

# drill_psql runs the SQL on stdin in the stack's database.
drill_psql() {
  docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -q -v ON_ERROR_STOP=1'
}

# db_digest prints "table rows md5" for every table. It refuses an empty
# result, so a broken query cannot make two digests equal.
db_digest() {
  local out
  out="$(printf '%s\n' "$DRILL_DIGEST_SQL" | drill_psql)" || return 1
  [ "$(grep -c . <<<"$out")" -ge 20 ] || { echo "database digest has too few tables: $out" >&2; return 1; }
  printf '%s\n' "$out"
}

# compare_digests BEFORE AFTER: fails and names the tables that differ.
compare_digests() {
  if [ "$1" = "$2" ]; then
    echo "database digest equal ($(grep -c . <<<"$1") tables)"
    return 0
  fi
  echo "the restored database differs from the backed-up one:" >&2
  diff <(printf '%s\n' "$1") <(printf '%s\n' "$2") >&2 || true
  return 1
}
