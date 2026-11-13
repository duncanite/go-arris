# go-arris

A Go client for the Arris SURFboard SB8200 cable modem, read over the LAN. This is the
repository's baseline; the code follows.

The modem publishes no API. Its admin page is HTML behind a token login, and the module
is a client for that page: the downstream and upstream channels with their lock,
frequency, power, signal-to-noise and error counts; the product information with model,
serial, firmware and uptime; the event log; and the one action the page offers, a reboot.
There is no setting on the modem to read back and change, so the module reads and
reboots, nothing more.
