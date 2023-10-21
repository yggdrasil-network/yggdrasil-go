package core

import (
	"crypto/tls"
	"crypto/x509"
)

func (c *Core) generateTLSConfig(cert *tls.Certificate) (*tls.Config, error) {
	config := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		ClientAuth:   tls.NoClientCert,
		GetClientCertificate: func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return cert, nil
		},
		VerifyPeerCertificate: c.verifyTLSCertificate,
		VerifyConnection:      c.verifyTLSConnection,
		InsecureSkipVerify:    true,
		MinVersion:            tls.VersionTLS13,
	}
	return config, nil
}

func (c *Core) verifyTLSCertificate(_ [][]byte, _ [][]*x509.Certificate) error {
	return nil
}

func (c *Core) verifyTLSConnection(_ tls.ConnectionState) error {
	return nil
}
