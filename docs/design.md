# go-arris: the design

A Go module and one command, `arris`, for the SURFboard SB8200 through the one interface
it has, its admin page ([protocol.md](protocol.md)). Run from the modem's network. It
reads, and it changes the password; the modem offers nothing else to change.

## Shape

- `sb8200`: `Login` is the page's login, and returns a `Session`; `Status`, `Info` and
  `Log` are the three pages the modem fills in, parsed into typed values; `ChangePassword`
  is the page's one write, after `CheckPassword`, the page's own rule; `Logout` ends the
  session. `HTTPClient` is the client `Login` takes, with the modem's self-signed
  certificate pinned by fingerprint, or accepted blind for the run that reads the
  fingerprint off. Errors are sentinels: `ErrRefused`, `ErrLocked`, `ErrSessionLost`,
  `ErrPage`, `ErrPassword`, `ErrFingerprint`, `ErrURL`, `ErrStatus`.
- `sb8200/sb8200test`: a modem for tests. A TLS server that logs in, serves the pages one
  modem served, with its serial and MAC addresses replaced, ends sessions, locks, and
  takes a password change, so a test and the command exercise the whole flow without a
  modem.
- `cmd/arris` and `internal/cli`: the command, kong under primordium's process setup.
  `status`, `info`, `log`, `password`, `fingerprint`. Each run is one login and one
  logout.

## Decisions

- **The page is the specification, read by position.** The cells carry no ids or classes,
  so a table is found by the text of its first cell and its columns are read by index,
  with the count of cells checked on every row. A firmware that moves a table fails
  loudly as `ErrPage` with the cell it choked on, never a wrong number read from the
  wrong column. The captured pages are the fixtures; a new firmware line is a new
  capture and, if it differs, a new parser.
- **An HTML parser, not regular expressions.** The markup is not well formed and the
  page's own script relies on a browser repairing it; `golang.org/x/net/html` repairs it
  the same way. The standard library has no HTML parser, so this is the one dependency
  the protocol buys.
- **Typed values, the page's words kept.** Frequencies in Hz as integers, power and SNR
  as floats, counters unsigned, uptime a `Duration`, clocks a `time.Time`. Where the page
  uses a word (`Other` for the OFDM channel's modulation, `SC-QAM Upstream`), the word is
  kept rather than translated: the page is the reference a person compares against.
- **Times in UTC, meaning the modem's clock.** The page prints no zone. Guessing the
  host's would be a guess; the values are the modem's wall clock, in UTC for want of
  anything better, and say so.
- **One login per run, a logout after.** The modem keeps a session open and can refuse
  the next login while it does; the command logs out whatever happened, and `Login`
  logs out first, as the page does.
- **Passwords from the environment only.** `ARRIS_PASSWORD` and `ARRIS_NEW_PASSWORD`,
  never arguments: an argument is in the process list and the shell history. The page's
  protocol then puts the credential in a query string, which is the page's design and
  is said in the protocol document.
- **The change is proven, not assumed.** The modem answers nothing readable to the
  password post. `password` logs in with the new password after the change, and says
  so; a failure there is reported as the modem having taken the change and refused the
  login, which is what the person needs to know.
- **No reboot, no configuration.** On the firmware seen, the configuration page's fields
  are disabled and the reboot button is commented out of the markup. The module does
  not post to a form the page does not show. Link aggregation has a page and an apply
  button; it reboots the modem and was left alone as out of scope.
- **The certificate is pinned.** The modem's certificate is self-signed and the system
  roots refuse it. `fingerprint` reads it once; `ARRIS_TLS_FINGERPRINT` pins it from
  then on; `--insecure` is for that first run and nothing else.
