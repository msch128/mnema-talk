package mnema

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BasePath is the path of the bot REST API.
const BasePath = "/api/bot/v1"

const (
	defaultMaxRetries = 3
	maxResponseBytes  = 16 << 20
)

// REST is the client of the bot REST API. It keeps within the server's rate
// limits (50 requests per second, 5 messages per 5 seconds per channel),
// waits out 429 answers (retry_after, Retry-After) with bounded retries, and
// stops waiting when the context ends. It is safe for concurrent use.
type REST struct {
	baseURL    string
	token      string
	http       *http.Client
	limits     *limiter
	maxRetries int
	logger     *slog.Logger
}

// NewREST returns a REST client for o.BaseURL and o.Token; the gateway fields
// of Options are ignored. New builds one for every Client.
func NewREST(o Options) (*REST, error) {
	base := strings.TrimRight(o.BaseURL, "/")
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("mnema: BaseURL must be an http or https URL, got %q", o.BaseURL)
	}
	if o.Token == "" {
		return nil, errors.New("mnema: Token is required")
	}
	hc := o.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	retries := o.MaxRetries
	switch {
	case retries == 0:
		retries = defaultMaxRetries
	case retries < 0:
		retries = 0
	}
	return &REST{baseURL: base, token: o.Token, http: hc, limits: newLimiter(), maxRetries: retries, logger: loggerOr(o.Logger)}, nil
}

func loggerOr(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.Default()
	}
	return l
}

// request is one call of the API. body is sent as is (it is kept for
// retries); out receives the decoded answer, unless the answer is empty.
type request struct {
	method      string
	path        string
	query       url.Values
	body        []byte
	contentType string
	out         any
}

func jsonRequest(method, path string, in, out any) (request, error) {
	req := request{method: method, path: path, out: out}
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return req, fmt.Errorf("mnema: encode request: %w", err)
		}
		req.body, req.contentType = b, "application/json"
	}
	return req, nil
}

// call sends a JSON request and decodes the answer into a new T; it returns
// nil without error when the server answered without a body (204).
func call[T any](ctx context.Context, r *REST, method, path string, in any) (*T, error) {
	out := new(T)
	req, err := jsonRequest(method, path, in, out)
	if err != nil {
		return nil, err
	}
	empty, err := r.do(ctx, req)
	if err != nil || empty {
		return nil, err
	}
	return out, nil
}

// do runs req, waiting for the rate limits and retrying 429 answers up to
// maxRetries times. It reports whether the answer had no body.
func (r *REST) do(ctx context.Context, req request) (bool, error) {
	key := bucketKey(req.method, req.path)
	for attempt := 0; ; attempt++ {
		if err := r.limits.acquire(ctx, key); err != nil {
			return false, err
		}
		status, header, body, err := r.send(ctx, req)
		if err != nil {
			return false, err
		}
		if status == http.StatusTooManyRequests {
			rl := parseRateLimit(header, body)
			// A wait beyond the cap is not taken or stored: the request fails
			// with the 429 and the bot decides, instead of every route
			// stalling behind one answer.
			if rl.RetryAfter > MaxRetryAfter {
				return false, rl
			}
			r.limits.pause(key, rl.Global, rl.RetryAfter+retryJitter())
			if attempt < r.maxRetries {
				r.logger.DebugContext(ctx, "mnema: rate limited, waiting", "bucket", key, "retry_after", rl.RetryAfter, "global", rl.Global)
				continue
			}
			return false, rl
		}
		return decodeAnswer(status, body, req.out)
	}
}

func (r *REST) send(ctx context.Context, req request) (int, http.Header, []byte, error) {
	u := r.baseURL + BasePath + req.path
	if len(req.query) > 0 {
		u += "?" + req.query.Encode()
	}
	var body io.Reader
	if req.body != nil {
		body = bytes.NewReader(req.body)
	}
	hr, err := http.NewRequestWithContext(ctx, req.method, u, body)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("mnema: build request: %w", err)
	}
	hr.Header.Set("Authorization", "Bot "+r.token)
	hr.Header.Set("User-Agent", userAgent())
	hr.Header.Set("Accept", "application/json")
	if req.contentType != "" {
		hr.Header.Set("Content-Type", req.contentType)
	}
	res, err := r.http.Do(hr)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("mnema: %s %s: %w", req.method, req.path, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("mnema: read answer of %s %s: %w", req.method, req.path, err)
	}
	return res.StatusCode, res.Header, b, nil
}

func decodeAnswer(status int, body []byte, out any) (bool, error) {
	if status < 200 || status > 299 {
		return false, parseError(status, body)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true, nil
	}
	if out == nil {
		return false, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("mnema: decode answer: %w", err)
	}
	return false, nil
}

func parseError(status int, body []byte) *RESTError {
	var e struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	}
	if json.Unmarshal(body, &e) != nil || e.Message == "" {
		e.Message = strings.TrimSpace(string(body))
		if e.Message == "" {
			e.Message = http.StatusText(status)
		}
	}
	return &RESTError{Status: status, Code: e.Code, Message: e.Message}
}

// MaxRetryAfter is the longest 429 wait the client takes on its own.
const MaxRetryAfter = 60 * time.Second

// retryJitter spreads retries a little, so bots on one host do not return at once.
func retryJitter() time.Duration { return time.Duration(rand.Int64N(int64(250 * time.Millisecond))) }

// seconds turns a retry_after in seconds into a duration; anything that is not
// a positive finite number, or that does not fit, gives 0.
func seconds(s float64) time.Duration {
	if math.IsNaN(s) || s <= 0 {
		return 0
	}
	if s > float64(math.MaxInt64/int64(time.Second)) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s * float64(time.Second))
}

func parseRateLimit(h http.Header, body []byte) *RESTError {
	var rl struct {
		Message    string  `json:"message"`
		RetryAfter float64 `json:"retry_after"`
		Global     bool    `json:"global"`
	}
	_ = json.Unmarshal(body, &rl)
	wait := seconds(rl.RetryAfter)
	if wait <= 0 {
		if s, err := strconv.ParseFloat(h.Get("Retry-After"), 64); err == nil {
			wait = seconds(s)
		}
	}
	if wait <= 0 {
		wait = time.Second
	}
	if rl.Message == "" {
		rl.Message = "You are being rate limited."
	}
	global := rl.Global || h.Get("X-RateLimit-Global") == "true"
	return &RESTError{Status: http.StatusTooManyRequests, Message: rl.Message, RetryAfter: wait, Global: global}
}
