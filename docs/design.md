# go-arris: the design

A Go module and one command, `arris`, for the SURFboard SB8200 through the one interface
it has, its admin page ([protocol.md](protocol.md)). Run from the modem's network. It
reads, and it changes the password; the modem offers nothing else to change.

## Shape

- `sb8200`: `Login` is the page's login, and returns a `Session`; `Status`, `Info` and
  `Log` are the three pages the modem fills in, parsed into typed values, each with a
  `Write` that prints it as a person reads it, so any command over the library prints
  the same; `ChangePassword`
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
- **One login per run, a logout after.** The command logs out whatever happened, so no
  session is left on the modem. `Login` sends no logout first: the modem's sessions do
  not exclude each other, and a logout could end someone else's.
- **The login page is loaded before the login.** After an idle spell the modem refuses a
  login from a client it has not just served its login page to, with nothing in the
  answer to tell that refusal from a wrong password. `Login` loads the page first, as a
  browser does, so a correct login is taken the first time and a wrong password stays
  one attempt. Asking a refused login again would also get through, at the price of a
  second attempt on every wrong password and on every login sent while the modem is
  locked; it was not chosen.
- **Passwords from the environment only.** `ARRIS_PASSWORD` and `ARRIS_NEW_PASSWORD`,
  never arguments: an argument is in the process list and the shell history. The page's
  protocol then puts the credential in a query string, which is the page's design and
  is said in the protocol document.
- **The change is proven, not assumed.** The modem was seen to answer a change it took
  with its login page, which elsewhere means a lost session, so the answer is not read.
  `password` logs in with the new password after the change, and says so; a failure
  there says that login failed and to try both passwords in the page, since which one
  holds is then unknown.
- **No reboot, no configuration.** On the firmware seen, the configuration page's fields
  are disabled and the reboot button is commented out of the markup. The module does
  not post to a form the page does not show. Link aggregation has a page and an apply
  button; it reboots the modem and was left alone as out of scope.
- **The modem's cipher suite, added to Go's.** The modem does RSA key exchange and
  nothing else, which Go dropped from its defaults for want of forward secrecy.
  `HTTPClient` offers Go's default suites and `TLS_RSA_WITH_AES_256_GCM_SHA384`, the one
  the modem was seen to take. Go's TLS ranks the suites itself, RSA key exchange below
  every ECDHE one whatever order they are given in, so the weaker exchange is used only
  with a server that has nothing better; a test against a server on Go's defaults checks
  it. The test modem speaks only the modem's suite, so a client without it fails every
  test.
- **The certificate is pinned.** The modem's certificate is self-signed and the system
  roots refuse it. `fingerprint` reads it once; `ARRIS_TLS_FINGERPRINT` pins it from
  then on; `--insecure` is for that first run and nothing else.
