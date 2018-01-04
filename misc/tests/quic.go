package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	quic "github.com/lucas-clemente/quic-go"
	"math/big"
	"sync"
	"time"
)

const addr = "[::1]:9001"

func main() {
	go run_server()
	run_client()
}

func run_server() {
	listener, err := quic.ListenAddr(addr, generateTLSConfig(), nil)
	if err != nil {
		panic(err)
	}
	ses, err := listener.Accept()
	if err != nil {
		panic(err)
	}
	for {
		stream, err := ses.AcceptStream()
		if err != nil {
			panic(err)
		}
		go func() {
			defer stream.Close()
			bs := bytes.Buffer{}
			_, err := bs.ReadFrom(stream)
			if err != nil {
				panic(err)
			} //<-- TooManyOpenStreams
		}()
	}
}

func run_client() {
	msgSize := 1048576
	msgCount := 128
	ses, err := quic.DialAddr(addr, &tls.Config{InsecureSkipVerify: true}, nil)
	if err != nil {
		panic(err)
	}
	bs := make([]byte, msgSize)
	wg := sync.WaitGroup{}
	start := time.Now()
	for idx := 0; idx < msgCount; idx++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stream, err := ses.OpenStreamSync()
			if err != nil {
				panic(err)
			}
			defer stream.Close()
			stream.Write(bs)
		}() // "go" this later
	}
	wg.Wait()
	timed := time.Since(start)
	fmt.Println("Client finished", timed, fmt.Sprintf("%f Bits/sec", 8*float64(msgSize*msgCount)/timed.Seconds()))
}

// Setup a bare-bones TLS config for the server
func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{tlsCert}}
}
