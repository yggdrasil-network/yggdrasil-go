/*
The config package contains structures related to the configuration of an
Yggdrasil node.

The configuration contains, amongst other things, encryption keys which are used
to derive a node's identity, information about peerings and node information
that is shared with the network. There are also some module-specific options
related to TUN, multicast and the admin socket.

In order for a node to maintain the same identity across restarts, you should
persist the configuration onto the filesystem or into some configuration storage
so that the encryption keys (and therefore the node ID) do not change.

Note that Yggdrasil will automatically populate sane defaults for any
configuration option that is not provided.
*/
package config

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"os"
	"time"

	"github.com/hjson/hjson-go/v4"
	"golang.org/x/text/encoding/unicode"
)

// NodeConfig is the main configuration structure, containing configuration
// options that are necessary for an Yggdrasil node to run. You will need to
// supply one of these structs to the Yggdrasil core when starting a node.
type NodeConfig struct {
	PrivateKey          KeyBytes                   `json:",omitempty" comment:"Your private key. DO NOT share this with anyone!"`
	PrivateKeyPath      string                     `json:",omitempty" comment:"The path to your private key file in PEM format."`
	Certificate         *tls.Certificate           `json:"-"`
	Peers               []string                   `comment:"List of connection strings for outbound peer connections in URI format,\ne.g. tls://a.b.c.d:e or socks://a.b.c.d:e/f.g.h.i:j. These connections\nwill obey the operating system routing table, therefore you should\nuse this section when you may connect via different interfaces."`
	InterfacePeers      map[string][]string        `comment:"List of connection strings for outbound peer connections in URI format,\narranged by source interface, e.g. { \"eth0\": [ \"tls://a.b.c.d:e\" ] }.\nNote that SOCKS peerings will NOT be affected by this option and should\ngo in the \"Peers\" section instead."`
	Listen              []string                   `comment:"Listen addresses for incoming connections. You will need to add\nlisteners in order to accept incoming peerings from non-local nodes.\nMulticast peer discovery will work regardless of any listeners set\nhere. Each listener should be specified in URI format as above, e.g.\ntls://0.0.0.0:0 or tls://[::]:0 to listen on all interfaces."`
	AdminListen         string                     `json:",omitempty" comment:"Listen address for admin connections. Default is to listen for local\nconnections either on TCP/9001 or a UNIX socket depending on your\nplatform. Use this value for yggdrasilctl -endpoint=X. To disable\nthe admin socket, use the value \"none\" instead."`
	MulticastInterfaces []MulticastInterfaceConfig `comment:"Configuration for which interfaces multicast peer discovery should be\nenabled on. Each entry in the list should be a json object which may\ncontain Regex, Beacon, Listen, and Port. Regex is a regular expression\nwhich is matched against an interface name, and interfaces use the\nfirst configuration that they match gainst. Beacon configures whether\nor not the node should send link-local multicast beacons to advertise\ntheir presence, while listening for incoming connections on Port.\nListen controls whether or not the node listens for multicast beacons\nand opens outgoing connections."`
	AllowedPublicKeys   []string                   `comment:"List of peer public keys to allow incoming peering connections\nfrom. If left empty/undefined then all connections will be allowed\nby default. This does not affect outgoing peerings, nor does it\naffect link-local peers discovered via multicast."`
	IfName              string                     `comment:"Local network interface name for TUN adapter, or \"auto\" to select\nan interface automatically, or \"none\" to run without TUN."`
	IfMTU               uint64                     `comment:"Maximum Transmission Unit (MTU) size for your local TUN interface.\nDefault is the largest supported size for your platform. The lowest\npossible value is 1280."`
	LogLookups          bool                       `json:",omitempty"`
	NodeInfoPrivacy     bool                       `comment:"By default, nodeinfo contains some defaults including the platform,\narchitecture and Yggdrasil version. These can help when surveying\nthe network and diagnosing network routing problems. Enabling\nnodeinfo privacy prevents this, so that only items specified in\n\"NodeInfo\" are sent back if specified."`
	NodeInfo            map[string]interface{}     `comment:"Optional node info. This must be a { \"key\": \"value\", ... } map\nor set as null. This is entirely optional but, if set, is visible\nto the whole network on request."`
}

type MulticastInterfaceConfig struct {
	Regex    string
	Beacon   bool
	Listen   bool
	Port     uint16
	Priority uint64 // really uint8, but gobind won't export it
	Password string
}

// Generates default configuration and returns a pointer to the resulting
// NodeConfig. This is used when outputting the -genconf parameter and also when
// using -autoconf.
func GenerateConfig() *NodeConfig {
	// Get the defaults for the platform.
	defaults := GetDefaults()
	// Create a node configuration and populate it.
	cfg := new(NodeConfig)
	cfg.NewPrivateKey()
	cfg.Listen = []string{}
	cfg.AdminListen = defaults.DefaultAdminListen
	cfg.Peers = []string{}
	cfg.InterfacePeers = map[string][]string{}
	cfg.AllowedPublicKeys = []string{}
	cfg.MulticastInterfaces = defaults.DefaultMulticastInterfaces
	cfg.IfName = defaults.DefaultIfName
	cfg.IfMTU = defaults.DefaultIfMTU
	cfg.NodeInfoPrivacy = false
	if err := cfg.postprocessConfig(); err != nil {
		panic(err)
	}
	return cfg
}

func (cfg *NodeConfig) ReadFrom(r io.Reader) (int64, error) {
	conf, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	n := int64(len(conf))
	// If there's a byte order mark - which Windows 10 is now incredibly fond of
	// throwing everywhere when it's converting things into UTF-16 for the hell
	// of it - remove it and decode back down into UTF-8. This is necessary
	// because hjson doesn't know what to do with UTF-16 and will panic
	if bytes.Equal(conf[0:2], []byte{0xFF, 0xFE}) ||
		bytes.Equal(conf[0:2], []byte{0xFE, 0xFF}) {
		utf := unicode.UTF16(unicode.BigEndian, unicode.UseBOM)
		decoder := utf.NewDecoder()
		conf, err = decoder.Bytes(conf)
		if err != nil {
			return n, err
		}
	}
	// Generate a new configuration - this gives us a set of sane defaults -
	// then parse the configuration we loaded above on top of it. The effect
	// of this is that any configuration item that is missing from the provided
	// configuration will use a sane default.
	*cfg = *GenerateConfig()
	if err := cfg.UnmarshalHJSON(conf); err != nil {
		return n, err
	}
	return n, nil
}

func (cfg *NodeConfig) UnmarshalHJSON(b []byte) error {
	if err := hjson.Unmarshal(b, cfg); err != nil {
		return err
	}
	return cfg.postprocessConfig()
}

func (cfg *NodeConfig) postprocessConfig() error {
	if cfg.PrivateKeyPath != "" {
		cfg.PrivateKey = nil
		f, err := os.ReadFile(cfg.PrivateKeyPath)
		if err != nil {
			return err
		}
		if err := cfg.UnmarshalPEMPrivateKey(f); err != nil {
			return err
		}
	}
	switch {
	case cfg.Certificate == nil:
		// No self-signed certificate has been generated yet.
		fallthrough
	case !bytes.Equal(cfg.Certificate.PrivateKey.(ed25519.PrivateKey), cfg.PrivateKey):
		// A self-signed certificate was generated but the private
		// key has changed since then, possibly because a new config
		// was parsed.
		if err := cfg.GenerateSelfSignedCertificate(); err != nil {
			return err
		}
	}
	return nil
}

// RFC5280 section 4.1.2.5
var notAfterNeverExpires = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)

func (cfg *NodeConfig) GenerateSelfSignedCertificate() error {
	key, err := cfg.MarshalPEMPrivateKey()
	if err != nil {
		return err
	}
	cert, err := cfg.MarshalPEMCertificate()
	if err != nil {
		return err
	}
	tlsCert, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return err
	}
	cfg.Certificate = &tlsCert
	return nil
}

func (cfg *NodeConfig) MarshalPEMCertificate() ([]byte, error) {
	privateKey := ed25519.PrivateKey(cfg.PrivateKey)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: hex.EncodeToString(publicKey),
		},
		NotBefore:             time.Now(),
		NotAfter:              notAfterNeverExpires,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certbytes, err := x509.CreateCertificate(rand.Reader, cert, cert, publicKey, privateKey)
	if err != nil {
		return nil, err
	}

	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certbytes,
	}
	return pem.EncodeToMemory(block), nil
}

func (cfg *NodeConfig) NewPrivateKey() {
	_, spriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	cfg.PrivateKey = KeyBytes(spriv)
}

func (cfg *NodeConfig) MarshalPEMPrivateKey() ([]byte, error) {
	b, err := x509.MarshalPKCS8PrivateKey(ed25519.PrivateKey(cfg.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PKCS8 key: %w", err)
	}
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: b,
	}
	return pem.EncodeToMemory(block), nil
}

func (cfg *NodeConfig) UnmarshalPEMPrivateKey(b []byte) error {
	p, _ := pem.Decode(b)
	if p == nil {
		return fmt.Errorf("failed to parse PEM file")
	}
	if p.Type != "PRIVATE KEY" {
		return fmt.Errorf("unexpected PEM type %q", p.Type)
	}
	k, err := x509.ParsePKCS8PrivateKey(p.Bytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal PKCS8 key: %w", err)
	}
	key, ok := k.(ed25519.PrivateKey)
	if !ok {
		return fmt.Errorf("private key must be ed25519 key")
	}
	if len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("unexpected ed25519 private key length")
	}
	cfg.PrivateKey = KeyBytes(key)
	return nil
}

type KeyBytes []byte

func (k KeyBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(hex.EncodeToString(k))
}

func (k *KeyBytes) UnmarshalJSON(b []byte) error {
	var s string
	var err error
	if err = json.Unmarshal(b, &s); err != nil {
		return err
	}
	*k, err = hex.DecodeString(s)
	return err
}
