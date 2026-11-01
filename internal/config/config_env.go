package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/msch128/mnema-talk/internal/ssrf"
	"github.com/msch128/mnema-talk/internal/stunprobe"
)

// FromEnv builds a Config from a lookup function, which keeps it testable.
func FromEnv(lookup func(string) (string, bool)) (*Config, error) {
	r := &envReader{lookup: lookup}
	cfg := r.textSettings()
	portMin, portMax := r.numericSettings(cfg)
	cfg.SMTP = r.smtpSettings()
	problems := r.typedProblems()
	problems = append(problems, RequiredProblems(lookup)...)
	if len(problems) > 0 {
		return nil, &ConfigError{Problems: problems}
	}
	for _, o := range SplitList(r.get("CORS_ALLOWED_ORIGINS", cfg.PublicURL)) {
		cfg.AllowedOrigins = append(cfg.AllowedOrigins, normalizeURL(o))
	}
	if err := cfg.validate(portMin, portMax); err != nil {
		return nil, err
	}
	return cfg, nil
}

// envReader reads typed settings from a lookup function. A malformed value
// is remembered in errs and read as the zero value, so
// the caller reads everything and reports all of them at once.
type envReader struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (r *envReader) fail(err error) { r.errs = append(r.errs, err) }

// typedProblems turns every malformed value read so far into a problem.
func (r *envReader) typedProblems() []Problem {
	ps := make([]Problem, 0, len(r.errs))
	for _, err := range r.errs {
		ps = append(ps, Problem{Text: err.Error()})
	}
	return ps
}

func (r *envReader) get(key, def string) string {
	if v, ok := r.lookup(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (r *envReader) int(key string, def int) int {
	raw := r.get(key, strconv.Itoa(def))
	v, err := strconv.Atoi(raw)
	if err != nil {
		r.fail(fmt.Errorf("%s must be an integer, got %q", key, raw))
		return 0
	}
	return v
}

func (r *envReader) bool(key string, def bool) bool {
	raw := r.get(key, strconv.FormatBool(def))
	v, err := strconv.ParseBool(raw)
	if err != nil {
		r.fail(fmt.Errorf("%s must be true or false, got %q", key, raw))
		return false
	}
	return v
}

// builtinTURNPort reads WEBRTC_BUILTIN_TURN_PORT. Only an explicit "on" lets a
// malformed value stop the start; otherwise it falls back to the default with
// a warning, so an installation that never asked for the relay keeps starting.
func (r *envReader) builtinTURNPort(mode string) uint16 {
	const key, def = "WEBRTC_BUILTIN_TURN_PORT", 3479
	raw := r.get(key, strconv.Itoa(def))
	v, err := strconv.ParseUint(raw, 10, 16)
	if err == nil {
		return uint16(v)
	}
	if mode == BuiltinTURNOn {
		r.fail(fmt.Errorf("%s must be a port number (1-65535), got %q", key, raw))
		return 0
	}
	slog.Warn(key+" is not a port number, using the default", "value", raw, "default", def)
	return def
}

func (r *envReader) port(key string, def uint16) uint16 {
	raw := r.get(key, strconv.Itoa(int(def)))
	v, err := strconv.ParseUint(raw, 10, 16)
	if err != nil {
		r.fail(fmt.Errorf("%s must be a port number (1-65535), got %q", key, raw))
		return 0
	}
	return uint16(v)
}

// textSettings reads every setting that cannot be malformed.
func (r *envReader) textSettings() *Config {
	return &Config{
		BindAddr:              r.get("BIND_ADDR", ""),
		AppEnv:                strings.ToLower(r.get("APP_ENV", "development")),
		LogLevel:              strings.ToLower(r.get("LOG_LEVEL", "")),
		PublicURL:             normalizeURL(r.get("PUBLIC_URL", "http://localhost:8080")),
		TrustedProxies:        SplitList(r.get("TRUSTED_PROXY_CIDRS", defaultTrustedProxies)),
		JWTSecret:             r.get("JWT_SECRET", ""),
		AdminUsername:         r.get("ADMIN_USERNAME", "admin"),
		InitialLanguage:       strings.ToLower(r.get("INITIAL_LANGUAGE", "en")),
		AdminInitialPassword:  r.get("ADMIN_INITIAL_PASSWORD", ""),
		DatabaseURL:           r.get("DATABASE_URL", ""),
		S3Endpoint:            r.get("S3_ENDPOINT", "http://localhost:8333"),
		S3Region:              r.get("S3_REGION", "us-east-1"),
		S3Bucket:              r.get("S3_BUCKET", "mnema-media"),
		S3AccessKey:           r.get("S3_ACCESS_KEY", ""),
		S3SecretKey:           r.get("S3_SECRET_KEY", ""),
		UpdaterURL:            r.get("UPDATER_URL", ""),
		UpdaterToken:          r.get("UPDATER_TOKEN", ""),
		AppImage:              r.get("MNEMA_IMAGE", ""),
		WebRTCTURNURLs:        SplitList(r.get("WEBRTC_TURN_URLS", "")),
		WebRTCTURNSecret:      r.get("WEBRTC_TURN_SECRET", ""),
		WebRTCBuiltinTURN:     strings.ToLower(r.get("WEBRTC_BUILTIN_TURN", BuiltinTURNAuto)),
		WebRTCBuiltinTURNHost: r.get("WEBRTC_BUILTIN_TURN_HOST", ""),
		MetricsToken:          r.get("METRICS_TOKEN", ""),
		PprofAddr:             r.get("PPROF_ADDR", ""),
		WebRTCAnnounce:        SplitList(r.get("WEBRTC_NAT_1TO1_IP", "")),
		WebRTCLANIPs:          SplitList(r.get("WEBRTC_LAN_IP", "")),
		WebRTCSTUNURLs:        SplitList(r.get("WEBRTC_STUN_URLS", "")),
		WebRTCDetectSTUN:      SplitList(r.get("WEBRTC_DETECT_STUN_URLS", strings.Join(stunprobe.DefaultServers, ","))),
		LegalOperatorName:     r.get("LEGAL_OPERATOR_NAME", "Community Operator"),
		LegalOperatorEmail:    r.get("LEGAL_OPERATOR_EMAIL", "admin@example.com"),
		LegalOperatorCountry:  r.get("LEGAL_OPERATOR_COUNTRY", ""),
		LegalProjectNotice:    r.get("LEGAL_PROJECT_NOTICE", ""),
	}
}

// numericSettings reads the numbers, switches and ports into cfg and returns
// the WebRTC UDP port range, which validate stores once it has checked it.
func (r *envReader) numericSettings(cfg *Config) (portMin, portMax uint16) {
	cfg.SessionExpiryHours = r.int("SESSION_EXPIRY_HOURS", 720)
	// 0 (default) disables automatic media pruning entirely.
	cfg.MediaRetentionDays = r.int("MEDIA_RETENTION_DAYS", 0)
	cfg.AuditRetentionDays = r.int("AUDIT_RETENTION_DAYS", 0)
	cfg.EditHistoryRetentionDays = r.int("EDIT_HISTORY_RETENTION_DAYS", 0)
	cfg.MaxUploadMB = r.int("MAX_UPLOAD_SIZE_MB", 50)
	cfg.VoiceUploadLimitMbit = r.int("VOICE_UPLOAD_LIMIT_MBIT", 0)
	cfg.EmojiMax = r.int("EMOJI_MAX", 200)
	cfg.UploadQuotaMBPerDay = r.int("UPLOAD_QUOTA_MB_PER_DAY", 2048)
	cfg.MediaMaxTotalMB = r.int("MEDIA_MAX_TOTAL_MB", 0)
	cfg.MediaMinFreeMB = r.int("MEDIA_MIN_FREE_MB", 1024)
	cfg.MediaDiskPath = r.get("MEDIA_DISK_PATH", "")
	portMin = r.port("WEBRTC_UDP_PORT_MIN", 50000)
	portMax = r.port("WEBRTC_UDP_PORT_MAX", 50050)
	cfg.WebRTCUDPMuxPort = r.port("WEBRTC_UDP_MUX_PORT", portMin)
	if m := cfg.WebRTCUDPMuxPort; m != 0 && (m < portMin || m > portMax) {
		r.fail(fmt.Errorf("WEBRTC_UDP_MUX_PORT must be 0 or inside WEBRTC_UDP_PORT_MIN/MAX"))
	}
	cfg.WebRTCBuiltinTURNPort = r.builtinTURNPort(cfg.WebRTCBuiltinTURN)
	cfg.Port = strconv.Itoa(int(r.port("PORT", 8080)))
	cfg.S3ForcePathStyle = r.bool("S3_FORCE_PATH_STYLE", true)
	cfg.LinkPreviews = r.bool("LINK_PREVIEWS_ENABLED", true)
	cfg.PushEnabled = r.bool("PUSH_ENABLED", true)
	cfg.PushPreviews = strings.ToLower(r.get("PUSH_PREVIEWS", "user"))
	cfg.PushVAPIDSubject = r.get("PUSH_VAPID_SUBJECT", "")
	allow, err := ssrf.ParseAllow(r.get("WEBHOOK_ALLOW_PRIVATE_TARGETS", ""))
	if err != nil {
		r.fail(fmt.Errorf("WEBHOOK_ALLOW_PRIVATE_TARGETS: %w", err))
	}
	cfg.WebhookAllowPrivateTargets = allow
	cfg.APIDocs = r.bool("API_DOCS_ENABLED", false)
	cfg.UpdateCheck = r.bool("UPDATE_CHECK_ENABLED", true)
	cfg.WebRTCAutoAnnounce = r.bool("WEBRTC_AUTO_ANNOUNCE", true)
	cfg.ClockCheck = r.bool("CLOCK_CHECK_ENABLED", true)
	return portMin, portMax
}
