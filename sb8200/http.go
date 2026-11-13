package sb8200

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPClient returns a client for Login, with the modem's TLS in place of
// the process default and timeout bounding each call. The modem's
// certificate is self-signed, so the system roots alone refuse it:
// fingerprint, the certificate's SHA-256 in hex, pins it instead, and
// insecure accepts any certificate, for the run that reads the
// fingerprint off. With neither, the system roots apply.
func HTTPClient(fingerprint string, insecure bool, timeout time.Duration) (*http.Client, error) {
	tlsConfig, err := tlsConfigFor(fingerprint, insecure)
	if err != nil {
		return nil, err
	}

	cloner, ok := http.DefaultTransport.(interface{ Clone() *http.Transport })
	if !ok {
		return nil, fmt.Errorf("%w: the default transport cannot be cloned", ErrPage)
	}

	transport := cloner.Clone()
	transport.TLSClientConfig = tlsConfig

	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

// Fingerprint is the SHA-256 of a certificate in the hex HTTPClient pins.
func Fingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:])
}

// PeerFingerprint connects to the modem at baseURL without verifying its
// certificate and returns the fingerprint of the one it presents, for
// HTTPClient to pin.
func PeerFingerprint(ctx context.Context, baseURL string, timeout time.Duration) (string, error) {
	target, err := url.Parse(baseURL)
	if err != nil || target.Host == "" {
		return "", fmt.Errorf("%w: %q", ErrURL, baseURL)
	}

	host := target.Host
	if target.Port() == "" {
		host = net.JoinHostPort(target.Hostname(), "443")
	}

	config, err := tlsConfigFor("", true)
	if err != nil {
		return "", err
	}

	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: timeout}, Config: config}

	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return "", fmt.Errorf("%s: %w", host, err)
	}

	defer func() { _ = conn.Close() }()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok || len(tlsConn.ConnectionState().PeerCertificates) == 0 {
		return "", fmt.Errorf("%w: %s presented no certificate", ErrFingerprint, host)
	}

	return Fingerprint(tlsConn.ConnectionState().PeerCertificates[0].Raw), nil
}

// cipherSuites is Go's default list with the one the modem takes added.
// The modem's server does TLS 1.2 with RSA key exchange only and picks
// TLS_RSA_WITH_AES_256_GCM_SHA384, which Go no longer offers by default
// since it gives no forward secrecy; without it the handshake fails. Its
// place in this list means nothing: crypto/tls ignores the order and
// ranks RSA key exchange below every ECDHE suite itself, so a server
// that can do better does.
func cipherSuites() []uint16 {
	var suites []uint16

	for _, suite := range tls.CipherSuites() {
		suites = append(suites, suite.ID)
	}

	return append(suites, tls.TLS_RSA_WITH_AES_256_GCM_SHA384)
}

// tlsConfigFor verifies the modem's certificate by its fingerprint when
// one is given, by the system roots otherwise; insecure verifies nothing.
func tlsConfigFor(fingerprint string, insecure bool) (*tls.Config, error) {
	config := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: cipherSuites(), // #nosec G402 -- the modem speaks RSA key exchange and nothing else
	}

	if insecure {
		config.InsecureSkipVerify = true // #nosec G402 -- the caller asked for it, for the run that reads the fingerprint

		return config, nil
	}

	if fingerprint == "" {
		return config, nil
	}

	pin, err := hex.DecodeString(strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(fingerprint)))
	if err != nil || len(pin) != sha256.Size {
		return nil, fmt.Errorf("%w: fingerprint %q is not a hex SHA-256", ErrFingerprint, fingerprint)
	}

	config.InsecureSkipVerify = true // #nosec G402 -- VerifyConnection below pins the certificate instead
	config.VerifyConnection = func(state tls.ConnectionState) error {
		for _, cert := range state.PeerCertificates {
			if Fingerprint(cert.Raw) == hex.EncodeToString(pin) {
				return nil
			}
		}

		return fmt.Errorf("%w: the modem's certificate does not match", ErrFingerprint)
	}

	return config, nil
}
