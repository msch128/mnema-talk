package server

import (
	"regexp"
	"strings"
	"testing"
)

// The matcher of the erasure oracle (personal_schema_integration_test.go): a
// statement of the erase plan covers one column about a person only when it
// reaches the rows through that column or rewrites it. A DELETE covers it when
// its WHERE compares the column with a parameter (the person's id or name); an
// UPDATE when its SET assigns the column or its WHERE compares it. String
// literals (JSON keys, LIKE patterns) and subqueries (which compare other
// tables' columns) are taken out first, so a name that only occurs there
// covers nothing.

var (
	sqlLiteral  = regexp.MustCompile(`'(?:[^']|'')*'`)
	deleteShape = regexp.MustCompile(`(?is)^\s*DELETE\s+FROM\s+([a-z_]+)\s+WHERE\s+(.*?)(?:\s+RETURNING\s.*)?$`)
	updateShape = regexp.MustCompile(`(?is)^\s*UPDATE\s+([a-z_]+)\s+SET\s+(.*?)\s+WHERE\s+(.*?)(?:\s+RETURNING\s.*)?$`)
)

// withoutSubqueries replaces every parenthesised SELECT by "()".
func withoutSubqueries(sql string) string {
	var b strings.Builder
	for i := 0; i < len(sql); i++ {
		if sql[i] == '(' && strings.HasPrefix(strings.ToUpper(strings.TrimLeft(sql[i+1:], " \t\n(")), "SELECT") {
			depth := 0
			for ; i < len(sql); i++ {
				if sql[i] == '(' {
					depth++
				} else if sql[i] == ')' {
					if depth--; depth == 0 {
						break
					}
				}
			}
			b.WriteString("()")
			continue
		}
		b.WriteByte(sql[i])
	}
	return b.String()
}

// assigned lists the columns a SET clause writes (top-level assignments).
func assigned(set string) []string {
	var cols []string
	depth, start := 0, 0
	for i := 0; i <= len(set); i++ {
		if i < len(set) && set[i] == '(' {
			depth++
		} else if i < len(set) && set[i] == ')' {
			depth--
		} else if i == len(set) || (set[i] == ',' && depth == 0) {
			if f := strings.Fields(strings.SplitN(set[start:i], "=", 2)[0]); len(f) == 1 {
				cols = append(cols, strings.ToLower(f[0]))
			}
			start = i + 1
		}
	}
	return cols
}

// compares reports whether where compares column (bare or qualified, also
// inside a function such as LOWER) with a parameter.
func compares(where, column string) bool {
	return regexp.MustCompile(`(?i)(^|[^a-z0-9_])` + regexp.QuoteMeta(column) + `\)?\s*(=|\bIN\b)\s*\(?\s*(?:[a-z_]+\()?\$\d`).MatchString(where)
}

// eraseDeletes reports whether a DELETE of plan reaches the rows of table
// through column.
func eraseDeletes(plan []string, table, column string) bool {
	for _, sql := range plan {
		s := withoutSubqueries(sqlLiteral.ReplaceAllString(sql, "''"))
		if m := deleteShape.FindStringSubmatch(s); m != nil && strings.EqualFold(m[1], table) && compares(m[2], column) {
			return true
		}
	}
	return false
}

// eraseCovers reports whether a statement of plan covers table.column.
func eraseCovers(plan []string, table, column string) bool {
	if eraseDeletes(plan, table, column) {
		return true
	}
	for _, sql := range plan {
		s := withoutSubqueries(sqlLiteral.ReplaceAllString(sql, "''"))
		if m := updateShape.FindStringSubmatch(s); m != nil && strings.EqualFold(m[1], table) {
			for _, c := range assigned(m[2]) {
				if c == column {
					return true
				}
			}
			if compares(m[3], column) {
				return true
			}
		}
	}
	return false
}

func TestTheErasureMatcherNeedsTheColumnItself(t *testing.T) {
	cases := []struct {
		sql, table, column string
		want               bool
	}{
		{`DELETE FROM message_nonces WHERE user_id = $1`, "message_nonces", "user_id", true},
		// A second person column on a table the plan deletes from by another column.
		{`DELETE FROM message_nonces WHERE user_id = $1`, "message_nonces", "reviewer_id", false},
		{`DELETE FROM user_blocks WHERE blocker_id = $1 OR blocked_id = $1`, "user_blocks", "blocked_id", true},
		{`DELETE FROM login_unlock_requests WHERE LOWER(username) = LOWER($1)`, "login_unlock_requests", "username", true},
		{`DELETE FROM media WHERE uploader_id = $1 AND NOT EXISTS (SELECT 1 FROM wiki_images w WHERE w.media_id = media.id) RETURNING s3_key`, "media", "uploader_id", true},
		// The column is compared only inside a subquery about another table.
		{`DELETE FROM polls WHERE message_id IN (SELECT id FROM messages WHERE user_id = $1)`, "polls", "user_id", false},
		{`UPDATE wiki_pages SET deleted_by = NULL WHERE deleted_by = $1`, "wiki_pages", "deleted_by", true},
		{`UPDATE messages SET content = '', author_name = NULL WHERE user_id = $1`, "messages", "author_name", true},
		{`UPDATE audit_log SET actor_name = CASE WHEN actor_id = $1 THEN '' ELSE actor_name END WHERE actor_id = $1 OR (target_type = 'user' AND target_id = $1::text)`, "audit_log", "target_id", true},
		// A name that only occurs as a JSON key in a literal.
		{`UPDATE message_reports SET snapshot = jsonb_build_object('message', snapshot->'message'->>'user_id') WHERE snapshot::text LIKE '%' || $1::text || '%'`, "message_reports", "user_id", false},
		// A longer column name that ends in the same word.
		{`UPDATE message_reports SET reported_user_id = NULL WHERE reported_user_id = $1`, "message_reports", "user_id", false},
		// The column in a CASE inside another column's assignment is not written.
		{`UPDATE t SET name = CASE WHEN owner_id = $1 THEN '' ELSE name END WHERE flag`, "t", "owner_id", false},
		{`DELETE FROM other WHERE user_id = $1`, "message_nonces", "user_id", false},
	}
	for _, c := range cases {
		if got := eraseCovers([]string{c.sql}, c.table, c.column); got != c.want {
			t.Errorf("%s.%s by %q: got %v, want %v", c.table, c.column, c.sql, got, c.want)
		}
	}
}
