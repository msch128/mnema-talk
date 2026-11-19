//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mnema-xyz/talk/internal/bot"
	"github.com/mnema-xyz/talk/internal/chat/core"
	"github.com/mnema-xyz/talk/internal/perm"
)

// helper to upload a file to a channel and return the created message.
func uploadFile(t *testing.T, f *botFixture, ch uuid.UUID, filename, ctype string, data []byte) core.Message {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("content", "message with attachment")
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	h.Set("Content-Type", ctype)
	part, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, f.a.srv.URL+botPath(ch, "/upload"), &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bot "+f.token.Secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	resp := response{status: res.StatusCode, body: raw}
	wantStatus(t, "upload", resp, http.StatusCreated)
	var m core.Message
	resp.decode(t, &m)
	return m
}

func TestBotDownloadAttachment_HappyPath(t *testing.T) {
	f := newBotFixture(t, false)
	msg := uploadFile(t, f, f.open, "test.png", "image/png", pngBytes)
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	att := msg.Attachments[0]

	// Verify attachment metadata fields on Message
	if att.Filename != "test.png" || att.ContentType != "image/png" || att.Size == nil || *att.Size != int64(len(pngBytes)) {
		t.Fatalf("attachment metadata mismatch: %+v", att)
	}

	// Download via bot endpoint
	res := f.client.get(bot.BasePath + "/media/" + att.ID.String())
	wantStatus(t, "download attachment", res, http.StatusOK)

	if !bytes.Equal(res.body, pngBytes) {
		t.Fatalf("downloaded body does not match uploaded bytes")
	}
	if res.header.Get("Content-Type") != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", res.header.Get("Content-Type"))
	}
	cd := res.header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, "filename=") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if res.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", res.header.Get("X-Content-Type-Options"))
	}
	if res.header.Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
		t.Errorf("Content-Security-Policy = %q, want sandbox", res.header.Get("Content-Security-Policy"))
	}
	if res.header.Get("Cache-Control") != "private, no-cache" {
		t.Errorf("Cache-Control = %q, want private, no-cache", res.header.Get("Cache-Control"))
	}
	etag := res.header.Get("ETag")
	if etag == "" {
		t.Errorf("ETag is empty")
	}

	// Revalidation with If-None-Match
	reval := f.client.req(http.MethodGet, bot.BasePath+"/media/"+att.ID.String(), nil, map[string]string{"If-None-Match": etag})
	wantStatus(t, "revalidation", reval, http.StatusNotModified)

	// Range request
	rng := f.client.req(http.MethodGet, bot.BasePath+"/media/"+att.ID.String(), nil, map[string]string{"Range": "bytes=0-3"})
	wantStatus(t, "range", rng, http.StatusPartialContent)
	if !bytes.Equal(rng.body, pngBytes[:4]) {
		t.Errorf("range body mismatch: got %v, want %v", rng.body, pngBytes[:4])
	}
}

func TestBotDownloadAttachment_WithoutView(t *testing.T) {
	f := newBotFixture(t, false)
	// Upload to f.open by owner
	res := f.owner.upload("/api/channels/"+f.open.String()+"/upload", "file", "secret.png", "image/png", pngBytes, nil)
	wantStatus(t, "owner upload", res, http.StatusCreated)
	var m core.Message
	res.decode(t, &m)
	attID := m.Attachments[0].ID

	// Revoke ViewChannel from the bot on f.open
	wantStatus(t, "revoke view", putOverwrites(f.owner, "channels", f.open,
		overwrite("role", f.roleID, 0, perm.ViewChannel)), http.StatusNoContent)

	dl := f.client.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "download without view", dl, http.StatusNotFound)

	// Range (206) and 304 probes on unauthorized media must stay 404
	reval := f.client.req(http.MethodGet, bot.BasePath+"/media/"+attID.String(), nil, map[string]string{"If-None-Match": `"` + attID.String() + `"`})
	wantStatus(t, "reval without view", reval, http.StatusNotFound)
	rng := f.client.req(http.MethodGet, bot.BasePath+"/media/"+attID.String(), nil, map[string]string{"Range": "bytes=0-3"})
	wantStatus(t, "range without view", rng, http.StatusNotFound)
}

func TestBotDownloadAttachment_AccessAllPrivateChannel(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	staff := a.createChannel(owner, "staff", "text")

	// Hide staff from @everyone
	ev := everyoneID(t, owner)
	wantStatus(t, "staff private", putOverwrites(owner, "channels", staff,
		overwrite("role", ev, 0, perm.ViewChannel)), http.StatusNoContent)

	// Install bot with access = "all"
	out := installAll(t, owner, "AllBot", allKeys)
	bc := &botClient{t: t, a: a, token: out.Token.Secret, user: out.Bot.UserID}

	// Upload file to staff channel by owner
	res := owner.upload("/api/channels/"+staff.String()+"/upload", "file", "staff.png", "image/png", pngBytes, nil)
	wantStatus(t, "owner upload to staff", res, http.StatusCreated)
	var m core.Message
	res.decode(t, &m)
	attID := m.Attachments[0].ID

	// Bot with access_all cannot see private channel attachment
	dl := bc.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "download staff attachment", dl, http.StatusNotFound)

	// But in open channel, bot with access_all can download
	resOpen := owner.upload("/api/channels/"+open.String()+"/upload", "file", "open.png", "image/png", pngBytes, nil)
	wantStatus(t, "owner upload to open", resOpen, http.StatusCreated)
	var mOpen core.Message
	resOpen.decode(t, &mOpen)
	dlOpen := bc.get(bot.BasePath + "/media/" + mOpen.Attachments[0].ID.String())
	wantStatus(t, "download open attachment", dlOpen, http.StatusOK)
}

func TestBotDownloadAttachment_DM(t *testing.T) {
	f := newBotFixture(t, false)
	alice := f.a.register(f.owner, "alice")
	bob := f.a.register(f.owner, "bob")

	// DM between alice and bob (bot is NOT participant)
	dmRes := alice.post("/api/dms", map[string]any{"user_ids": []uuid.UUID{bob.user.ID}})
	wantStatus(t, "create dm", dmRes, http.StatusCreated)
	var dm struct {
		ID uuid.UUID `json:"id"`
	}
	dmRes.decode(t, &dm)

	// Alice uploads file to DM
	upRes := alice.upload("/api/channels/"+dm.ID.String()+"/upload", "file", "dm.png", "image/png", pngBytes, nil)
	wantStatus(t, "upload to dm", upRes, http.StatusCreated)
	var msg core.Message
	upRes.decode(t, &msg)
	attID := msg.Attachments[0].ID

	// Bot is not participant -> 404
	dl := f.client.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "bot not in dm", dl, http.StatusNotFound)

	// When bot is added to dm_members, bot IS participant -> 200 OK
	if _, err := f.a.db.Exec(context.Background(), `INSERT INTO dm_members (channel_id, user_id) VALUES ($1, $2)`, dm.ID, f.bot.UserID); err != nil {
		t.Fatal(err)
	}
	f.a.router.Hub.Perms.Invalidate()

	dlBot := f.client.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "bot in dm", dlBot, http.StatusOK)
	if !bytes.Equal(dlBot.body, pngBytes) {
		t.Fatalf("bot dm attachment body mismatch")
	}
}

func TestBotDownloadAttachment_EndedTimedMessage(t *testing.T) {
	f := newBotFixture(t, false)
	msg := uploadFile(t, f, f.open, "timed.png", "image/png", pngBytes)
	attID := msg.Attachments[0].ID

	// Create a message whose lifetime has ended in the past and point the media to it
	timedMsgID := uuid.New()
	if _, err := f.a.db.Exec(context.Background(), `
		INSERT INTO messages (id, channel_id, user_id, content, created_at, expires_at)
		VALUES ($1, $2, $3, 'timed', NOW() - INTERVAL '2 hours', NOW() - INTERVAL '1 hour')`,
		timedMsgID, f.open, f.bot.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(context.Background(), `UPDATE media SET message_id = $1 WHERE id = $2`, timedMsgID, attID); err != nil {
		t.Fatal(err)
	}

	dl := f.client.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "expired message attachment", dl, http.StatusNotFound)
}

func TestBotDownloadAttachment_DeletedMessage(t *testing.T) {
	f := newBotFixture(t, false)
	msg := uploadFile(t, f, f.open, "deleted.png", "image/png", pngBytes)
	attID := msg.Attachments[0].ID

	// Delete message
	delRes := f.client.delete(botPath(f.open, "/messages/"+msg.ID.String()))
	wantStatus(t, "delete message", delRes, http.StatusNoContent)

	dl := f.client.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "deleted message attachment", dl, http.StatusNotFound)
}

func TestBotDownloadAttachment_ForeignOrInventedID(t *testing.T) {
	f := newBotFixture(t, false)
	dl := f.client.get(bot.BasePath + "/media/" + uuid.NewString())
	wantStatus(t, "invented id", dl, http.StatusNotFound)
}

func TestBotDownloadAttachment_MemberCookieOnBotRoute(t *testing.T) {
	f := newBotFixture(t, false)
	msg := uploadFile(t, f, f.open, "auth.png", "image/png", pngBytes)
	attID := msg.Attachments[0].ID

	// Owner session cookie on bot route -> 401
	res := f.owner.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "cookie on bot route", res, http.StatusUnauthorized)
}

func TestBotDownloadAttachment_BotTokenOnMemberRoute(t *testing.T) {
	f := newBotFixture(t, false)
	msg := uploadFile(t, f, f.open, "auth2.png", "image/png", pngBytes)
	attID := msg.Attachments[0].ID

	// Bot token on member route -> 401
	req, _ := http.NewRequest(http.MethodGet, f.a.srv.URL+"/api/media/"+attID.String(), nil)
	req.Header.Set("Authorization", "Bot "+f.token.Secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bot token on member route status = %d, want 401", res.StatusCode)
	}
}

func TestBotDownloadAttachment_VoiceChannel(t *testing.T) {
	f := newBotFixture(t, false)
	voice := f.a.createChannel(f.owner, "talk-voice", "voice")
	vMsg := f.owner.upload("/api/channels/"+voice.String()+"/upload", "file", "voice.png", "image/png", pngBytes, nil)
	wantStatus(t, "upload to voice", vMsg, http.StatusCreated)
	var vMessage core.Message
	vMsg.decode(t, &vMessage)
	vAttID := vMessage.Attachments[0].ID

	// Bot attempts to download voice channel attachment without View -> 404
	dlVoice := f.client.get(bot.BasePath + "/media/" + vAttID.String())
	wantStatus(t, "voice channel media without view", dlVoice, http.StatusNotFound)

	// Grant bot ViewChannel on voice channel
	wantStatus(t, "grant voice view", putOverwrites(f.owner, "channels", voice,
		overwrite("role", f.roleID, perm.ViewChannel, 0)), http.StatusNoContent)
	f.a.router.Hub.Perms.Invalidate()

	// Bot can download voice channel attachment when it has View -> 200 OK
	dlView := f.client.get(bot.BasePath + "/media/" + vAttID.String())
	wantStatus(t, "voice channel media with view", dlView, http.StatusOK)
	if !bytes.Equal(dlView.body, pngBytes) {
		t.Fatalf("voice attachment body mismatch")
	}
}

func TestBotDownloadAttachment_DMTalk(t *testing.T) {
	f := newBotFixture(t, false)
	alice := f.a.register(f.owner, "alice")
	bob := f.a.register(f.owner, "bob")

	// DM between alice and bob
	dmRes := alice.post("/api/dms", map[string]any{"user_ids": []uuid.UUID{bob.user.ID}})
	wantStatus(t, "create dm", dmRes, http.StatusCreated)
	var dm struct {
		ID uuid.UUID `json:"id"`
	}
	dmRes.decode(t, &dm)

	// Create a DM-call Talk channel for this DM
	callID := uuid.New()
	if _, err := f.a.db.Exec(context.Background(), `
		INSERT INTO channels (id, name, type, is_temp, dm_id)
		VALUES ($1, 'call', 'voice', TRUE, $2)`, callID, dm.ID); err != nil {
		t.Fatal(err)
	}

	// Alice uploads file to DM call channel
	upRes := alice.upload("/api/channels/"+callID.String()+"/upload", "file", "call.png", "image/png", pngBytes, nil)
	wantStatus(t, "upload to dm call", upRes, http.StatusCreated)
	var msg core.Message
	upRes.decode(t, &msg)
	attID := msg.Attachments[0].ID

	// Bot is not participant in DM -> 404
	dl := f.client.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "bot not in dm-call", dl, http.StatusNotFound)

	// When bot is added to dm_members, bot IS participant -> 200 OK
	if _, err := f.a.db.Exec(context.Background(), `INSERT INTO dm_members (channel_id, user_id) VALUES ($1, $2)`, dm.ID, f.bot.UserID); err != nil {
		t.Fatal(err)
	}
	f.a.router.Hub.Perms.Invalidate()

	dlBot := f.client.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "bot in dm-call", dlBot, http.StatusOK)
	if !bytes.Equal(dlBot.body, pngBytes) {
		t.Fatalf("bot dm-call attachment body mismatch")
	}
}

func TestBotDownloadAttachment_MessageIDNil(t *testing.T) {
	f := newBotFixture(t, false)

	// 1. Event-cover shaped row: has channel_id (open channel bot can view), but message_id == nil
	coverID := uuid.New()
	if err := f.a.store.Upload(context.Background(), "covers/cover.png", bytes.NewReader(pngBytes), "image/png", int64(len(pngBytes))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(context.Background(), `
		INSERT INTO media (id, uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, $3, NULL, 'test', 'covers/cover.png', 'cover.png', 'image/png', $4)`,
		coverID, f.bot.UserID, f.open, len(pngBytes)); err != nil {
		t.Fatal(err)
	}
	dlCover := f.client.get(bot.BasePath + "/media/" + coverID.String())
	wantStatus(t, "cover shaped row (messageID==nil)", dlCover, http.StatusNotFound)

	// 2. Avatar shaped row: channel_id == nil, message_id == nil
	avatarID := uuid.New()
	if err := f.a.store.Upload(context.Background(), "avatars/av.png", bytes.NewReader(pngBytes), "image/png", int64(len(pngBytes))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(context.Background(), `
		INSERT INTO media (id, uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, NULL, NULL, 'test', 'avatars/av.png', 'av.png', 'image/png', $3)`,
		avatarID, f.bot.UserID, len(pngBytes)); err != nil {
		t.Fatal(err)
	}
	dlAvatar := f.client.get(bot.BasePath + "/media/" + avatarID.String())
	wantStatus(t, "avatar shaped row (messageID==nil, channelID==nil)", dlAvatar, http.StatusNotFound)

	// 3. Staged upload: channel_id != nil, message_id == nil, staged == true
	stagedID := uuid.New()
	if err := f.a.store.Upload(context.Background(), "staged/st.png", bytes.NewReader(pngBytes), "image/png", int64(len(pngBytes))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(context.Background(), `
		INSERT INTO media (id, uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes, staged)
		VALUES ($1, $2, $3, NULL, 'test', 'staged/st.png', 'st.png', 'image/png', $4, TRUE)`,
		stagedID, f.bot.UserID, f.open, len(pngBytes)); err != nil {
		t.Fatal(err)
	}
	dlStaged := f.client.get(bot.BasePath + "/media/" + stagedID.String())
	wantStatus(t, "staged upload (messageID==nil, staged==true)", dlStaged, http.StatusNotFound)

	// 4. Staged row with message_id: must answer 404 because staged == true
	validMsg := uploadFile(t, f, f.open, "valid.png", "image/png", pngBytes)
	stagedWithMsgID := uuid.New()
	if err := f.a.store.Upload(context.Background(), "staged/st2.png", bytes.NewReader(pngBytes), "image/png", int64(len(pngBytes))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(context.Background(), `
		INSERT INTO media (id, uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes, staged)
		VALUES ($1, $2, $3, $4, 'test', 'staged/st2.png', 'st2.png', 'image/png', $5, TRUE)`,
		stagedWithMsgID, f.bot.UserID, f.open, validMsg.ID, len(pngBytes)); err != nil {
		t.Fatal(err)
	}
	dlStagedWithMsg := f.client.get(bot.BasePath + "/media/" + stagedWithMsgID.String())
	wantStatus(t, "staged upload with message_id (staged==true)", dlStagedWithMsg, http.StatusNotFound)

	// 5. Media with message_id but nil channel_id: must answer 404
	nilChanID := uuid.New()
	if err := f.a.store.Upload(context.Background(), "covers/nc.png", bytes.NewReader(pngBytes), "image/png", int64(len(pngBytes))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(context.Background(), `
		INSERT INTO media (id, uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, NULL, $3, 'test', 'covers/nc.png', 'nc.png', 'image/png', $4)`,
		nilChanID, f.bot.UserID, validMsg.ID, len(pngBytes)); err != nil {
		t.Fatal(err)
	}
	dlNilChan := f.client.get(bot.BasePath + "/media/" + nilChanID.String())
	wantStatus(t, "media with message_id but nil channel_id", dlNilChan, http.StatusNotFound)
}

func TestBotDownloadAttachment_AdminDeleted(t *testing.T) {
	f := newBotFixture(t, false)
	msg := uploadFile(t, f, f.open, "adm.png", "image/png", pngBytes)
	attID := msg.Attachments[0].ID

	// 1. Admin delete media via API
	delRes := f.owner.delete("/api/admin/media/" + attID.String())
	wantStatus(t, "admin delete", delRes, http.StatusNoContent)

	dl := f.client.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "admin-deleted media", dl, http.StatusNotFound)

	// 2. Direct is_deleted = TRUE in database
	msg2 := uploadFile(t, f, f.open, "isdel.png", "image/png", pngBytes)
	attID2 := msg2.Attachments[0].ID
	if _, err := f.a.db.Exec(context.Background(), `UPDATE media SET is_deleted = TRUE WHERE id = $1`, attID2); err != nil {
		t.Fatal(err)
	}
	dl2 := f.client.get(bot.BasePath + "/media/" + attID2.String())
	wantStatus(t, "is_deleted = true media", dl2, http.StatusNotFound)
}

func TestBotDownloadAttachment_BotPayloadURL(t *testing.T) {
	f := newBotFixture(t, false)
	msg := uploadFile(t, f, f.open, "payload.png", "image/png", pngBytes)
	attID := msg.Attachments[0].ID

	// 1. Bot gets message through bot API -> URL is /api/bot/v1/media/{id}
	res := f.client.get(botPath(f.open, "/messages/"+msg.ID.String()))
	wantStatus(t, "bot get message", res, http.StatusOK)
	var botMsg core.Message
	res.decode(t, &botMsg)
	if len(botMsg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(botMsg.Attachments))
	}
	wantURL := bot.BasePath + "/media/" + attID.String()
	if botMsg.Attachments[0].URL != wantURL {
		t.Fatalf("bot message URL = %q, want %q", botMsg.Attachments[0].URL, wantURL)
	}
	if botMsg.Attachments[0].Filename != "payload.png" || botMsg.Attachments[0].ContentType != "image/png" || botMsg.Attachments[0].Size == nil || *botMsg.Attachments[0].Size != int64(len(pngBytes)) {
		t.Fatalf("bot message attachment metadata mismatch: %+v", botMsg.Attachments[0])
	}

	// 2. Member gets message through member API -> URL is /api/media/{id}
	memRes := f.owner.get("/api/channels/" + f.open.String() + "/messages")
	wantStatus(t, "member get message", memRes, http.StatusOK)
	var msgs []core.Message
	memRes.decode(t, &msgs)
	if len(msgs) == 0 || len(msgs[len(msgs)-1].Attachments) != 1 {
		t.Fatalf("expected member message with 1 attachment, got %d messages", len(msgs))
	}
	memAtt := msgs[len(msgs)-1].Attachments[0]
	if memAtt.URL != "/api/media/"+attID.String() {
		t.Fatalf("member message URL = %q, want %q", memAtt.URL, "/api/media/"+attID.String())
	}
}

func TestBotDownloadAttachment_GatewayPayloadURL(t *testing.T) {
	f := gwFixture(t)
	g := f.dialGW("Bot " + f.token.Secret)
	g.identify("", bot.IntentGuildMessages)

	res := f.owner.upload("/api/channels/"+f.open.String()+"/upload", "file", "gw.png", "image/png", pngBytes, nil)
	wantStatus(t, "owner upload", res, http.StatusCreated)
	var ownerMsg core.Message
	res.decode(t, &ownerMsg)
	attID := ownerMsg.Attachments[0].ID

	_, d := g.expect("MESSAGE_CREATE")
	var m core.Message
	if err := json.Unmarshal(d, &m); err != nil {
		t.Fatal(err)
	}
	if m.ID == ownerMsg.ID {
		if len(m.Attachments) != 1 {
			t.Fatalf("expected 1 attachment on gateway, got %d", len(m.Attachments))
		}
		wantURL := bot.BasePath + "/media/" + attID.String()
		if m.Attachments[0].URL != wantURL {
			t.Fatalf("gateway message URL = %q, want %q", m.Attachments[0].URL, wantURL)
		}
	}
}

func TestBotDownloadAttachment_MissingObject(t *testing.T) {
	f := newBotFixture(t, false)
	msg := uploadFile(t, f, f.open, "missing.png", "image/png", pngBytes)
	attID := msg.Attachments[0].ID

	// Delete backing object from storage
	var s3Key string
	if err := f.a.db.QueryRow(context.Background(), `SELECT s3_key FROM media WHERE id = $1`, attID).Scan(&s3Key); err != nil {
		t.Fatal(err)
	}
	if err := f.a.store.Delete(context.Background(), s3Key); err != nil {
		t.Fatal(err)
	}

	// Bot endpoint returns 404 (never 410)
	dl := f.client.get(bot.BasePath + "/media/" + attID.String())
	wantStatus(t, "missing object answers 404 for bot", dl, http.StatusNotFound)

	// Member route answers 410 (Gone)
	memDl := f.owner.get("/api/media/" + attID.String())
	wantStatus(t, "missing object answers 410 for member", memDl, http.StatusGone)
}
