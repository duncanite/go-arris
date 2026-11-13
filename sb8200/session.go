// Package sb8200 reads the Arris SURFboard SB8200 cable modem through its
// admin page, the only interface the modem has. A Session is a login:
// the credential goes to the status page as a query string and a Basic
// authorization header, the body that comes back is a token, and every
// page after that carries the token as the "credential" cookie. Status,
// Info and Log are the three pages the modem fills in, parsed by the
// position of their table cells; ChangePassword is the one thing the
// page lets a person change.
package sb8200

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

// The pages, by the names the modem's menu gives them.
const (
	pageStatus = "cmconnectionstatus.html"
	pageInfo   = "cmswinfo.html"
	pageLog    = "cmeventlog.html"
	pageChange = "changepwd_tab.html"
	pageLogout = "logout.html"
	pageLock   = "LoginLock.txt"

	// cookieName is the cookie the modem's page sets by hand after a
	// login; the modem's own Set-Cookie header is malformed and sets
	// nothing.
	cookieName = "credential"
	// loginTitle is how the login page gives itself away: the modem
	// serves it, with a 200, for a refused login, a lost session and
	// any path it does not know.
	loginTitle = "<title>Login</title>"
	lockedFlag = "Locked"

	// The page's own rule for a new password.
	passwordMin = 8
	passwordMax = 20
	// maxBody bounds what is read of any page; the largest is the event
	// log, well under this.
	maxBody = 1 << 20
)

// The errors a Session hands back, to test with errors.Is.
var (
	// ErrRefused: the modem answered a login with its login page; the
	// credentials are wrong, or a session is still open elsewhere.
	ErrRefused = errors.New("the modem refused the login")
	// ErrLocked: too many refused logins; the modem locks its page for
	// five minutes.
	ErrLocked = errors.New("the modem has locked its page after invalid logins")
	// ErrSessionLost: a page came back as the login page; the modem
	// ended the session, which it does after a while idle.
	ErrSessionLost = errors.New("the modem ended the session")
	// ErrPage: a page is not in the shape this package knows, the
	// firmware having moved its tables.
	ErrPage = errors.New("the page is not in the expected shape")
	// ErrPassword: a new password breaks the page's own rule.
	ErrPassword = errors.New("the password does not meet the page's rule")
	// ErrStatus: the modem answered with an HTTP status other than 200.
	ErrStatus = errors.New("unexpected HTTP status")
	// ErrURL: the modem's URL has no host.
	ErrURL = errors.New("not a URL with a host")
	// ErrFingerprint: the pinned fingerprint is not a SHA-256, or the
	// modem's certificate is not the pinned one.
	ErrFingerprint = errors.New("the certificate fingerprint does not match")
)

// Session is one login to the modem, until Logout or until the modem
// ends it. It is not safe for concurrent use: the modem is not either.
type Session struct {
	client   *http.Client
	base     *url.URL
	username string
	password string
	token    string
}

// Login logs in to the modem at baseURL, the scheme and host of its page,
// and returns the session. It logs out first, as the page does: a
// session left open makes the modem refuse the next login. ErrRefused
// is the modem's answer to wrong credentials, ErrLocked its answer after
// too many of them.
func Login(ctx context.Context, client *http.Client, baseURL, username, password string) (*Session, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("%w: %q", ErrURL, baseURL)
	}

	session := &Session{client: client, base: base, username: username, password: password}

	if _, err = session.get(ctx, pageLogout, "", false); err != nil {
		return nil, err
	}

	credential := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	body, err := session.get(ctx, pageStatus, credential, true)
	if err != nil {
		return nil, err
	}

	if strings.Contains(body, loginTitle) {
		return nil, session.refusal(ctx)
	}

	session.token = strings.TrimSpace(body)
	if session.token == "" || strings.ContainsAny(session.token, "<>\n") {
		return nil, fmt.Errorf("%w: the login answered neither a token nor the login page", ErrPage)
	}

	return session, nil
}

// Logout ends the session on the modem. The modem keeps a session for a
// while otherwise, and refuses a new login meanwhile.
func (s *Session) Logout(ctx context.Context) error {
	_, err := s.get(ctx, pageLogout, "", false)
	s.token = ""

	return err
}

// Status reads the connection status page: the startup procedure, the
// bonded channels and the modem's clock.
func (s *Session) Status(ctx context.Context) (*Status, error) {
	body, err := s.page(ctx, pageStatus)
	if err != nil {
		return nil, err
	}

	return parseStatus(body)
}

// Info reads the product information page.
func (s *Session) Info(ctx context.Context) (*Info, error) {
	body, err := s.page(ctx, pageInfo)
	if err != nil {
		return nil, err
	}

	return parseInfo(body)
}

// Log reads the event log, newest first as the page lists it.
func (s *Session) Log(ctx context.Context) ([]Event, error) {
	body, err := s.page(ctx, pageLog)
	if err != nil {
		return nil, err
	}

	return parseLog(body)
}

// ChangePassword sets a new password for the session's user, as the
// page does: a POST of the new credential as the query string, under the
// session's cookie. The modem was seen to answer a change it took with
// its login page, the page that elsewhere means the session is gone, so
// the answer is not read and a nil error is not proof: the only proof of
// the change is a Login with the new password. The page logs out at once, and so does
// this. CheckPassword is applied first; the modem's own check is in its
// page's script, not behind it.
func (s *Session) ChangePassword(ctx context.Context, password string) error {
	if err := CheckPassword(password); err != nil {
		return err
	}

	if password == s.password {
		return fmt.Errorf("%w: it is the current password", ErrPassword)
	}

	credential := base64.StdEncoding.EncodeToString([]byte(s.username + ":" + password))

	target := s.base.JoinPath(pageChange)
	target.RawQuery = credential

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), http.NoBody)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPage, err)
	}

	request.AddCookie(
		&http.Cookie{Name: cookieName, Value: s.token},
	) // #nosec G124 -- a request's cookie; the attributes are a response's

	if _, err := s.do(request); err != nil {
		return err
	}

	s.password = password

	return s.Logout(ctx)
}

// CheckPassword is the rule the page enforces on a new password: 8 to 20
// characters, with a lower-case letter, an upper-case letter, a digit
// and a character that is none of those and not a space.
func CheckPassword(password string) error {
	if len(password) < passwordMin || len(password) > passwordMax {
		return fmt.Errorf("%w: %d to %d characters", ErrPassword, passwordMin, passwordMax)
	}

	var lower, upper, digit, special bool

	for _, char := range password {
		switch {
		case char >= 'a' && char <= 'z':
			lower = true
		case char >= 'A' && char <= 'Z':
			upper = true
		case unicode.IsDigit(char):
			digit = true
		case char != ' ':
			special = true
		}
	}

	if !lower || !upper || !digit || !special {
		return fmt.Errorf(
			"%w: a lower-case letter, an upper-case letter, a digit and a special character",
			ErrPassword,
		)
	}

	return nil
}

// refusal tells ErrLocked from ErrRefused by the flag the page reads.
func (s *Session) refusal(ctx context.Context) error {
	flag, err := s.get(ctx, pageLock, "", false)
	if err == nil && strings.TrimSpace(flag) == lockedFlag {
		return ErrLocked
	}

	return ErrRefused
}

// page fetches one page under the session's cookie and recognizes the
// login page in its place.
func (s *Session) page(ctx context.Context, name string) (string, error) {
	body, err := s.get(ctx, name, "", false)
	if err != nil {
		return "", err
	}

	if strings.Contains(body, loginTitle) {
		return "", ErrSessionLost
	}

	return body, nil
}

// get fetches a page; query, when set, is the raw query string, and
// authorize sends it as Basic authorization too, the login's shape.
func (s *Session) get(ctx context.Context, name, query string, authorize bool) (string, error) {
	target := s.base.JoinPath(name)
	target.RawQuery = query

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrPage, err)
	}

	if authorize {
		request.Header.Set("Authorization", "Basic "+query)
	}

	if s.token != "" {
		request.AddCookie(
			&http.Cookie{Name: cookieName, Value: s.token},
		) // #nosec G124 -- a request's cookie; the attributes are a response's
	}

	return s.do(request)
}

// do sends the request and returns the body of a 200.
func (s *Session) do(request *http.Request) (string, error) {
	response, err := s.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%s: %w", request.URL.Path, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody))
	if err != nil {
		return "", fmt.Errorf("%s: %w", request.URL.Path, err)
	}

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: %s %d", ErrStatus, request.URL.Path, response.StatusCode)
	}

	return string(body), nil
}
