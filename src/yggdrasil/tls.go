package yggdrasil

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"time"
)

type tcptls struct {
	tcp         *tcp
	config      *tls.Config
	forDialer   *TcpUpgrade
	forListener *TcpUpgrade
}

func (t *tcptls) init(tcp *tcp) {
	t.tcp = tcp
	t.forDialer = &TcpUpgrade{
		upgrade: t.upgradeDialer,
		name:    "tls",
	}
	t.forListener = &TcpUpgrade{
		upgrade: t.upgradeListener,
		name:    "tls",
	}

	edpriv := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	copy(edpriv[:], tcp.links.core.sigPriv[:])

	certBuf := &bytes.Buffer{}

	// TODO: because NotAfter is finite, we should add some mechanism to regenerate the certificate and restart the listeners periodically for nodes with very high uptimes. Perhaps regenerate certs and restart listeners every few months or so.
	pubtemp := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: hex.EncodeToString(tcp.links.core.sigPub[:]),
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour * 24 * 365),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derbytes, err := x509.CreateCertificate(rand.Reader, &pubtemp, &pubtemp, edpriv.Public(), edpriv)
	if err != nil {
		log.Fatalf("Failed to create certificate: %s", err)
	}

	if err := pem.Encode(certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: derbytes}); err != nil {
		panic("failed to encode certificate into PEM")
	}

	cpool := x509.NewCertPool()
	cpool.AppendCertsFromPEM(derbytes)

	t.config = &tls.Config{
		RootCAs: cpool,
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{derbytes},
				PrivateKey:  edpriv,
			},
		},
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}
}

func (t *tcptls) upgradeListener(c net.Conn) (net.Conn, error) {
	conn := tls.Server(c, t.config)
	if err := conn.Handshake(); err != nil {
		return c, err
	}
	return conn, nil
}

func (t *tcptls) upgradeDialer(c net.Conn) (net.Conn, error) {
	conn := tls.Client(c, t.config)
	if err := conn.Handshake(); err != nil {
		return c, err
	}
	return conn, nil
}
