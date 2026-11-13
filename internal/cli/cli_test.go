package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/duncanite/go-arris/internal/cli"
	"github.com/duncanite/go-arris/sb8200/sb8200test"
)

func run(t *testing.T, modem *sb8200test.Modem, args ...string) (string, int) {
	t.Helper()

	var out, errs bytes.Buffer

	status := cli.Run(
		t.Context(),
		append([]string{"--url", modem.URL(), "--fingerprint", modem.Fingerprint()}, args...),
		&out,
		&errs,
	)

	return out.String() + errs.String(), status
}

// TestTheCommands: every reading command against the modem, its output
// as a person reads it, each run a login and a logout.
func TestTheCommands(t *testing.T) {
	modem := sb8200test.New()
	defer modem.Close()

	t.Setenv("ARRIS_PASSWORD", sb8200test.Password)

	for _, step := range []struct {
		args []string
		want []string
	}{
		{[]string{"status"}, []string{
			"Acquire Downstream Channel", "789000000 Hz  Locked",
			"Downstream (29 channels)",
			"27  locked  QAM256      789000000 Hz  1.9 dBmV  40.6 dB  454",
			"Upstream (3 channels)",
			"3        3   locked  OFDM Upstream    4600000 Hz   16200000 Hz  28.0 dBmV",
			"System time: 2026-11-12 19:32:59, by the modem's clock",
		}},
		{[]string{"info"}, []string{
			"Software:       AB01.02.053.01_112320_193.0A.NSH",
			"Serial:         11K000000000000",
			"Uptime:         93h38m33s",
		}},
		{[]string{"log"}, []string{"2026-11-12 18:23  67050300  5  DBC-REQ Mismatch"}},
		{[]string{"fingerprint"}, []string{modem.Fingerprint()}},
	} {
		out, status := run(t, modem, step.args...)
		if status != cli.ExitOK {
			t.Fatalf("%v: status %d, output:\n%s", step.args, status, out)
		}

		for _, want := range step.want {
			if !strings.Contains(out, want) {
				t.Fatalf("%v: output lacks %q:\n%s", step.args, want, out)
			}
		}

		if modem.LoggedIn() {
			t.Fatalf("%v left the session open", step.args)
		}
	}

	if modem.Logins() != 3 {
		t.Fatalf("logins: %d", modem.Logins())
	}
}

// TestTheErrors: what a person sees when it goes wrong.
func TestTheErrors(t *testing.T) {
	modem := sb8200test.New()
	defer modem.Close()

	t.Setenv("ARRIS_PASSWORD", "")

	out, status := run(t, modem, "status")
	if status != cli.ExitError || !strings.Contains(out, "ARRIS_PASSWORD is empty") {
		t.Fatalf("no password: %d\n%s", status, out)
	}

	// Neither password is a flag, hidden or otherwise.
	out, status = run(t, modem, "--password", "x", "status")
	if status != cli.ExitError || !strings.Contains(out, "unknown flag --password") {
		t.Fatalf("--password: %d\n%s", status, out)
	}

	out, status = run(t, modem, "password", "--new-password", "Another1!")
	if status != cli.ExitError || !strings.Contains(out, "unknown flag --new-password") {
		t.Fatalf("--new-password: %d\n%s", status, out)
	}

	t.Setenv("ARRIS_PASSWORD", "wrong")

	out, status = run(t, modem, "info")
	if status != cli.ExitError || !strings.Contains(out, "the modem refused the login") {
		t.Fatalf("wrong password: %d\n%s", status, out)
	}

	t.Setenv("ARRIS_PASSWORD", sb8200test.Password)
	t.Setenv("ARRIS_NEW_PASSWORD", "weak")

	out, status = run(t, modem, "password")
	if status != cli.ExitError ||
		!strings.Contains(out, "ARRIS_NEW_PASSWORD: the password does not meet the page's rule") {
		t.Fatalf("weak password: %d\n%s", status, out)
	}

	modem.Lock()

	out, status = run(t, modem, "log")
	if status != cli.ExitError || !strings.Contains(out, "locked its page") {
		t.Fatalf("locked: %d\n%s", status, out)
	}
}

// TestThePasswordCommand: the change, proven by the login that follows.
func TestThePasswordCommand(t *testing.T) {
	modem := sb8200test.New()
	defer modem.Close()

	t.Setenv("ARRIS_PASSWORD", sb8200test.Password)
	t.Setenv("ARRIS_NEW_PASSWORD", "Another1!")

	out, status := run(t, modem, "password")
	if status != cli.ExitOK || !strings.Contains(out, "a login with the new password succeeded") {
		t.Fatalf("password: %d\n%s", status, out)
	}

	if modem.CurrentPassword() != "Another1!" || modem.LoggedIn() || modem.Logins() != 2 {
		t.Fatalf(
			"after: password %q, logged in %v, logins %d",
			modem.CurrentPassword(),
			modem.LoggedIn(),
			modem.Logins(),
		)
	}
}
