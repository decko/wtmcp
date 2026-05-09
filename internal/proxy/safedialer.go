package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"time"
)

// safeDialer wraps net.Dialer to reject connections to private,
// loopback, link-local, and unspecified IP addresses at connection
// time. This prevents SSRF via DNS rebinding — the IP validation
// happens on the same DNS resolution used for the actual connection,
// eliminating the TOCTOU window of a separate pre-check.
type safeDialer struct {
	dialer       net.Dialer
	allowPrivate bool // true for plugins with allow_private_ips + allowed_domains
}

// DialContext resolves the host, validates all IPs against the
// private/loopback denylist, then connects to the first valid IP.
func (d *safeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}

	if !d.allowPrivate {
		for _, ipStr := range ips {
			if err := checkIP(ipStr); err != nil {
				return nil, fmt.Errorf("SSRF blocked: host %s: %w", host, err)
			}
		}
	}

	// Connect using the validated IPs directly
	var lastErr error
	for _, ipStr := range ips {
		conn, err := d.dialer.DialContext(ctx, network, net.JoinHostPort(ipStr, port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("connect to %s: %w", addr, lastErr)
}

// Reserved IP ranges not covered by Go's net.IP methods.
var (
	cgnatNet     = mustParseCIDR("100.64.0.0/10") // RFC 6598: shared/CGNAT
	benchmarkNet = mustParseCIDR("198.18.0.0/15") // RFC 2544: benchmarking
	sixToFourNet = mustParseCIDR("2002::/16")     // RFC 3056: 6to4 (deprecated by RFC 7526)
	teredoNet    = mustParseCIDR("2001::/32")     // RFC 4380: Teredo tunneling
)

func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic("invalid CIDR: " + s)
	}
	return n
}

// checkIP rejects private, loopback, link-local, unspecified,
// CGNAT, and benchmarking addresses, including IPv6-mapped IPv4.
func checkIP(ipStr string) error {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return fmt.Errorf("unparseable IP address %q", ipStr)
	}

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("resolves to private/loopback address %s", ipStr)
	}

	if cgnatNet.Contains(ip) || benchmarkNet.Contains(ip) ||
		sixToFourNet.Contains(ip) || teredoNet.Contains(ip) {
		return fmt.Errorf("resolves to reserved address %s", ipStr)
	}

	// IPv6-mapped IPv4 (::ffff:x.x.x.x): Go's IsPrivate only checks
	// IPv4 ranges against 4-byte IPs. A 16-byte representation like
	// ::ffff:10.0.0.1 would slip through the check above.
	if ipv4 := ip.To4(); ipv4 != nil && len(ip) == net.IPv6len {
		if ipv4.IsLoopback() || ipv4.IsPrivate() || ipv4.IsLinkLocalUnicast() ||
			ipv4.IsMulticast() || ipv4.IsUnspecified() {
			return fmt.Errorf("resolves to private/loopback address %s", ipStr)
		}
		if cgnatNet.Contains(ipv4) || benchmarkNet.Contains(ipv4) {
			return fmt.Errorf("resolves to reserved address %s", ipStr)
		}
	}

	return nil
}

// safeTransport returns an http.Transport that uses a safeDialer
// to reject connections to private/loopback IPs.
func safeTransport(allowPrivate bool) *http.Transport {
	return &http.Transport{
		DialContext:           (&safeDialer{dialer: net.Dialer{Timeout: 30 * time.Second}, allowPrivate: allowPrivate}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

// SafeTransportWithTLS returns a transport with custom TLS config
// for plugins that need private CAs, mTLS, or hostname skip.
// Uses pre-loaded CACertPEM bytes (not file path) to prevent TOCTOU.
func SafeTransportWithTLS(allowPrivate bool, tlsCfg TLSConfig) (*http.Transport, error) {
	transport := safeTransport(allowPrivate)

	if !tlsCfg.HasConfig() {
		return transport, nil
	}

	goTLS := &tls.Config{MinVersion: tls.VersionTLS12}

	// Load CA pool once — used by both RootCAs and chain verifier
	var caPool *x509.CertPool
	if len(tlsCfg.CACertPEM) > 0 {
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool() // fallback: custom CA only
		}
		if !pool.AppendCertsFromPEM(tlsCfg.CACertPEM) {
			return nil, fmt.Errorf("no valid PEM certificates in ca_cert")
		}
		caPool = pool
		goTLS.RootCAs = pool
	}

	if tlsCfg.ClientCert != "" {
		cert, err := tls.LoadX509KeyPair(tlsCfg.ClientCert, tlsCfg.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("load client cert/key: %w", err)
		}
		goTLS.Certificates = []tls.Certificate{cert}
	}

	// Skip hostname verification but still verify certificate chain.
	// This is safer than bare InsecureSkipVerify which skips everything.
	if tlsCfg.SkipHostnameVerify {
		goTLS.InsecureSkipVerify = true                         //nolint:gosec // hostname skip with chain verification below
		goTLS.VerifyPeerCertificate = makeChainVerifier(caPool) //nolint:gosec // intentional: hostname skip with full chain verification
	}

	transport.TLSClientConfig = goTLS
	return transport, nil
}

// makeChainVerifier returns a VerifyPeerCertificate function that
// verifies the certificate chain against the CA pool but does NOT
// check hostname. This is used with InsecureSkipVerify to skip only
// hostname matching while still validating the certificate chain.
func makeChainVerifier(roots *x509.CertPool) func([][]byte, [][]*x509.Certificate) error {
	// Explicitly load system pool when roots is nil — fail loudly
	// rather than silently accepting any certificate.
	if roots == nil {
		var err error
		roots, err = x509.SystemCertPool()
		if err != nil {
			return func([][]byte, [][]*x509.Certificate) error {
				return fmt.Errorf("no CA pool available for chain verification: %w", err)
			}
		}
	}

	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("no certificates presented")
		}

		certs := make([]*x509.Certificate, len(rawCerts))
		for i, raw := range rawCerts {
			cert, err := x509.ParseCertificate(raw)
			if err != nil {
				return fmt.Errorf("parse certificate: %w", err)
			}
			certs[i] = cert
		}

		opts := x509.VerifyOptions{
			Roots:         roots,
			Intermediates: x509.NewCertPool(),
		}
		for _, cert := range certs[1:] {
			opts.Intermediates.AddCert(cert)
		}

		_, err := certs[0].Verify(opts)
		return err
	}
}
