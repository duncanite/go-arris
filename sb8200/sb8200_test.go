package sb8200_test

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/duncanite/go-arris/sb8200"
	"github.com/duncanite/go-arris/sb8200/sb8200test"
)

const timeout = 5 * time.Second

func client(t *testing.T, modem *sb8200test.Modem) *http.Client {
	t.Helper()

	httpClient, err := sb8200.HTTPClient(modem.Fingerprint(), false, timeout)
	if err != nil {
		t.Fatal(err)
	}

	return httpClient
}

func login(t *testing.T, modem *sb8200test.Modem) *sb8200.Session {
	t.Helper()

	session, err := sb8200.Login(
		t.Context(),
		client(t, modem),
		modem.URL(),
		sb8200test.Username,
		modem.CurrentPassword(),
	)
	if err != nil {
		t.Fatal(err)
	}

	return session
}

// TestTheReader: a login, the three pages as a person reads them, and
// the logout.
func TestTheReader(t *testing.T) {
	t.Parallel()

	modem := sb8200test.New()
	defer modem.Close()

	session := login(t, modem)

	if modem.Logouts() != 1 || modem.Logins() != 1 {
		t.Fatalf("login: %d logouts before, %d logins", modem.Logouts(), modem.Logins())
	}

	status, err := session.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(status.Startup) != 6 || status.Startup[0].Procedure != "Acquire Downstream Channel" ||
		status.Startup[0].Status != "789000000 Hz" || status.Startup[0].Comment != "Locked" {
		t.Fatalf("startup: %+v", status.Startup)
	}

	if len(status.Downstream) != 29 || len(status.Upstream) != 3 {
		t.Fatalf("channels: %d down, %d up", len(status.Downstream), len(status.Upstream))
	}

	first := status.Downstream[0]
	if first.ID != 27 || !first.Locked || first.Modulation != "QAM256" || first.Frequency != 789000000 ||
		first.Power != 1.9 || first.SNR != 40.6 || first.Corrected != 454 || first.Uncorrectable != 1202 {
		t.Fatalf("downstream[0]: %+v", first)
	}

	ofdm := status.Downstream[28]
	if ofdm.ID != 33 || ofdm.Modulation != "Other" || ofdm.Frequency != 402000000 || ofdm.Corrected != 2317957349 ||
		ofdm.Uncorrectable != 4326 {
		t.Fatalf("downstream OFDM: %+v", ofdm)
	}

	up := status.Upstream[2]
	if up.Channel != 3 || up.ID != 3 || !up.Locked || up.Type != "OFDM Upstream" ||
		up.Frequency != 4600000 || up.Width != 16200000 || up.Power != 28 {
		t.Fatalf("upstream[2]: %+v", up)
	}

	if want := time.Date(2026, time.November, 12, 19, 32, 59, 0, time.UTC); !status.SystemTime.Equal(want) {
		t.Fatalf("system time: %s", status.SystemTime)
	}

	info, err := session.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	want := sb8200.Info{
		Specification: "Docsis 3.1", Hardware: "7", Software: "AB01.02.053.01_112320_193.0A.NSH",
		MAC: "02:00:00:00:00:02", Serial: "11K000000000000",
		Uptime: 3*24*time.Hour + 21*time.Hour + 38*time.Minute + 33*time.Second,
	}
	if *info != want {
		t.Fatalf("info: %+v", *info)
	}

	events, err := session.Log(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 49 {
		t.Fatalf("events: %d", len(events))
	}

	event := events[0]
	if !event.Time.Equal(time.Date(2026, time.November, 12, 18, 23, 0, 0, time.UTC)) ||
		event.ID != 67050300 || event.Level != 5 ||
		event.Text != "DBC-REQ Mismatch Between Calculated Value for P1.6hi Compared to CCAP Provided Value;"+
			"CM-MAC=02:00:00:00:00:02;CMTS-MAC=02:00:00:00:00:01;CM-QOS=1.1;CM-VER=3.1;" {
		t.Fatalf("events[0]: %+v", event)
	}

	if err := session.Logout(t.Context()); err != nil {
		t.Fatal(err)
	}

	if modem.LoggedIn() {
		t.Fatal("logout left the session open")
	}

	if _, err := session.Status(t.Context()); !errors.Is(err, sb8200.ErrSessionLost) {
		t.Fatalf("status after logout: %v", err)
	}
}

// TestTheRefusals: wrong credentials, the lock, and a session the modem
// ended.
func TestTheRefusals(t *testing.T) {
	t.Parallel()

	modem := sb8200test.New()
	defer modem.Close()

	httpClient := client(t, modem)

	_, err := sb8200.Login(t.Context(), httpClient, modem.URL(), sb8200test.Username, "wrong")
	if !errors.Is(err, sb8200.ErrRefused) {
		t.Fatalf("wrong password: %v", err)
	}

	session := login(t, modem)

	modem.Lock()

	_, err = sb8200.Login(t.Context(), httpClient, modem.URL(), sb8200test.Username, modem.CurrentPassword())
	if !errors.Is(err, sb8200.ErrLocked) {
		t.Fatalf("locked: %v", err)
	}

	// The lock is on logins; the page the login's logout ended is gone
	// with it.
	if _, err = session.Info(t.Context()); !errors.Is(err, sb8200.ErrSessionLost) {
		t.Fatalf("after another login's logout: %v", err)
	}

	pinned, err := sb8200.HTTPClient(strings.Repeat("0", 64), false, timeout)
	if err != nil {
		t.Fatal(err)
	}

	_, err = sb8200.Login(t.Context(), pinned, modem.URL(), sb8200test.Username, modem.CurrentPassword())
	if !errors.Is(err, sb8200.ErrFingerprint) {
		t.Fatalf("another certificate's fingerprint: %v", err)
	}
}

// TestThePasswordChange: the page's rule, the change, and the new
// password the next login takes.
func TestThePasswordChange(t *testing.T) {
	t.Parallel()

	modem := sb8200test.New()
	defer modem.Close()

	for _, bad := range []string{"Short1!", "nouppercase1!", "NOLOWERCASE1!", "NoDigits!!", "NoSpecial11", "Space is no 1"} {
		if err := sb8200.CheckPassword(bad); !errors.Is(err, sb8200.ErrPassword) {
			t.Fatalf("%q: %v", bad, err)
		}
	}

	session := login(t, modem)

	if err := session.ChangePassword(t.Context(), sb8200test.Password); !errors.Is(err, sb8200.ErrPassword) {
		t.Fatalf("same password: %v", err)
	}

	logouts := modem.Logouts()

	if err := session.ChangePassword(t.Context(), "Another1!"); err != nil {
		t.Fatal(err)
	}

	// The page logs out after the change, and so does this.
	if modem.Logouts() != logouts+1 {
		t.Fatalf("logouts across the change: %d, want one", modem.Logouts()-logouts)
	}

	if modem.LoggedIn() || modem.CurrentPassword() != "Another1!" {
		t.Fatalf("after the change: logged in %v, password %q", modem.LoggedIn(), modem.CurrentPassword())
	}

	httpClient := client(t, modem)

	if _, err := sb8200.Login(
		t.Context(),
		httpClient,
		modem.URL(),
		sb8200test.Username,
		sb8200test.Password,
	); !errors.Is(
		err,
		sb8200.ErrRefused,
	) {
		t.Fatalf("old password: %v", err)
	}

	// A change under a session the modem has ended gets the same answer
	// and changes nothing: only a login tells.
	stale, err := sb8200.Login(t.Context(), httpClient, modem.URL(), sb8200test.Username, "Another1!")
	if err != nil {
		t.Fatal(err)
	}

	// The next login's logout ends it.
	again, err := sb8200.Login(t.Context(), httpClient, modem.URL(), sb8200test.Username, "Another1!")
	if err != nil {
		t.Fatal(err)
	}

	if err = stale.ChangePassword(t.Context(), "Unheard1!"); err != nil {
		t.Fatalf("a change under an ended session: %v", err)
	}

	if modem.CurrentPassword() != "Another1!" {
		t.Fatalf("an ended session changed the password to %q", modem.CurrentPassword())
	}

	if err = again.Logout(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestTheBetterServer: the modem's RSA suite is offered, and a server
// that has better is not given it: against one on Go's default suites
// the exchange is an ECDHE one.
func TestTheBetterServer(t *testing.T) {
	t.Parallel()

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}
	server.StartTLS()

	defer server.Close()

	httpClient, err := sb8200.HTTPClient("", true, timeout)
	if err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	response, err := httpClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	_ = response.Body.Close()

	if suite := tls.CipherSuiteName(response.TLS.CipherSuite); !strings.HasPrefix(suite, "TLS_ECDHE_") {
		t.Fatalf("a server on Go's defaults negotiated %s", suite)
	}
}

// TestTheClient: the fingerprint's forms, and insecure.
func TestTheClient(t *testing.T) {
	t.Parallel()

	modem := sb8200test.New()
	defer modem.Close()

	if _, err := sb8200.HTTPClient("not hex", false, timeout); !errors.Is(err, sb8200.ErrFingerprint) {
		t.Fatalf("bad fingerprint: %v", err)
	}

	insecure, err := sb8200.HTTPClient("", true, timeout)
	if err != nil {
		t.Fatal(err)
	}

	session, err := sb8200.Login(t.Context(), insecure, modem.URL(), sb8200test.Username, sb8200test.Password)
	if err != nil {
		t.Fatal(err)
	}

	if err = session.Logout(t.Context()); err != nil {
		t.Fatal(err)
	}

	roots, err := sb8200.HTTPClient("", false, timeout)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sb8200.Login(t.Context(), roots, modem.URL(), sb8200test.Username, sb8200test.Password); err == nil {
		t.Fatal("the system roots accepted a self-signed certificate")
	}
}

// TestThePageShapes: a page not in the shape this package knows is
// ErrPage, never a wrong value or a panic: a table shorter than its
// header, a row short of a cell, a cell in the wrong unit.
func TestThePageShapes(t *testing.T) {
	t.Parallel()

	const (
		startup    = `<table><tr><th colspan="3">Startup Procedure</th></tr><tr><td>Procedure</td><td>Status</td><td>Comment</td></tr>`
		downstream = `<table><tr><th colspan="8">Downstream Bonded Channels</strong></th></tr>` +
			`<td>Channel ID</td><td>Lock Status</td><td>Modulation</td><td>Frequency</td><td>Power</td><td>SNR/MER</td><td>Corrected</td><td>Uncorrectables</td></tr>`
		upstream = `<table><tr><th colspan="7">Upstream Bonded Channels</strong></th></tr>` +
			`<td>Channel</td><td>Channel ID</td><td>Lock Status</td><td>US Channel Type</td><td>Frequency</td><td>Width</td><td>Power</td></tr>`
		downRow = `<tr><td>1</td><td>Locked</td><td>QAM256</td><td>633000000 Hz</td><td>0.8 dBmV</td><td>40.0 dB</td><td>514</td><td>1650</td></tr>`
		log     = `<table><tr><th>Date Time</th><th>Event ID</th><th>Event Level</th><th>Description</th></tr>`
		// clock is the paragraph under the tables; a page with every table
		// and the clock has one fault, the one the row names.
		clock = `<p><strong>Current System Time:</strong> Thu Nov 12 19:32:59 2026</p>`
	)

	modem := sb8200test.New()
	defer modem.Close()

	session := login(t, modem)

	for name, page := range map[string]string{
		"startup of one row":      `<table><tr><th>Startup Procedure</th></tr></table>`,
		"startup row of two":      startup + `<tr><td>Boot State</td><td>OK</td></tr></table>`,
		"downstream of one row":   startup + `</table><table><tr><th>Downstream Bonded Channels</th></tr></table>`,
		"downstream row of seven": startup + `</table>` + downstream + `<tr><td>1</td><td>Locked</td><td>QAM256</td><td>633000000 Hz</td><td>0.8 dBmV</td><td>40.0 dB</td><td>514</td></tr></table>`,
		"power in volts": startup + `</table>` + downstream +
			`<tr><td>1</td><td>Locked</td><td>QAM256</td><td>633000000 Hz</td><td>0.8 V</td><td>40.0 dB</td><td>514</td><td>1650</td></tr></table>` +
			upstream + `</table>` + clock,
		"upstream of one row": startup + `</table>` + downstream + downRow + `</table><table><tr><th>Upstream Bonded Channels</th></tr></table>`,
		"upstream row of six": startup + `</table>` + downstream + downRow + `</table>` + upstream + `<tr><td>1</td><td>1</td><td>Locked</td><td>SC-QAM Upstream</td><td>30400000 Hz</td><td>6400000 Hz</td></tr></table>`,
		"no clock":            startup + `</table>` + downstream + downRow + `</table>` + upstream + `</table>`,
	} {
		modem.SetPage("cmconnectionstatus.html", page)

		if _, err := session.Status(t.Context()); !errors.Is(err, sb8200.ErrPage) {
			t.Fatalf("%s: %v", name, err)
		}
	}

	for name, page := range map[string]string{
		"no log table":       `<table><tr><th>Information</th></tr></table>`,
		"event row of three": log + `<tr><td>11/12/2026 18:23</td><td>67050300</td><td>5</td></tr></table>`,
		"event out of time":  log + `<tr><td>yesterday</td><td>67050300</td><td>5</td><td>x</td></tr></table>`,
	} {
		modem.SetPage("cmeventlog.html", page)

		if _, err := session.Log(t.Context()); !errors.Is(err, sb8200.ErrPage) {
			t.Fatalf("%s: %v", name, err)
		}
	}

	modem.SetPage("cmeventlog.html", log+`</table>`)

	events, err := session.Log(t.Context())
	if err != nil || len(events) != 0 {
		t.Fatalf("empty log: %v, %d events", err, len(events))
	}

	for name, page := range map[string]string{
		"no information": `<table><tr><th>Status</th></tr><tr><td>Up Time</td><td>1 days 0h:0m:1s.00</td></tr></table>`,
		"no serial":      `<table><tr><th>Information</th></tr><tr><td>Software Version</td><td>x</td></tr></table><table><tr><th>Status</th></tr><tr><td>Up Time</td><td>1 days 0h:0m:1s.00</td></tr></table>`,
		"uptime in words": `<table><tr><th>Information</th></tr>` +
			`<tr><td>Standard Specification Compliant</td><td>a</td></tr><tr><td>Hardware Version</td><td>b</td></tr><tr><td>Software Version</td><td>c</td></tr>` +
			`<tr><td>Cable Modem MAC Address</td><td>d</td></tr><tr><td>Serial Number</td><td>e</td></tr></table>` +
			`<table><tr><th>Status</th></tr><tr><td>Up Time</td><td>three days</td></tr></table>`,
	} {
		modem.SetPage("cmswinfo.html", page)

		if _, err := session.Info(t.Context()); !errors.Is(err, sb8200.ErrPage) {
			t.Fatalf("%s: %v", name, err)
		}
	}

	if _, err := sb8200.Login(
		t.Context(),
		client(t, modem),
		"nowhere",
		sb8200test.Username,
		sb8200test.Password,
	); !errors.Is(
		err,
		sb8200.ErrURL,
	) {
		t.Fatalf("no host: %v", err)
	}
}

// TestTheCredentialStaysOut: a login or a password change the modem
// drops fails with an error that holds no credential. Both carry it in
// the query, and the HTTP client's own error quotes the URL.
func TestTheCredentialStaysOut(t *testing.T) {
	t.Parallel()

	modem := sb8200test.New()
	defer modem.Close()

	session := login(t, modem)

	modem.Drop()

	leaks := func(err error, password string) bool {
		credential := base64.StdEncoding.EncodeToString([]byte(sb8200test.Username + ":" + password))

		return strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), "?")
	}

	err := session.ChangePassword(t.Context(), "Another1!")
	if err == nil || leaks(err, "Another1!") || !strings.Contains(err.Error(), "/changepwd_tab.html") {
		t.Fatalf("a dropped change: %v", err)
	}

	_, err = sb8200.Login(t.Context(), client(t, modem), modem.URL(), sb8200test.Username, sb8200test.Password)
	if err == nil || leaks(err, sb8200test.Password) || !strings.Contains(err.Error(), "/cmconnectionstatus.html") {
		t.Fatalf("a dropped login: %v", err)
	}
}
