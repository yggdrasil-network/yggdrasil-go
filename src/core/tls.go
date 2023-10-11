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
		NextProtos: []string{
			fmt.Sprintf("yggdrasil/%d.%d", ProtocolVersionMajor, ProtocolVersionMinor),
		},
	}
	return config, nil
}

func (c *Core) verifyTLSCertificate(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) != 1 {
		return fmt.Errorf("expected one certificate")
	}

	/*
		opts := x509.VerifyOptions{}
		cert, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("failed to parse leaf certificate: %w", err)
		}

		_, err = cert.Verify(opts)
		return err
	*/

	return nil
}

func (c *Core) verifyTLSConnection(cs tls.ConnectionState) error {
	return nil
}
