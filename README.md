# go-arris

The Arris SURFboard SB8200 cable modem: a Go library and command for what its admin page
knows, read over the LAN. The modem publishes no API; its page is HTML behind a token
login, and this is a client for that page: the downstream and upstream channels with
their lock, frequency, power, signal-to-noise and error counts, the product information
with model, serial, firmware and uptime, the event log, and the one thing the page lets a
person change, the password.

The command, run from the modem's network, the password in the environment:

```bash
export ARRIS_URL=https://192.168.100.1          # where the page is; this is Arris's default
export ARRIS_PASSWORD='...'                     # never an argument
just arris fingerprint                          # the certificate's SHA-256, once
export ARRIS_TLS_FINGERPRINT=<that>             # pinned from then on
just arris status                               # startup, channels, the modem's clock
just arris info                                 # versions, MAC, serial, uptime
just arris log                                  # the event log, newest first
ARRIS_NEW_PASSWORD='...' just arris password    # change it, and prove it with a login
```

[docs/protocol.md](docs/protocol.md) is what the page does on the wire, read from its own
scripts and from one modem; [docs/design.md](docs/design.md) is the shape of the module
and the decisions behind it.

The library is `sb8200`: a `Session` from `Login`, `Status`, `Info` and `Log` as typed
values, `ChangePassword`, `Logout`; `sb8200test` is a modem for tests. `cmd/arris` and
`internal/cli` are the command around them.
