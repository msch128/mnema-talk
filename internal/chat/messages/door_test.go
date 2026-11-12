package messages

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Messages have one door: every message row is written by CreateMessage, which
// only insertMessageTx calls, behind insertMessage (automod, then the post gate
// in the transaction). The exported InsertMessage leaves the permission check
// and the send rules (mention policy, new-member wait, slow-mode exemption) to
// its caller, through the request context (rulesFrom). This test pins every
// caller with where its rules come from: a new caller fails it until it is
// reviewed and added here. (2026-11 defect hunt, lane 3, pair 6.)
var insertMessageCallers = map[string]string{
	"internal/bot/interactions_respond.go:respond":           "botapi.SendContext and an explicit mention policy",
	"internal/chat/dm/dm_calls.go:recordMissed":              "none: a missed-call record without text, written as the caller; the DM gate still runs",
	"internal/chat/forum/forum_posts.go:CreateForumPost":     "ServeSendContext in handler_forum.go",
	"internal/chat/messages/forward.go:forwardMessage":       "ServeSendContext",
	"internal/chat/talks/talks_invite.go:postTalkInvite":     "ServeSendContext through dmSendContext",
	"internal/media/handler_upload.go:insertUploadMessage":   "UploadContext",
	"internal/poll/service.go:Create":                        "SendContext in handler.go and command.go",
	"internal/webhook/handler_store.go:insertMessage":        "WithMentionPolicy in execute.go; webhooks skip automod and slow mode by design",
	"internal/chat/messages/service.go:Post":                 "Service.Post: decides and passes the rules as a value",
	"internal/chat/messages/post.go:InsertMessage":           "the exported door itself (rulesFrom)",
	"internal/chat/messages/post.go:insertMessageTx(Create)": "the only caller of CreateMessage",
}

type site struct{ file, fn, callee string }

func callSites(t *testing.T) []site {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	var out []site
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					if name := calleeName(x.Fun); name == "InsertMessage" || name == "insertMessage" || name == "CreateMessage" {
						out = append(out, site{filepath.ToSlash(rel), fn.Name.Name, name})
					}
				case *ast.BasicLit:
					if s, err := strconv.Unquote(x.Value); err == nil && strings.Contains(strings.ToUpper(s), "INSERT INTO MESSAGES ") {
						out = append(out, site{filepath.ToSlash(rel), fn.Name.Name, "SQL"})
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func calleeName(e ast.Expr) string {
	switch f := e.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if f.Sel.Name == "insertMessage" {
			return "" // a method of the webhook store, listed by its own caller below
		}
		return f.Sel.Name
	}
	return ""
}

func TestEveryMessageGoesThroughTheOneDoor(t *testing.T) {
	var got []string
	for _, s := range callSites(t) {
		switch {
		case s.callee == "SQL":
			// chattest seeds rows for tests only; no production package imports it.
			if (s.file != "internal/chat/messages/messages.go" || s.fn != "CreateMessage") && !strings.HasPrefix(s.file, "internal/chat/chattest/") {
				t.Errorf("%s:%s writes message rows itself, around CreateMessage", s.file, s.fn)
			}
		case s.callee == "CreateMessage":
			got = append(got, s.file+":"+s.fn+"(Create)")
		case s.callee == "insertMessage" && s.file == "internal/chat/messages/service.go":
			got = append(got, s.file+":"+s.fn)
		case s.callee == "insertMessage" && s.file == "internal/chat/messages/post.go":
			got = append(got, s.file+":"+s.fn)
		case s.callee == "InsertMessage":
			got = append(got, s.file+":"+s.fn)
		}
	}
	sort.Strings(got)
	var want []string
	for k := range insertMessageCallers {
		want = append(want, k)
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the callers of the message door changed; review where each gets its send rules and update insertMessageCallers.\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
