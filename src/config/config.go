package config

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/defaults"
)

// NodeConfig defines all configuration values needed to run a signle yggdrasil node
type NodeConfig struct {
	Listen                      []string               `comment:"Listen addresses for peer connections. Default is to listen for all\nTCP connections over IPv4 and IPv6 with a random port."`
	AdminListen                 string                 `comment:"Listen address for admin connections. Default is to listen for local\nconnections either on TCP/9001 or a UNIX socket depending on your\nplatform. Use this value for yggdrasilctl -endpoint=X. To disable\nthe admin socket, use the value \"none\" instead."`
	Peers                       []string               `comment:"List of connection strings for static peers in URI format, e.g.\ntcp://a.b.c.d:e or socks://a.b.c.d:e/f.g.h.i:j."`
	InterfacePeers              map[string][]string    `comment:"List of connection strings for static peers in URI format, arranged\nby source interface, e.g. { \"eth0\": [ tcp://a.b.c.d:e ] }. Note that\nSOCKS peerings will NOT be affected by this option and should go in\nthe \"Peers\" section instead."`
	AllowedEncryptionPublicKeys []string               `comment:"List of peer encryption public keys to allow incoming TCP peering\nconnections from. If left empty/undefined then all connections will\nbe allowed by default. This does not affect outgoing peerings, nor\ndoes it affect link-local peers discovered via multicast."`
	EncryptionPublicKey         string                 `comment:"Your public encryption key. Your peers may ask you for this to put\ninto their AllowedEncryptionPublicKeys configuration."`
	EncryptionPrivateKey        string                 `comment:"Your private encryption key. DO NOT share this with anyone!"`
	SigningPublicKey            string                 `comment:"Your public signing key. You should not ordinarily need to share\nthis with anyone."`
	SigningPrivateKey           string                 `comment:"Your private signing key. DO NOT share this with anyone!"`
	MulticastInterfaces         []string               `comment:"Regular expressions for which interfaces multicast peer discovery\nshould be enabled on. If none specified, multicast peer discovery is\ndisabled. The default value is .* which uses all interfaces."`
	LinkLocalTCPPort            uint16                 `comment:"The port number to be used for the link-local TCP listeners for the\nconfigured MulticastInterfaces. This option does not affect listeners\nspecified in the Listen option. Unless you plan to firewall link-local\ntraffic, it is best to leave this as the default value of 0. This\noption cannot currently be changed by reloading config during runtime."`
	IfName                      string                 `comment:"Local network interface name for TUN/TAP adapter, or \"auto\" to select\nan interface automatically, or \"none\" to run without TUN/TAP."`
	IfTAPMode                   bool                   `comment:"Set local network interface to TAP mode rather than TUN mode if\nsupported by your platform - option will be ignored if not."`
	IfMTU                       int                    `comment:"Maximux Transmission Unit (MTU) size for your local TUN/TAP interface.\nDefault is the largest supported size for your platform. The lowest\npossible value is 1280."`
	SessionFirewall             SessionFirewall        `comment:"The session firewall controls who can send/receive network traffic\nto/from. This is useful if you want to protect this node without\nresorting to using a real firewall. This does not affect traffic\nbeing routed via this node to somewhere else. Rules are prioritised as\nfollows: blacklist, whitelist, always allow outgoing, direct, remote."`
	TunnelRouting               TunnelRouting          `comment:"Allow tunneling non-Yggdrasil traffic over Yggdrasil. This effectively\nallows you to use Yggdrasil to route to, or to bridge other networks,\nsimilar to a VPN tunnel. Tunnelling works between any two nodes and\ndoes not require them to be directly peered."`
	SwitchOptions               SwitchOptions          `comment:"Advanced options for tuning the switch. Normally you will not need\nto edit these options."`
	NodeInfoPrivacy             bool                   `comment:"By default, nodeinfo contains some defaults including the platform,\narchitecture and Yggdrasil version. These can help when surveying\nthe network and diagnosing network routing problems. Enabling\nnodeinfo privacy prevents this, so that only items specified in\n\"NodeInfo\" are sent back if specified."`
	NodeInfo                    map[string]interface{} `comment:"Optional node info. This must be a { \"key\": \"value\", ... } map\nor set as null. This is entirely optional but, if set, is visible\nto the whole network on request."`
}

// SessionFirewall controls the session firewall configuration
type SessionFirewall struct {
	Enable                        bool     `comment:"Enable or disable the session firewall. If disabled, network traffic\nfrom any node will be allowed. If enabled, the below rules apply."`
	AllowFromDirect               bool     `comment:"Allow network traffic from directly connected peers."`
	AllowFromRemote               bool     `comment:"Allow network traffic from remote nodes on the network that you are\nnot directly peered with."`
	AlwaysAllowOutbound           bool     `comment:"Allow outbound network traffic regardless of AllowFromDirect or\nAllowFromRemote. This does allow a remote node to send unsolicited\ntraffic back to you for the length of the session."`
	WhitelistEncryptionPublicKeys []string `comment:"List of public keys from which network traffic is always accepted,\nregardless of AllowFromDirect or AllowFromRemote."`
	BlacklistEncryptionPublicKeys []string `comment:"List of public keys from which network traffic is always rejected,\nregardless of the whitelist, AllowFromDirect or AllowFromRemote."`
}

// TunnelRouting contains the crypto-key routing tables for tunneling
type TunnelRouting struct {
	Enable           bool              `comment:"Enable or disable tunnel routing."`
	IPv6Destinations map[string]string `comment:"IPv6 CIDR subnets, mapped to the EncryptionPublicKey to which they\nshould be routed, e.g. { \"aaaa:bbbb:cccc::/e\": \"boxpubkey\", ... }"`
	IPv6Sources      []string          `comment:"Optional IPv6 source subnets which are allowed to be tunnelled in\naddition to this node's Yggdrasil address/subnet. If not\nspecified, only traffic originating from this node's Yggdrasil\naddress or subnet will be tunnelled."`
	IPv4Destinations map[string]string `comment:"IPv4 CIDR subnets, mapped to the EncryptionPublicKey to which they\nshould be routed, e.g. { \"a.b.c.d/e\": \"boxpubkey\", ... }"`
	IPv4Sources      []string          `comment:"IPv4 source subnets which are allowed to be tunnelled. Unlike for\nIPv6, this option is required for bridging IPv4 traffic. Only\ntraffic with a source matching these subnets will be tunnelled."`
}

// SwitchOptions contains tuning options for the switch
type SwitchOptions struct {
	MaxTotalQueueSize uint64 `comment:"Maximum size of all switch queues combined (in bytes)."`
}

// Generates default configuration. This is used when outputting the -genconf
// parameter and also when using -autoconf. The isAutoconf flag is used to
// determine whether the operating system should select a free port by itself
// (which guarantees that there will not be a conflict with any other services)
// or whether to generate a random port number. The only side effect of setting
// isAutoconf is that the TCP and UDP ports will likely end up with different
// port numbers.
func GenerateConfig(isAutoconf bool) *NodeConfig {
	// Generate encryption keys.
	bpub, bpriv := crypto.NewBoxKeys()
	spub, spriv := crypto.NewSigKeys()
	// Create a node configuration and populate it.
	cfg := NodeConfig{}
	if isAutoconf {
		cfg.Listen = []string{"tcp://[::]:0"}
	} else {
		r1 := rand.New(rand.NewSource(time.Now().UnixNano()))
		cfg.Listen = []string{fmt.Sprintf("tcp://[::]:%d", r1.Intn(65534-32768)+32768)}
	}
	cfg.AdminListen = defaults.GetDefaults().DefaultAdminListen
	cfg.EncryptionPublicKey = hex.EncodeToString(bpub[:])
	cfg.EncryptionPrivateKey = hex.EncodeToString(bpriv[:])
	cfg.SigningPublicKey = hex.EncodeToString(spub[:])
	cfg.SigningPrivateKey = hex.EncodeToString(spriv[:])
	cfg.Peers = []string{}
	cfg.InterfacePeers = map[string][]string{}
	cfg.AllowedEncryptionPublicKeys = []string{}
	cfg.MulticastInterfaces = []string{".*"}
	cfg.IfName = defaults.GetDefaults().DefaultIfName
	cfg.IfMTU = defaults.GetDefaults().DefaultIfMTU
	cfg.IfTAPMode = defaults.GetDefaults().DefaultIfTAPMode
	cfg.SessionFirewall.Enable = false
	cfg.SessionFirewall.AllowFromDirect = true
	cfg.SessionFirewall.AllowFromRemote = true
	cfg.SwitchOptions.MaxTotalQueueSize = 4 * 1024 * 1024
	cfg.NodeInfoPrivacy = false

	return &cfg
}
