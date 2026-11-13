// Package sb8200test is an SB8200 for tests: a TLS server speaking what
// one modem on firmware AB01.02.053.01 was seen to speak, serving pages
// it served with the serial and MAC addresses replaced. The login is
// the page's: the credential as the status page's query string and a
// Basic authorization header, a token in answer, the token expected
// back as the "credential" cookie; the login page, with a 200, for a
// refused login, a page asked without the cookie, and any path the
// modem does not know. Sessions do not exclude each other, as the
// modem's do not: a login leaves the earlier tokens good. Its TLS is the modem's: 1.2, RSA key exchange
// only, which a client on Go's default cipher suites cannot join. A password change is a POST of the new
// credential as the query string, which also ends the session.
package sb8200test

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

//go:embed testdata/*.html
var pages embed.FS

// The credentials the modem starts with.
const (
	Username = "admin"
	Password = "Changeme1!"
)

const (
	cookieName  = "credential"
	tokenLength = 31
	loginPage   = "testdata/login.html"
	statusPage  = "cmconnectionstatus.html"
	changePage  = "changepwd_tab.html"
	logoutPage  = "logout.html"
	lockFile    = "LoginLock.txt"
)

// Modem is the fake, listening until Close.
type Modem struct {
	server *httptest.Server

	mu           sync.Mutex
	pages        map[string]string
	password     string
	tokens       map[string]struct{}
	primed       bool
	refusals     int
	locked       bool
	drop         bool
	keepPassword bool
	logins       int
	logouts      int
}

// New starts a modem with the default credentials.
func New() *Modem {
	modem := &Modem{password: Password, pages: map[string]string{}, tokens: map[string]struct{}{}}
	modem.server = httptest.NewUnstartedServer(http.HandlerFunc(modem.serve))
	modem.server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_256_GCM_SHA384}, // #nosec G402 -- what the modem offers
	}
	modem.server.StartTLS()

	return modem
}

// URL is the modem's page: scheme and host.
func (m *Modem) URL() string { return m.server.URL }

// Fingerprint is the SHA-256 of the modem's certificate, in hex.
func (m *Modem) Fingerprint() string {
	sum := sha256.Sum256(m.server.Certificate().Raw)

	return hex.EncodeToString(sum[:])
}

// Close stops the server.
func (m *Modem) Close() { m.server.Close() }

// SetPage replaces what the modem serves for a page, for a test of a
// page in another shape.
func (m *Modem) SetPage(name, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pages[name] = body
}

// Drop makes the modem hang up on a login and on a password change, the
// two requests whose query is a credential, without answering.
func (m *Modem) Drop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.drop = true
}

// Idle makes the modem forget it has served its login page: until it
// serves the page again it refuses every login. A new modem starts that
// way. Seen on the modem after it had sat idle: a login with nothing
// before it refused, and one after a load of the root taken. That a
// refusal's page, or the page served for any other path, counts as well
// was not seen; the test modem takes it to, since a refused login asked
// again was taken.
func (m *Modem) Idle() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.primed = false
}

// Refusals counts the logins refused.
func (m *Modem) Refusals() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.refusals
}

// Lock makes the modem answer every login with its login page and
// report itself locked, as it does after too many invalid logins.
func (m *Modem) Lock() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.locked = true
}

// KeepPassword makes the modem answer a password change as it answers
// one it takes, and keep its password.
func (m *Modem) KeepPassword() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.keepPassword = true
}

// CurrentPassword is the password a login takes now.
func (m *Modem) CurrentPassword() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.password
}

// LoggedIn reports whether a session is open.
func (m *Modem) LoggedIn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.tokens) > 0
}

// Logins counts the logins accepted.
func (m *Modem) Logins() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.logins
}

// Logouts counts the logouts seen.
func (m *Modem) Logouts() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.logouts
}

func (m *Modem) serve(writer http.ResponseWriter, request *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := strings.TrimPrefix(request.URL.Path, "/")

	credential := request.URL.RawQuery != "" && (name == statusPage || name == changePage)

	switch {
	case m.drop && credential:
		hangUp(writer)
	case name == logoutPage:
		// Every session ends: which of them a logout ends on the modem
		// was not seen.
		clear(m.tokens)
		m.logouts++
	case name == lockFile:
		m.write(writer, m.lockFlag())
	case name == statusPage && request.URL.RawQuery != "":
		m.login(writer, request)
	case name == changePage && request.Method == http.MethodPost:
		m.changePassword(writer, request)
	default:
		m.page(writer, request, name)
	}
}

func (m *Modem) lockFlag() string {
	if m.locked {
		return "Locked"
	}

	return "NotLocked"
}

// login takes the credential as the query string, and insists on the
// same in the Authorization header, as the page sends both.
func (m *Modem) login(writer http.ResponseWriter, request *http.Request) {
	credential := request.URL.RawQuery
	expected := base64.StdEncoding.EncodeToString([]byte(Username + ":" + m.password))

	wrong := credential != expected || request.Header.Get("Authorization") != "Basic "+credential

	if m.locked || !m.primed || wrong {
		m.refusals++
		m.loginPage(writer)

		return
	}

	raw := make([]byte, tokenLength)
	_, _ = rand.Read(raw)
	token := hex.EncodeToString(raw)[:tokenLength]
	m.tokens[token] = struct{}{}
	m.logins++

	m.write(writer, token)
}

// changePassword takes the new credential as the query string under the
// session's cookie, sets it, and ends the session as the page does. It
// checks nothing about the new password: the modem's own rule is in
// its page's script.
func (m *Modem) changePassword(writer http.ResponseWriter, request *http.Request) {
	if !m.authenticated(request) {
		m.loginPage(writer)

		return
	}

	decoded, err := base64.StdEncoding.DecodeString(request.URL.RawQuery)
	user, password, ok := strings.Cut(string(decoded), ":")

	if err != nil || !ok || user != Username {
		m.loginPage(writer)

		return
	}

	if !m.keepPassword {
		m.password = password
	}

	if cookie, err := request.Cookie(cookieName); err == nil {
		delete(m.tokens, cookie.Value)
	}

	// The modem was seen to answer a change it took with its login page.
	m.loginPage(writer)
}

// page serves a captured page under the session's cookie, the login
// page otherwise and for any name it does not have.
func (m *Modem) page(writer http.ResponseWriter, request *http.Request, name string) {
	if !m.authenticated(request) {
		m.loginPage(writer)

		return
	}

	if body, ok := m.pages[name]; ok {
		m.write(writer, body)

		return
	}

	body, err := pages.ReadFile("testdata/" + name)
	if err != nil {
		m.loginPage(writer)

		return
	}

	m.write(writer, string(body))
}

func (m *Modem) authenticated(request *http.Request) bool {
	cookie, err := request.Cookie(cookieName)
	if err != nil || cookie == nil {
		return false
	}

	_, known := m.tokens[cookie.Value]

	return known
}

func (m *Modem) loginPage(writer http.ResponseWriter) {
	m.primed = true

	body, _ := pages.ReadFile(loginPage)
	m.write(writer, string(body))
}

func (*Modem) write(writer http.ResponseWriter, body string) {
	writer.Header().Set("Content-Type", "text/html")
	_, _ = writer.Write([]byte(body)) // #nosec G705 -- a test fake serving its fixtures
}

// hangUp closes the connection under the request, unanswered.
func hangUp(writer http.ResponseWriter) {
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		return
	}

	conn, _, err := hijacker.Hijack()
	if err != nil {
		return
	}

	_ = conn.Close()
}
