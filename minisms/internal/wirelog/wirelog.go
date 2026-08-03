// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
// Package wirelog writes per-entity carrier/client wire logs to dedicated, size-rotated files under a
// base directory (default /var/log/minisms). One file per entity per channel:
//
//	/var/log/minisms/<name>_smpp.log   and   /var/log/minisms/<name>_http.log
//
// It records every SMPP PDU and every HTTP request/response exchanged with a carrier or client, with
// direction markers (>> sent, << received, -- lifecycle). Credentials (SMPP passwords, HTTP auth and
// secret headers/params) are always masked: this file may live on a shared or published host, so the
// standing "never write secrets to disk" rule wins over verbosity.
//
// A process-wide singleton (Init/Emit) lets the many I/O call sites log without threading a handle
// through every signature; when uninitialised or disabled, all calls are cheap no-ops.
package wirelog

import (
	"fmt"
	"log/slog"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Manager owns the open per-entity log files and their rotation policy.
type Manager struct {
	dir      string
	maxBytes int64
	maxFiles int
	enabled  bool
	mu       sync.Mutex
	loggers  map[string]*fileLogger
}

var std *Manager

// Init installs the process-wide wire logger. maxMB is the rotation threshold per file and maxFiles
// the number of rotated generations to keep. When enabled, the base directory is created if missing.
func Init(dir string, enabled bool, maxMB, maxFiles int) error {
	if strings.TrimSpace(dir) == "" {
		dir = "/var/log/minisms"
	}
	if maxMB <= 0 {
		maxMB = 100
	}
	if maxFiles <= 0 {
		maxFiles = 5
	}
	m := &Manager{
		dir:      dir,
		maxBytes: int64(maxMB) * 1024 * 1024,
		maxFiles: maxFiles,
		enabled:  enabled,
		loggers:  make(map[string]*fileLogger),
	}
	if enabled {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("wirelog: create %s: %w", dir, err)
		}
	}
	std = m
	return nil
}

// Enabled reports whether wire logging is active.
func Enabled() bool { return std != nil && std.enabled }

// Emit appends one line to <name>_<channel>.log. channel is "smpp" or "http"; dir is ">>" (sent to the
// entity), "<<" (received from the entity) or "--" (a connection/lifecycle event). kv are alternating
// key/value strings and must already have secrets masked by the caller where structure is known; Emit
// additionally masks any value under an obviously-sensitive key as a backstop.
func Emit(name, channel, dir, event string, kv ...string) {
	if std == nil || !std.enabled {
		return
	}
	std.line(name, channel, dir, event, kv...)
}

func (m *Manager) line(name, channel, dir, event string, kv ...string) {
	lg := m.loggerFor(name, channel)
	if lg == nil {
		return
	}
	var b strings.Builder
	b.WriteString(time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	b.WriteByte(' ')
	b.WriteString(dir)
	b.WriteByte(' ')
	b.WriteString(event)
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&b, " %s=%s", kv[i], quoteVal(maskKV(kv[i], kv[i+1])))
	}
	b.WriteByte('\n')
	lg.write(b.String())
}

func (m *Manager) loggerFor(name, channel string) *fileLogger {
	key := sanitize(name) + "_" + channel
	m.mu.Lock()
	defer m.mu.Unlock()
	if lg, ok := m.loggers[key]; ok {
		return lg
	}
	lg := &fileLogger{
		path:     filepath.Join(m.dir, key+".log"),
		maxBytes: m.maxBytes,
		maxFiles: m.maxFiles,
	}
	m.loggers[key] = lg
	return lg
}

// fileLogger is a single append-only, size-rotated log file.
type fileLogger struct {
	path     string
	maxBytes int64
	maxFiles int
	mu       sync.Mutex
	f        *os.File
	size     int64
	warned   bool
}

func (l *fileLogger) write(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		if err := l.open(); err != nil {
			return
		}
	}
	if l.size+int64(len(line)) > l.maxBytes {
		l.rotate()
	}
	n, err := l.f.WriteString(line)
	if err != nil {
		// Drop the handle so the next write retries a fresh open rather than looping on a bad fd.
		_ = l.f.Close()
		l.f = nil
		return
	}
	l.size += int64(n)
}

func (l *fileLogger) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		if !l.warned {
			// Surface the first failure (permissions, a read-only mount / systemd sandbox, full disk)
			// so a misconfigured log path is diagnosable instead of silently dropping every line.
			slog.Warn("wirelog: cannot open log file, entries will be dropped", "path", l.path, "error", err.Error())
			l.warned = true
		}
		return err
	}
	if st, err := f.Stat(); err == nil {
		l.size = st.Size()
	}
	l.f = f
	return nil
}

// rotate closes the current file, shifts path.(n-1) -> path.n dropping the oldest, then reopens fresh.
func (l *fileLogger) rotate() {
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	oldest := fmt.Sprintf("%s.%d", l.path, l.maxFiles)
	_ = os.Remove(oldest)
	for i := l.maxFiles - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	_ = os.Rename(l.path, l.path+".1")
	l.size = 0
	_ = l.open()
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitize turns an entity name into a safe, readable filename stem.
func sanitize(name string) string {
	s := unsafeName.ReplaceAllString(strings.TrimSpace(name), "_")
	s = strings.Trim(s, "._-")
	if s == "" {
		s = "unknown"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func quoteVal(v string) string {
	if v == "" {
		return "\"\""
	}
	if strings.ContainsAny(v, " \t\"") {
		return "\"" + strings.ReplaceAll(v, "\"", "'") + "\""
	}
	return v
}

// secretTokens is the shared set of credential-shaped substrings recognised across headers, query/form
// params and JSON body keys, so masking stays consistent everywhere.
const secretTokens = `pass|secret|token|authoriz|api[-_]?key|credential`

var secretKey = regexp.MustCompile(`(?i)` + secretTokens + `|x-api-key`)

// jsonSecretRe matches a JSON string member whose key contains a credential token (for example
// "dlr_secret":"...", "password":"...", "access_token":"...") so its value can be masked. The value
// pattern tolerates escaped characters. Non-string values are left as-is (secrets are strings here).
var jsonSecretRe = regexp.MustCompile(`(?i)"([^"]*(?:` + secretTokens + `)[^"]*)"(\s*:\s*)"(?:[^"\\]|\\.)*"`)

// maskKV masks a value whose key looks sensitive, as a backstop to explicit call-site masking.
func maskKV(key, val string) string {
	if val != "" && secretKey.MatchString(key) {
		return "***"
	}
	return val
}

// Mask returns a redacted form of a secret suitable for a wire log: empty stays empty, otherwise a
// fixed marker with the length so operators can still spot a wrong/blank credential without leaking it.
func Mask(secret string) string {
	if secret == "" {
		return "(empty)"
	}
	return fmt.Sprintf("***(%d)", len(secret))
}

// redactQueryValues masks the values of sensitive parameters (password, secret, token, apikey, ...) in
// a url-encoded query/form string, preserving everything else.
func redactQueryValues(raw string) string {
	v, err := neturl.ParseQuery(raw)
	if err != nil {
		return "(unparseable)"
	}
	for k := range v {
		if secretKey.MatchString(k) {
			v[k] = []string{"***"}
		}
	}
	return v.Encode()
}

// RedactURL returns a loggable URL with any credential query parameters masked.
func RedactURL(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil {
		return "(unparseable url)"
	}
	if u.RawQuery != "" {
		u.RawQuery = redactQueryValues(u.RawQuery)
	}
	if u.User != nil {
		u.User = neturl.User(u.User.Username()) // drop any embedded password
	}
	return u.String()
}

// RedactBody masks credential parameters in a request/response body: credential-shaped keys are masked
// in JSON bodies and in url-encoded (form) bodies alike. XML bodies (none carry secrets in this system)
// pass through. Operates textually so it is safe on bodies truncated by the capture cap.
func RedactBody(body string) string {
	t := strings.TrimSpace(body)
	switch {
	case t == "":
		return body
	case strings.HasPrefix(t, "{") || strings.HasPrefix(t, "["):
		return jsonSecretRe.ReplaceAllString(body, `"${1}"${2}"***"`)
	case strings.HasPrefix(t, "<"):
		return body
	case strings.Contains(body, "="):
		return redactQueryValues(body)
	}
	return body
}

// RedactHeaders formats a header map for logging with sensitive header values masked, keys sorted.
func RedactHeaders(h map[string]string) string {
	if len(h) == 0 {
		return ""
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := h[k]
		if secretKey.MatchString(k) {
			v = "***"
		}
		parts = append(parts, k+": "+v)
	}
	return strings.Join(parts, "; ")
}
