// Package cli is the arris command: the SB8200's pages as a person reads
// them, and the password change. The modem's URL, user and TLS pin are
// flags with environment variables; the passwords come from the
// environment only.
package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"text/tabwriter"
	"time"

	"github.com/alecthomas/kong"

	"github.com/duncanite/go-arris/sb8200"
)

// Exit statuses.
const (
	ExitOK    = 0
	ExitError = 1
)

// requestTimeout bounds one call to the modem; it answers a page in
// under a second, the event log in a few.
const requestTimeout = 30 * time.Second

// The passwords' variables; neither is a flag.
const passwordVar = "ARRIS_PASSWORD" // #nosec G101 -- the variable's name, not a credential

const newPasswordVar = "ARRIS_NEW_PASSWORD" // #nosec G101 -- the variable's name, not a credential

var errVerify = errors.New("the modem took the new password, and refused a login with it")

// exitStatus is how the parser's own exit, on --help, leaves Run: kong
// calls its exit function and goes on, so the function panics with the
// status and Run recovers it.
type exitStatus int

type rootCommand struct {
	URL         string `default:"https://192.168.100.1"                                            env:"ARRIS_URL"                                                                      help:"The modem's page: scheme and host."`
	Username    string `default:"admin"                                                            env:"ARRIS_USERNAME"                                                                 help:"The administrator account."`
	Fingerprint string `env:"ARRIS_TLS_FINGERPRINT"                                                help:"SHA-256 of the modem's certificate, hex, pinned in place of the system roots."`
	Insecure    bool   `help:"Accept any certificate; for the run that reads the fingerprint off."`

	Status         statusCommand      `cmd:"" help:"Print the startup procedure, the bonded channels and the modem's clock."`
	Info           infoCommand        `cmd:"" help:"Print the product information: specification, versions, MAC, serial, uptime."`
	Log            logCommand         `cmd:"" help:"Print the event log, newest first."`
	ChangePassword passwordCommand    `cmd:"" help:"Change the account's password. The new one comes from ARRIS_NEW_PASSWORD."    name:"password"`
	FingerprintOf  fingerprintCommand `cmd:"" help:"Print the SHA-256 of the certificate the modem presents, to pin."             name:"fingerprint"`

	out io.Writer
	// password is ARRIS_PASSWORD, read in login: never a flag, so it is
	// never in a process list or a shell history.
	password string
}

type (
	statusCommand      struct{}
	infoCommand        struct{}
	logCommand         struct{}
	passwordCommand    struct{}
	fingerprintCommand struct{}
)

// Run parses args and runs the command, printing to stdout and errors to
// stderr, and returns the exit status.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) (status int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			exited, ok := recovered.(exitStatus)
			if !ok {
				panic(recovered)
			}

			status = int(exited)
		}
	}()

	root := rootCommand{out: stdout}

	parser, err := kong.New(&root,
		kong.Name("arris"),
		kong.Description("Read the SURFboard SB8200 cable modem through its page. "+
			"The account's password comes from ARRIS_PASSWORD, never an argument."),
		kong.UsageOnError(),
		kong.Writers(stdout, stderr),
		kong.Exit(func(code int) { panic(exitStatus(code)) }),
		kong.BindTo(ctx, (*context.Context)(nil)),
	)
	if err != nil {
		fmt.Fprintln(stderr, "arris:", err)

		return ExitError
	}

	kctx, err := parser.Parse(args)
	if err != nil {
		parser.Errorf("%s", err)

		return ExitError
	}

	if err := kctx.Run(&root); err != nil {
		fmt.Fprintln(stderr, "arris:", err)

		return ExitError
	}

	return ExitOK
}

// login opens the session the reading commands share; the caller logs
// out.
func (root *rootCommand) login(ctx context.Context) (*sb8200.Session, error) {
	root.password = os.Getenv(passwordVar)
	if root.password == "" {
		return nil, fmt.Errorf("%w: %s is empty", sb8200.ErrRefused, passwordVar)
	}

	client, err := sb8200.HTTPClient(root.Fingerprint, root.Insecure, requestTimeout)
	if err != nil {
		return nil, err
	}

	return sb8200.Login(ctx, client, root.URL, root.Username, root.password)
}

// read runs visit under a session and logs out after it, whatever it did.
func (root *rootCommand) read(ctx context.Context, visit func(*sb8200.Session) error) error {
	session, err := root.login(ctx)
	if err != nil {
		return err
	}

	err = visit(session)

	if logoutErr := session.Logout(ctx); err == nil {
		err = logoutErr
	}

	return err
}

func (*statusCommand) Run(ctx context.Context, root *rootCommand) error {
	return root.read(ctx, func(session *sb8200.Session) error {
		status, err := session.Status(ctx)
		if err != nil {
			return err
		}

		root.printStatus(status)

		return nil
	})
}

func (root *rootCommand) printStatus(status *sb8200.Status) {
	// A table per section: one tabwriter would align the startup steps'
	// columns with the channels'.
	table := root.table()

	fmt.Fprintln(table, "Startup")

	for _, step := range status.Startup {
		fmt.Fprintf(table, "  %s\t%s\t%s\n", step.Procedure, step.Status, step.Comment)
	}

	_ = table.Flush()
	table = root.table()

	fmt.Fprintf(table, "\nDownstream (%d channels)\n", len(status.Downstream))
	fmt.Fprintln(table, "  ID\tLock\tModulation\tFrequency\tPower\tSNR/MER\tCorrected\tUncorrectable")

	for _, channel := range status.Downstream {
		fmt.Fprintf(table, "  %d\t%s\t%s\t%d Hz\t%.1f dBmV\t%.1f dB\t%d\t%d\n",
			channel.ID, lock(channel.Locked), channel.Modulation, channel.Frequency,
			channel.Power, channel.SNR, channel.Corrected, channel.Uncorrectable)
	}

	_ = table.Flush()
	table = root.table()

	fmt.Fprintf(table, "\nUpstream (%d channels)\n", len(status.Upstream))
	fmt.Fprintln(table, "  Channel\tID\tLock\tType\tFrequency\tWidth\tPower")

	for _, channel := range status.Upstream {
		fmt.Fprintf(table, "  %d\t%d\t%s\t%s\t%d Hz\t%d Hz\t%.1f dBmV\n",
			channel.Channel, channel.ID, lock(channel.Locked), channel.Type,
			channel.Frequency, channel.Width, channel.Power)
	}

	_ = table.Flush()

	fmt.Fprintf(root.out, "\nSystem time: %s, by the modem's clock\n", status.SystemTime.Format(time.DateTime))
}

func (root *rootCommand) table() *tabwriter.Writer {
	return tabwriter.NewWriter(root.out, 0, 0, 2, ' ', 0)
}

func lock(locked bool) string {
	if locked {
		return "locked"
	}

	return "not locked"
}

func (*infoCommand) Run(ctx context.Context, root *rootCommand) error {
	return root.read(ctx, func(session *sb8200.Session) error {
		info, err := session.Info(ctx)
		if err != nil {
			return err
		}

		table := root.table()
		fmt.Fprintf(table, "Specification:\t%s\n", info.Specification)
		fmt.Fprintf(table, "Hardware:\t%s\n", info.Hardware)
		fmt.Fprintf(table, "Software:\t%s\n", info.Software)
		fmt.Fprintf(table, "MAC:\t%s\n", info.MAC)
		fmt.Fprintf(table, "Serial:\t%s\n", info.Serial)
		fmt.Fprintf(table, "Uptime:\t%s\n", info.Uptime)

		return table.Flush()
	})
}

func (*logCommand) Run(ctx context.Context, root *rootCommand) error {
	return root.read(ctx, func(session *sb8200.Session) error {
		events, err := session.Log(ctx)
		if err != nil {
			return err
		}

		table := root.table()

		for _, event := range events {
			fmt.Fprintf(table, "%s\t%d\t%d\t%s\n",
				event.Time.Format("2006-01-02 15:04"), event.ID, event.Level, event.Text)
		}

		return table.Flush()
	})
}

// Run changes the password and proves it: a login with the new one, the
// only confirmation the modem gives.
func (*passwordCommand) Run(ctx context.Context, root *rootCommand) error {
	newPassword := os.Getenv(newPasswordVar)
	if err := sb8200.CheckPassword(newPassword); err != nil {
		return fmt.Errorf("%s: %w", newPasswordVar, err)
	}

	session, err := root.login(ctx)
	if err != nil {
		return err
	}

	if err = session.ChangePassword(ctx, newPassword); err != nil {
		return err
	}

	// The proof: a login under the new password, which login reads from
	// the environment, so the environment is set to it for this process.
	if err = os.Setenv(passwordVar, newPassword); err != nil {
		return fmt.Errorf("%w: %w", errVerify, err)
	}

	err = root.read(ctx, func(*sb8200.Session) error { return nil })
	if err != nil {
		return fmt.Errorf("%w: %w", errVerify, err)
	}

	fmt.Fprintln(root.out, "Password changed; a login with the new password succeeded.")

	return nil
}

// Run connects without verifying and prints the leaf certificate's
// fingerprint, for ARRIS_TLS_FINGERPRINT.
func (*fingerprintCommand) Run(ctx context.Context, root *rootCommand) error {
	target, err := url.Parse(root.URL)
	if err != nil || target.Host == "" {
		return fmt.Errorf("%w: %q", sb8200.ErrURL, root.URL)
	}

	host := target.Host
	if _, _, splitErr := net.SplitHostPort(host); splitErr != nil {
		host = net.JoinHostPort(host, "443")
	}

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: requestTimeout},
		Config: &tls.Config{
			InsecureSkipVerify: true, // #nosec G402 -- reading the certificate is the point
			MinVersion:         tls.VersionTLS12,
		},
	}

	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return fmt.Errorf("%s: %w", host, err)
	}
	defer func() { _ = conn.Close() }()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok || len(tlsConn.ConnectionState().PeerCertificates) == 0 {
		return fmt.Errorf("%w: %s presented no certificate", sb8200.ErrFingerprint, host)
	}

	fmt.Fprintln(root.out, sb8200.Fingerprint(tlsConn.ConnectionState().PeerCertificates[0].Raw))

	return nil
}
