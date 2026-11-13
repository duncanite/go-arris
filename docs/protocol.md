# What the SB8200's page speaks

The Arris SURFboard SB8200 has no API. Its one interface is the admin page its web server
serves, and this is what that page does on the wire, read from the page's own scripts and
from what one modem answered. The modem: hardware version 7, firmware
`AB01.02.053.01_112320_193.0A.NSH`, retail (`customerID.txt` is `1`). Other firmware lines
log in differently; they are noted at the end, unseen.

## The server

`micro_httpd`, HTTPS on port 443 with a self-signed certificate, TLS 1.2 with RSA key
exchange only: it picks `TLS_RSA_WITH_AES_256_GCM_SHA384`, a suite Go's TLS no longer
offers by default, so a Go client has to ask for it or the handshake fails. Arris documents the page
at `192.168.100.1`; an operator's provisioning can put it elsewhere. Every answer is a
`200`, the login page included: a refused login, a page asked without a session, and any
path the modem does not know all come back as the login page, `<title>Login</title>`,
with a `200`. The HNAP endpoint newer SURFboards have, `/HNAP1/`, answers the login page
too: there is none.

The server's `Set-Cookie` header is malformed (`HttpOnly: true, Secure: true`, no name
and no value), so a client gets no cookie from it; the page's script sets the one cookie
that matters by hand.

## Login

The login page's script (`validate()` on `/`) does this:

1. `credential = base64(username + ":" + password)`.
2. `GET /cmconnectionstatus.html?<credential>`, with `Authorization: Basic <credential>`
   as well. The credential is the whole query string, no key.
3. The answer's body is the session token, 31 characters, nothing else. The script stores
   it as the `credential` cookie.
4. Every page after that is a plain `GET` with `Cookie: credential=<token>`.

A wrong password gets the login page back, `200`. `GET /LoginLock.txt` says `Locked` or
`NotLocked`, and the login page reads it to word its message: when it says `Locked`, the
page prints that access has timed out due to invalid login attempts and to try again
after five minutes. That wording is the only source here for what the lock is and how
long it lasts; neither was measured.

`GET /logout.html` ends the session; the page's own logout does it before leaving. Which
sessions a logout without a cookie ends was not seen.

Sessions do not exclude each other. Seen against the modem: a login right after a login,
with no logout between, gets its own token, and the earlier token still reads pages.

After the modem has sat idle, a login sent with nothing before it is refused whatever
the credentials, and the same login asked again at once is taken. Seen twice, after
fifteen and after ten minutes idle, each with only a read of `LoginLock.txt` sent before
the login, so that read does not count; the shortest idle that does it was not measured.
The refusal is the login page with a `200`, the same answer as a wrong password, and
`LoginLock.txt` stays `NotLocked` through it. A login sent after fifteen minutes idle
with a `GET /` before it, the login page as a browser loads it, was taken at once.

Those three things were seen: the cold login refused, twice; the same login asked again
taken, twice; the login after `GET /` taken, once. The reading of them is that the modem
takes a login only from a client it has recently served its login page to, and that a
refusal, being that page, is what lets the next attempt through. That reading was not
isolated: which client, how recently, and whether the refusal's page or the page served
for another path counts were not tested. A client loads `/` before it logs in, which
rests on the one thing seen.

`LoginLock.txt` was read as `Locked` twice, thirteen minutes apart, on an evening when
no wrong password is known to have been sent, and as `NotLocked` two minutes after the
second reading, following a login through the page. Nothing was read between the two,
so whether it stayed `Locked` or was set twice is not known; what sets it and what
clears it is not established.

The credential is in the query string of the login request, so it is in whatever logs
the path: the modem's, a proxy's, a shell history if typed. That is the page's design,
not a choice here.

## Pages

The menu (`menuCM` in `main_arris.js`) names seven pages, all behind the session.

| page | what it holds |
| --- | --- |
| `cmconnectionstatus.html` | Startup procedure (six rows: procedure, status, comment); downstream bonded channels (id, lock, modulation, frequency, power, SNR/MER, corrected, uncorrectables); upstream bonded channels (channel, id, lock, type, frequency, width, power); the modem's clock as `Current System Time`. |
| `cmswinfo.html` | Specification, hardware version, software version, cable modem MAC, serial; uptime as `N days HHh:MMm:SSs.00`. |
| `cmeventlog.html` | The events the modem keeps, newest first, 49 in the capture: date and time to the minute, DOCSIS event id, level, text; the text carries the modem's and the CMTS's MAC addresses. |
| `cmaddress.html` | Serial and HFC MAC again; the CPE table is empty. |
| `cmconfiguration.html` | Downstream frequency, upstream channel id, frequency plan. On this firmware every field is `disabled`, the Energy Efficient Ethernet row and the reboot button are commented out of the markup. Nothing to do here. |
| `lagcfg.html` | Link aggregation, one checkbox and an apply that reboots the modem. Not driven by this module. |
| `changepwd_tab.html` | The password change, below. |

The tables are plain `<table class="simpleTable">` rows with no ids or classes on the
cells; a parser finds a table by the text of its first cell and reads its cells by
position. The markup is not well formed, a header row without its `<tr>`, a `</td>`
closing a `<th>`; an HTML5 parser repairs it as a browser does.

Units on the page: frequency and width in `Hz`, power in `dBmV`, SNR in `dB`, all as
`<number> <unit>`. The downstream OFDM channel's modulation is the word `Other`; the
upstream types are `SC-QAM Upstream` and `OFDM Upstream`. Times carry no zone.

## The password change

The page's script (`validate()` on `changepwd_tab.html`) checks the current password
against what the browser stored at login, never against the modem, then:

1. `credential = base64(username + ":" + newPassword)`.
2. `POST /changepwd_tab.html?<credential>`, no body, under the session cookie.
3. Logs out. The modem answered a change it took with a page titled `Login`, the page
   that elsewhere means the session is gone, so the answer cannot be relied on and the
   next login is the proof. What it answers to a change it does not take was not seen.

The rule the script enforces, and the modem does not: 8 to 20 characters, at least one
lower-case letter, one upper-case letter, one digit and one character that is none of
those and not a space; not the current password. The username is `admin` and cannot be
changed.

## Other firmware lines, unseen

Readers of other SB8200 units describe two more login flows, neither seen here:

- Firmware from September 2021 on (`AB01.02.053.05_051921` and later) logs in with
  `?login_<credential>` and reads pages as `?ct_<token>` with a `sessionId` cookie.
- Firmware before October 2020 had no login at all; the status page answered anyone.

The SB6183 and SB6190 share this page's design without the login. The S33, S34 and the
G34 and G36 gateways speak HNAP instead, JSON over `/HNAP1/` with an HMAC challenge, a
different protocol this module does not cover.

Sources: the page's own `main_arris.js`, `/` and `changepwd_tab.html`;
[cable-modem-stats](https://github.com/sarabveer/cable-modem-stats) for the 2020 and 2021
variants; [sb820_prometheus_exporter's
notes](https://github.com/kquinsland/sb820_prometheus_exporter) for the idle logout and
the 2021 flow.
