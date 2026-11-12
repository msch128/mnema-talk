//go:build integration

package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestNoCanaryReachesAnObserverOutsideItsClass builds the canary world, then
// asks every GET route of the real router as every observer, with every path
// parameter filled from the world's objects and the documented query
// parameters filled from its words and ids, and records every WebSocket frame
// the observers received. A word or an object id that reaches an observer
// outside its class is a leak; an id counts only when the request did not name
// it. insider is the control: it must find every Staff word somewhere, so a
// clean run is not a sweep that reached nothing.
//
// Smoke (the default) samples each route's parameter combinations; VIS_SWEEP=1
// tries all of them and every word as a search term.
func TestNoCanaryReachesAnObserverOutsideItsClass(t *testing.T) {
	deep := os.Getenv("VIS_SWEEP") == "1"
	seed := uint64(1)
	if s, err := strconv.ParseUint(os.Getenv("VIS_SEED"), 10, 64); err == nil {
		seed = s
	}
	w := newCanaryWorld(t, seed)
	frames := map[string]*frameLog{}
	for name, c := range map[string]*client{"outsider": w.outsider, "carol": w.carol, "mod": w.mod, "admin": w.admin, "owner": w.owner} {
		frames[name] = recordFrames(t, c)
	}
	w.build(t)
	frames["exstaff"] = recordFrames(t, w.exstaff) // after the demotion: what came before was theirs to see
	gatewayLog := w.gatewayFrames(t)
	live := w.say(t, w.owner, clsStaff, w.staffText, "staff live after sockets", map[string]any{})
	if res := w.insider.post("/api/messages/"+live.String()+"/reactions", map[string]string{"emoji": "👀"}); res.status >= 300 {
		t.Fatalf("live reaction: %d %s", res.status, res.body)
	}
	w.say(t, w.insider, clsDM, w.dm, "dm live after sockets", map[string]any{})
	w.say(t, w.owner, clsOpen, w.open, "open live after sockets", map[string]any{})
	botFrames := &frameLog{frames: gatewayLog()}

	s := &canarySweep{w: w, deep: deep, found: map[string]map[string]bool{}, status: map[string]map[int]int{}, known: map[string]bool{}}
	s.requests = s.plan(t)
	observers := map[string]func(string) response{
		"outsider": w.outsider.get, "carol": w.carol.get, "mod": w.mod.get, "exstaff": w.exstaff.get,
		"anon": w.a.anon().get, "insider": w.insider.get, "bot": w.bot.get, "admin": w.admin.get, "owner": w.owner.get,
	}
	names := slices.Sorted(func(yield func(string) bool) {
		for k := range observers {
			if !yield(k) {
				return
			}
		}
	})
	for _, who := range names {
		start := time.Now()
		s.run(t, who, observers[who])
		t.Logf("%s: swept in %v", who, time.Since(start).Round(time.Millisecond))
	}
	time.Sleep(300 * time.Millisecond)
	for who, log := range frames {
		s.frames(t, who, log)
	}
	s.frames(t, "bot", botFrames)
	s.report(t)
}

type canaryRequest struct{ route, path string }

type canarySweep struct {
	w        *canaryWorld
	deep     bool
	requests []canaryRequest
	found    map[string]map[string]bool // observer -> words seen
	status   map[string]map[int]int     // observer -> status -> count
	leaks    []string
	known    map[string]bool // rows of canaryKnownLeaks seen in this run
	twins    int             // hidden-against-missing comparisons made
	sent     int
}

// skipRoutes are GET routes the sweep does not ask: they stream, upgrade or
// measure, and carry no object.
var skipRoutes = map[string]bool{
	"/api/ws": true, "/api/webrtc/speedtest": true, "/api/docs": true, "/api/openapi.json": true,
	"/api/bot/v1/gateway": true, "/api/metrics": true,
}

var routeParam2 = regexp.MustCompile(`\{([^}]+)\}`)

// plan expands every mounted GET route into concrete requests.
func (s *canarySweep) plan(t *testing.T) []canaryRequest {
	t.Helper()
	queries := openAPIQueries(t)
	var out []canaryRequest
	for _, r := range apiRoutes(t, s.w.a) {
		if r.method != http.MethodGet || skipRoutes[r.pattern] {
			continue
		}
		for _, p := range s.expand(r.pattern) {
			out = append(out, canaryRequest{r.pattern, p})
			for _, q := range s.queryVariants(queries[r.pattern]) {
				out = append(out, canaryRequest{r.pattern, p + "?" + q})
			}
		}
	}
	if len(out) < 300 {
		t.Fatalf("planned only %d requests", len(out))
	}
	return out
}

// kindOf names the pool a path parameter is filled from.
func kindOf(pattern, param string) string {
	switch param {
	case "channelID":
		return "channel"
	case "messageID", "threadID":
		return "message"
	case "userID":
		return "user"
	case "number":
		return "number"
	case "slug", "code", "rev", "emoji", "commandID", "tokenID", "templateID", "tagID", "scheduledID", "noteID", "interactionID", "token":
		return "literal"
	}
	before := pattern[:strings.Index(pattern, "{"+param+"}")]
	segs := strings.Split(strings.Trim(before, "/"), "/")
	switch segs[len(segs)-1] {
	case "channels", "talks":
		return "channel"
	case "categories":
		return "category"
	case "dms":
		return "dm"
	case "events":
		return "event"
	case "media":
		return "media"
	case "users":
		return "user"
	case "roles":
		return "role"
	case "reports":
		return "report"
	}
	return "any"
}

func (s *canarySweep) values(kind string) []string {
	switch kind {
	case "literal":
		return []string{"1", "x"}
	case "any":
		var all []string
		for _, v := range s.w.pool {
			all = append(all, v...)
		}
		sort.Strings(all)
		return all
	}
	return s.w.pool[kind]
}

// expand fills every parameter of a pattern; smoke keeps at most 6 of the
// combinations, always including the first (hidden objects come in the pools
// in creation order, so sampling is random, not first-n).
func (s *canarySweep) expand(pattern string) []string {
	paths := []string{pattern}
	for _, m := range routeParam2.FindAllStringSubmatch(pattern, -1) {
		var next []string
		for _, p := range paths {
			for _, v := range s.values(kindOf(pattern, m[1])) {
				next = append(next, strings.Replace(p, m[0], url.PathEscape(v), 1))
			}
		}
		paths = next
	}
	if !s.deep && len(paths) > 6 {
		s.w.rng.Shuffle(len(paths), func(i, j int) { paths[i], paths[j] = paths[j], paths[i] })
		paths = paths[:6]
	}
	return paths
}

// queryVariants gives each documented query parameter of a route values from
// the world, one parameter at a time.
func (s *canarySweep) queryVariants(params []string) []string {
	var out []string
	for _, p := range params {
		var vals []string
		switch {
		case p == "q" || p == "query":
			vals = s.searchTerms()
		case p == "channel_id" || p == "channel":
			vals = s.w.pool["channel"]
		case p == "around" || p == "before" || p == "after" || p == "message_id" || p == "anchor" || p == "before_id":
			vals = s.w.pool["message"]
		case p == "user_id" || p == "author_id" || p == "author" || p == "mentions" || p == "actor":
			vals = s.w.pool["user"]
		case p == "target_id":
			vals = s.values("any")
		case p == "dm" || p == "deleted":
			vals = []string{"1", "true"}
		case p == "url":
			vals = s.internalLinks()
		}
		if !s.deep && len(vals) > 8 {
			vals = slices.Clone(vals)
			s.w.rng.Shuffle(len(vals), func(i, j int) { vals[i], vals[j] = vals[j], vals[i] })
			vals = vals[:8]
		}
		for _, v := range vals {
			out = append(out, p+"="+url.QueryEscape(v))
		}
	}
	return out
}

func (s *canarySweep) searchTerms() []string {
	var out []string
	for word, c := range s.w.words {
		if s.deep || c != clsOpen {
			out = append(out, word)
		}
	}
	sort.Strings(out)
	return out
}

func (s *canarySweep) internalLinks() []string {
	var out []string
	for _, m := range s.w.pool["message"] {
		out = append(out, s.w.a.origin()+"/channels/"+s.w.staffText.String()+"/"+m)
	}
	return out
}

// openAPIQueries maps each GET path to its documented query parameters.
func openAPIQueries(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []struct{ In, Name string }
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for p, ops := range doc.Paths {
		for _, prm := range ops["get"].Parameters {
			if prm.In == "query" {
				out[p] = append(out[p], prm.Name)
			}
		}
	}
	return out
}

// run sends every request as one observer and checks each answer.
func (s *canarySweep) run(t *testing.T, who string, get func(string) response) {
	t.Helper()
	s.found[who], s.status[who] = map[string]bool{}, map[int]int{}
	for _, r := range s.requests {
		if strings.HasPrefix(r.route, "/api/bot/") != (who == "bot") {
			continue
		}
		res := s.get(get, r)
		s.status[who][res.status]++
		s.check(who, r.route, r.path, expandBody(res.body))
		if twin, ok := s.twin(who, r.path); ok {
			s.existence(who, r, res, s.get(get, canaryRequest{r.route, twin}))
		}
	}
}

func (s *canarySweep) get(get func(string) response, r canaryRequest) response {
	res := get(r.path)
	for retry := 0; res.status == http.StatusTooManyRequests && retry < 2 && !codeGuessLimited(r.route); retry++ {
		time.Sleep(time.Duration(retryAfter(res)) * time.Second)
		res = get(r.path)
	}
	s.sent++
	return res
}

// twin is the request with every id the observer may not see replaced by an
// id nobody has; false when the request names no such id.
func (s *canarySweep) twin(who, path string) (string, bool) {
	if who == "exstaff" || who == "bot" {
		return "", false // exstaff knew the ids; the bot API is TestBotAPIScope's
	}
	out := path
	for _, id := range uuidPattern.FindAllString(path, -1) {
		c, hidden := s.w.ids[id]
		if who == "owner" && c == clsDM {
			// Suspicion S-L6-b: the Owner holds everything on an unknown channel
			// id (perm: the Owner shortcut comes before "channel not known"), so a
			// conversation they are not in answers 404 where a random id answers
			// 200. The Owner operates the server and its database; not compared.
			return "", false
		}
		if hidden && !mayRead(who, c) {
			out = strings.ReplaceAll(out, id, uuid.NewString())
		}
	}
	return out, out != path
}

// existence: an object the observer may not see answers exactly like one
// that does not exist.
func (s *canarySweep) existence(who string, r canaryRequest, hidden, missing response) {
	if hidden.status == http.StatusTooManyRequests || missing.status == http.StatusTooManyRequests {
		return // a rate limit answered, not the route: no comparison
	}
	s.twins++
	if hidden.status == missing.status && bytes.Equal(hidden.body, missing.body) {
		return
	}
	s.leak(who, r.route, "existence", fmt.Sprintf("%s: %s answers a hidden object %d %.200s, a missing one %d %.200s",
		who, r.path, hidden.status, hidden.body, missing.status, missing.body))
}

// codeGuessLimited are the public routes that take a secret code: their
// limiter is per address and long, so a refusal there is counted, not waited out.
func codeGuessLimited(route string) bool {
	return strings.HasPrefix(route, "/api/join/") || strings.HasPrefix(route, "/api/invites/")
}

func retryAfter(res response) int {
	n, err := strconv.Atoi(res.header.Get("Retry-After"))
	if err != nil || n < 1 {
		return 1
	}
	return min(n, 2)
}

// expandBody returns the body, with the files of a zip archive appended.
func expandBody(body []byte) []byte {
	if !bytes.HasPrefix(body, []byte("PK")) {
		return body
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return body
	}
	out := slices.Clone(body)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		out = append(out, b...)
	}
	return out
}

var canaryWord = regexp.MustCompile(`zq[a-z]{10}`)
var uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// check records the words an observer saw and every leak in one answer.
func (s *canarySweep) check(who, route, where string, body []byte) {
	seen := map[string]bool{}
	for _, word := range canaryWord.FindAllString(string(body), -1) {
		c, ok := s.w.words[word]
		if !ok || seen[word] {
			continue
		}
		seen[word] = true
		s.found[who][word] = true
		if !mayRead(who, c) {
			s.leak(who, route, s.w.wordWhat[word], fmt.Sprintf("%s: %s word of %q (class %s) in %s: %s", who, word, s.w.wordWhat[word], c, where, excerpt(body, word)))
		}
	}
	for _, id := range uuidPattern.FindAllString(string(body), -1) {
		c, ok := s.w.ids[id]
		// exstaff learnt the Staff ids while on Staff; only its words are checked.
		if !ok || seen[id] || strings.Contains(where, id) || mayRead(who, c) || (who == "exstaff" && c == clsStaff) {
			continue
		}
		seen[id] = true
		s.leak(who, route, s.w.idWhat[id], fmt.Sprintf("%s: id of %q (class %s) in %s: %s", who, s.w.idWhat[id], c, where, excerpt(body, id)))
	}
}

// canaryKnownLeaks are the leaks of a recorded finding: observer, route and
// object. Each must still happen (the list may only shrink: a row that no
// longer leaks fails until it is removed), and no other leak may.
var canaryKnownLeaks = map[string]string{
	"mod /api/admin/audit-log staff forum":                         "H-L6-01",
	"mod /api/admin/audit-log staff forum tag":                     "H-L6-01",
	"mod /api/admin/audit-log staff moderated post":                "H-L6-01",
	"mod /api/admin/audit-log staff post first title":              "H-L6-01",
	"mod /api/admin/audit-log staff post title set by a moderator": "H-L6-01",
	// The same entries found by target_id: a forum id lists them, a missing id
	// lists nothing. Smoke samples the target ids, so only the deep run must hit it.
	"mod /api/admin/audit-log existence": "H-L6-01",
}

// canaryKnownDeepOnly are rows of canaryKnownLeaks that only the deep run is
// sure to reach.
var canaryKnownDeepOnly = map[string]bool{"mod /api/admin/audit-log existence": true}

func (s *canarySweep) leak(who, route, what, msg string) {
	if key := who + " " + route + " " + what; canaryKnownLeaks[key] != "" {
		s.known[key] = true
		return
	}
	if !slices.Contains(s.leaks, msg) {
		s.leaks = append(s.leaks, msg)
	}
}

func excerpt(body []byte, needle string) string {
	i := bytes.Index(body, []byte(needle))
	lo, hi := max(0, i-160), min(len(body), i+len(needle)+80)
	return string(body[lo:hi])
}

// frameLog keeps every frame a socket received.
type frameLog struct {
	mu     sync.Mutex
	frames []wsEvent
}

func recordFrames(t *testing.T, c *client) *frameLog {
	t.Helper()
	ws, _, err := c.dialWSQuery(c.a.origin(), "?dm=1")
	if err != nil {
		t.Fatal(err)
	}
	log := &frameLog{}
	go func() {
		for ev := range ws.events {
			log.mu.Lock()
			log.frames = append(log.frames, ev)
			log.mu.Unlock()
		}
	}()
	if _, ok := ws.expect("presence_snapshot", soon); ok {
		return log
	}
	return log
}

func (s *canarySweep) frames(t *testing.T, who string, log *frameLog) {
	t.Helper()
	log.mu.Lock()
	defer log.mu.Unlock()
	// The bot's gateway carries the one open message posted after it connected.
	if least := map[bool]int{true: 1, false: 3}[who == "bot"]; len(log.frames) < least {
		t.Errorf("%s: only %d frames recorded: the socket recorded nothing", who, len(log.frames))
	}
	for i, ev := range log.frames {
		s.check(who, "ws", fmt.Sprintf("ws frame %d %s", i, ev.Type), append([]byte(ev.Type+" "), ev.Payload...))
	}
	s.status[who+" ws"] = map[int]int{len(log.frames): 1}
}

// report fails on every leak and on a control that reached too little.
func (s *canarySweep) report(t *testing.T) {
	t.Helper()
	t.Logf("sent %d requests, %d hidden-against-missing pairs; statuses %v", s.sent, s.twins, s.status)
	if s.twins < 200 {
		t.Errorf("only %d hidden-against-missing pairs: the existence check compares nothing", s.twins)
	}
	var missed []string
	for word, c := range s.w.words {
		if c == clsStaff && !s.found["insider"][word] {
			missed = append(missed, s.w.wordWhat[word])
		}
	}
	sort.Strings(missed)
	t.Logf("Staff words the control never reached (%d): %v", len(missed), missed)
	if ok := s.status["insider"][http.StatusOK]; ok < 120 {
		t.Errorf("the control got only %d answers of 200 (120 expected): the sweep reaches nothing", ok)
	}
	if len(missed) > len(s.w.words)/4 {
		t.Errorf("the control reached too few Staff words")
	}
	for key, id := range canaryKnownLeaks {
		if !s.known[key] && (s.deep || !canaryKnownDeepOnly[key]) {
			t.Errorf("known leak %s (%s) did not happen: drop it from canaryKnownLeaks", key, id)
		}
	}
	sort.Strings(s.leaks)
	for _, l := range s.leaks {
		t.Error(l)
	}
}
