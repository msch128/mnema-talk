package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

// decorate adds the session's identity to a REST request: the cookie, and for
// the origin check of the server the page the request would come from.
func (s *session) decorate(req *http.Request) {
	base := s.r.cfg.base
	req.Header.Set("Cookie", s.r.cfg.cookie+"="+s.tok.Token)
	req.Header.Set("Origin", lt.Origin(base))
	if s.r.cfg.spoofXFF {
		req.Header.Set("X-Forwarded-For", s.xff())
	}
	if req.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
}

// readAll drains and closes a response, returning its body (capped).
func readAll(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// outcome is how a write request ended.
type outcome int

const (
	outcomeOK outcome = iota
	outcomeLimited
	outcomeError
)

// call sends one write request and counts it under kind k. On success the
// response body is returned. A request in flight when the writers stop is let
// finish (it runs under the context of the sessions, not of the hold), so its
// event is still expected and counted.
func (s *session) call(k kind, method, path string, body any) ([]byte, outcome, time.Duration) {
	st := &s.r.req[k]
	st.attempts.Add(1)
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(s.r.sctx, method, s.r.cfg.base.String()+path, bytes.NewReader(raw))
	if err != nil {
		st.errors.Add(1)
		return nil, outcomeError, 0
	}
	s.decorate(req)
	resp, err := s.r.http.Do(req)
	if err != nil {
		st.errors.Add(1)
		s.r.noteFailure(k, 0, []byte(err.Error()))
		return nil, outcomeError, 0
	}
	retry := retryAfter(resp)
	data, _ := readAll(resp)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		st.rateLimited.Add(1)
		return nil, outcomeLimited, retry
	case resp.StatusCode/100 != 2:
		st.errors.Add(1)
		s.r.noteFailure(k, resp.StatusCode, data)
		return nil, outcomeError, 0
	}
	return data, outcomeOK, 0
}

// noteFailure keeps the first answer of every kind and status that failed, so
// the report can say why requests failed. The server's error body names the
// cause and carries no secrets.
func (r *run) noteFailure(k kind, status int, body []byte) {
	key := fmt.Sprintf("%s %d", k, status)
	text := strings.TrimSpace(string(body))
	if len(text) > 160 {
		text = text[:160]
	}
	r.failMu.Lock()
	defer r.failMu.Unlock()
	if r.failures == nil {
		r.failures = map[string]*failure{}
	}
	f := r.failures[key]
	if f == nil {
		f = &failure{Sample: text}
		r.failures[key] = f
	}
	f.Count++
}

// retryAfter reads the Retry-After header in seconds, a second when absent.
func retryAfter(resp *http.Response) time.Duration {
	if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return time.Second
}
