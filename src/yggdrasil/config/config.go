package config

// NodeConfig defines all configuration values needed to run a signle yggdrasil node
type NodeConfig struct {
	Listen                      string              `comment:"Listen address for peer connections. Default is to listen for all\nTCP connections over IPv4 and IPv6 with a random port."`
	AdminListen                 string              `comment:"Listen address for admin connections Default is to listen for local\nconnections either on TCP/9001 or a UNIX socket depending on your\nplatform. Use this value for yggdrasilctl -endpoint=X."`
	Peers                       []string            `comment:"List of connection strings for static peers in URI format, i.e.\ntcp://a.b.c.d:e or socks://a.b.c.d:e/f.g.h.i:j."`
	InterfacePeers              map[string][]string `comment:"List of connection strings for static peers in URI format, arranged\nby source interface, i.e. { \"eth0\": [ tcp://a.b.c.d:e ] }. Note that\nSOCKS peerings will NOT be affected by this option and should go in\nthe \"Peers\" section instead."`
	ReadTimeout                 int32               `comment:"Read timeout for connections, specified in milliseconds. If less\nthan 6000 and not negative, 6000 (the default) is used. If negative,\nreads won't time out."`
	DSCPClassification          uint8               `comment:"DSCP classification, in decimal, for applying Quality of Service\n(QoS) markings to outbound peering traffic. This value will more\nthan likely be ignored if peering over the Internet."`
	AllowedEncryptionPublicKeys []string            `comment:"List of peer encryption public keys to allow or incoming TCP\nconnections from. If left empty/undefined then all connections\nwill be allowed by default."`
	EncryptionPublicKey         string              `comment:"Your public encryption key. Your peers may ask you for this to put\ninto their AllowedEncryptionPublicKeys configuration."`
	EncryptionPrivateKey        string              `comment:"Your private encryption key. DO NOT share this with anyone!"`
	SigningPublicKey            string              `comment:"Your public signing key. You should not ordinarily need to share\nthis with anyone."`
	SigningPrivateKey           string              `comment:"Your private signing key. DO NOT share this with anyone!"`
	MulticastInterfaces         []string            `comment:"Regular expressions for which interfaces multicast peer discovery\nshould be enabled on. If none specified, multicast peer discovery is\ndisabled. The default value is .* which uses all interfaces."`
	IfName                      string              `comment:"Local network interface name for TUN/TAP adapter, or \"auto\" to select\nan interface automatically, or \"none\" to run without TUN/TAP."`
	IfTAPMode                   bool                `comment:"Set local network interface to TAP mode rather than TUN mode if\nsupported by your platform - option will be ignored if not."`
	IfMTU                       int                 `comment:"Maximux Transmission Unit (MTU) size for your local TUN/TAP interface.\nDefault is the largest supported size for your platform. The lowest\npossible value is 1280."`
	SessionFirewall             SessionFirewall     `comment:"The session firewall controls who can send/receive network traffic\nto/from. This is useful if you want to protect this node without\nresorting to using a real firewall. This does not affect traffic\nbeing routed via this node to somewhere else. Rules are prioritised as\nfollows: blacklist, whitelist, always allow outgoing, direct, remote."`
	//Net                         NetConfig `comment:"Extended options for connecting to peers over other networks."`
}

// NetConfig defines network/proxy related configuration values
type NetConfig struct {
	Tor TorConfig `comment:"Experimental options for configuring peerings over Tor."`
	I2P I2PConfig `comment:"Experimental options for configuring peerings over I2P."`
}

type SessionFirewall struct {
	Enable                        bool     `comment:"Enable or disable the session firewall. If disabled, network traffic\nfrom any node will be allowed. If enabled, the below rules apply."`
	AllowFromDirect               bool     `comment:"Allow network traffic from directly connected peers."`
	AllowFromRemote               bool     `comment:"Allow network traffic from remote nodes on the network that you are\nnot directly peered with."`
	AlwaysAllowOutbound           bool     `comment:"Allow outbound network traffic regardless of AllowFromDirect or\nAllowFromRemote. This does allow a remote node to send unsolicited\ntraffic back to you for the length of the session."`
	WhitelistEncryptionPublicKeys []string `comment:"List of public keys from which network traffic is always accepted,\nregardless of AllowFromDirect or AllowFromRemote."`
	BlacklistEncryptionPublicKeys []string `comment:"List of public keys from which network traffic is always rejected,\nregardless of the whitelist, AllowFromDirect or AllowFromRemote."`
}
