package config

// NodeConfig defines all configuration values needed to run a signle yggdrasil node
type NodeConfig struct {
	Peers                       []string               `comment:"List of connection strings for outbound peer connections in URI format,\ne.g. tcp://a.b.c.d:e or socks://a.b.c.d:e/f.g.h.i:j. These connections\nwill obey the operating system routing table, therefore you should\nuse this section when you may connect via different interfaces."`
	InterfacePeers              map[string][]string    `comment:"List of connection strings for outbound peer connections in URI format,\narranged by source interface, e.g. { \"eth0\": [ tcp://a.b.c.d:e ] }.\nNote that SOCKS peerings will NOT be affected by this option and should\ngo in the \"Peers\" section instead."`
	Listen                      []string               `comment:"Listen addresses for incoming connections. You will need to add\nlisteners in order to accept incoming peerings from non-local nodes.\nMulticast peer discovery will work regardless of any listeners set\nhere. Each listener should be specified in URI format as above, e.g.\ntcp://0.0.0.0:0 or tcp://[::]:0 to listen on all interfaces."`
	AdminListen                 string                 `comment:"Listen address for admin connections. Default is to listen for local\nconnections either on TCP/9001 or a UNIX socket depending on your\nplatform. Use this value for yggdrasilctl -endpoint=X. To disable\nthe admin socket, use the value \"none\" instead."`
	MulticastInterfaces         []string               `comment:"Regular expressions for which interfaces multicast peer discovery\nshould be enabled on. If none specified, multicast peer discovery is\ndisabled. The default value is .* which uses all interfaces."`
	AllowedEncryptionPublicKeys []string               `comment:"List of peer encryption public keys to allow incoming TCP peering\nconnections from. If left empty/undefined then all connections will\nbe allowed by default. This does not affect outgoing peerings, nor\ndoes it affect link-local peers discovered via multicast."`
	EncryptionPublicKey         string                 `comment:"Your public encryption key. Your peers may ask you for this to put\ninto their AllowedEncryptionPublicKeys configuration."`
	EncryptionPrivateKey        string                 `comment:"Your private encryption key. DO NOT share this with anyone!"`
	SigningPublicKey            string                 `comment:"Your public signing key. You should not ordinarily need to share\nthis with anyone."`
	SigningPrivateKey           string                 `comment:"Your private signing key. DO NOT share this with anyone!"`
	LinkLocalTCPPort            uint16                 `comment:"The port number to be used for the link-local TCP listeners for the\nconfigured MulticastInterfaces. This option does not affect listeners\nspecified in the Listen option. Unless you plan to firewall link-local\ntraffic, it is best to leave this as the default value of 0. This\noption cannot currently be changed by reloading config during runtime."`
	IfName                      string                 `comment:"Local network interface name for TUN/TAP adapter, or \"auto\" to select\nan interface automatically, or \"none\" to run without TUN/TAP."`
	IfTAPMode                   bool                   `comment:"Set local network interface to TAP mode rather than TUN mode if\nsupported by your platform - option will be ignored if not."`
	IfMTU                       int                    `comment:"Maximux Transmission Unit (MTU) size for your local TUN/TAP interface.\nDefault is the largest supported size for your platform. The lowest\npossible value is 1280."`
	TunnelRouting               TunnelRouting          `comment:"Allow tunneling non-Yggdrasil traffic over Yggdrasil. This effectively\nallows you to use Yggdrasil to route to, or to bridge other networks,\nsimilar to a VPN tunnel. Tunnelling works between any two nodes and\ndoes not require them to be directly peered."`
	SessionFirewall             SessionFirewall        `comment:"The session firewall controls who can send/receive network traffic\nto/from. This is useful if you want to protect this node without\nresorting to using a real firewall. This does not affect traffic\nbeing routed via this node to somewhere else. Rules are prioritised as\nfollows: blacklist, whitelist, always allow outgoing, direct, remote."`
	SwitchOptions               SwitchOptions          `comment:"Advanced options for tuning the switch. Normally you will not need\nto edit these options."`
	NodeInfoPrivacy             bool                   `comment:"By default, nodeinfo contains some defaults including the platform,\narchitecture and Yggdrasil version. These can help when surveying\nthe network and diagnosing network routing problems. Enabling\nnodeinfo privacy prevents this, so that only items specified in\n\"NodeInfo\" are sent back if specified."`
	NodeInfo                    map[string]interface{} `comment:"Optional node info. This must be a { \"key\": \"value\", ... } map\nor set as null. This is entirely optional but, if set, is visible\nto the whole network on request."`
}

// TunnelRouting contains the crypto-key routing tables for tunneling
type TunnelRouting struct {
	Enable            bool              `comment:"Enable or disable tunnel routing."`
	IPv6RemoteSubnets map[string]string `comment:"IPv6 subnets belonging to remote nodes, mapped to the node's public\nkey, e.g. { \"aaaa:bbbb:cccc::/e\": \"boxpubkey\", ... }"`
	IPv6LocalSubnets  []string          `comment:"IPv6 subnets belonging to this node's end of the tunnels. Only traffic\nfrom these ranges (or the Yggdrasil node's IPv6 address/subnet)\nwill be tunnelled."`
	IPv4RemoteSubnets map[string]string `comment:"IPv4 subnets belonging to remote nodes, mapped to the node's public\nkey, e.g. { \"a.b.c.d/e\": \"boxpubkey\", ... }"`
	IPv4LocalSubnets  []string          `comment:"IPv4 subnets belonging to this node's end of the tunnels. Only traffic\nfrom these ranges will be tunnelled."`
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

// SwitchOptions contains tuning options for the switch
type SwitchOptions struct {
	MaxTotalQueueSize uint64 `comment:"Maximum size of all switch queues combined (in bytes)."`
}
