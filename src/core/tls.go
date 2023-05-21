package core

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

func (c *Core) generateTLSConfig(cert *tls.Certificate) (*tls.Config, error) {
	config := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		ClientAuth:   tls.RequireAnyClientCert,
		GetClientCertificate: func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return cert, nil
		},
		VerifyPeerCertificate: c.verifyTLSCertificate,
		VerifyConnection:      c.verifyTLSConnection,
		InsecureSkipVerify:    true,
		MinVersion:            tls.VersionTLS13,
		NextProtos:            []string{"yggdrasil/0.5"},
	}
	return config, nil
}

func (c *Core) verifyTLSCertificate(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if c.config.roots == nil {
		// If there's no certificate pool configured then we will
		// accept all TLS certificates.
		return nil
	}
	if len(rawCerts) == 0 {
		return fmt.Errorf("expected at least one certificate")
	}

	opts := x509.VerifyOptions{
		Roots: c.config.roots,
	}

	for i, rawCert := range rawCerts {
		if i == 0 {
			// The first certificate is the leaf certificate. All other
			// certificates in the list are intermediates, so add them
			// into the VerifyOptions.
			continue
		}
		cert, err := x509.ParseCertificate(rawCert)
		if err != nil {
			return fmt.Errorf("failed to parse intermediate certificate: %w", err)
		}
		opts.Intermediates.AddCert(cert)
	}

	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("failed to parse leaf certificate: %w", err)
	}

	_, err = cert.Verify(opts)
	return err
}

func (c *Core) verifyTLSConnection(cs tls.ConnectionState) error {
	return nil
}
