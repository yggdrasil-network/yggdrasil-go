package config

// NodeConfig defines all configuration values needed to run a signle yggdrasil node
type NodeConfig struct {
	Listen         string    `comment:"Listen address for peer connections (default is to listen for all\nconnections over IPv4 and IPv6)"`
	AdminListen    string    `comment:"Listen address for admin connections (default is to listen only\nfor local connections)"`
	Peers          []string  `comment:"List of connection strings for static peers (i.e. tcp://a.b.c.d:e)"`
	AllowedBoxPubs []string  `comment:"List of peer BoxPubs to allow UDP incoming TCP connections from\n(if left empty/undefined then connections will be allowed by default)"`
	BoxPub         string    `comment:"Your public encryption key (your peers may ask you for this to put\ninto their AllowedBoxPubs configuration)"`
	BoxPriv        string    `comment:"Your private encryption key (do not share this with anyone!)"`
	SigPub         string    `comment:"Your public signing key"`
	SigPriv        string    `comment:"Your private signing key (do not share this with anyone!)"`
	Multicast      bool      `comment:"Enable or disable automatic peer discovery on the same LAN using multicast"`
	LinkLocal      string    `comment:"Regex for which interfaces multicast peer discovery should be enabled on"`
	IfName         string    `comment:"Local network interface name for TUN/TAP adapter, or \"auto\", or \"none\""`
	IfTAPMode      bool      `comment:"Set local network interface to TAP mode rather than TUN mode (if supported\nby your platform, option will be ignored if not)"`
	IfMTU          int       `comment:"Maximux Transmission Unit (MTU) size for your local network interface"`
	Net            NetConfig `comment:"Extended options for interoperability with other networks"`
}

// NetConfig defines network/proxy related configuration values
type NetConfig struct {
	Tor TorConfig `comment:"Experimental options for configuring peerings over Tor"`
	I2P I2PConfig `comment:"Experimental options for configuring peerings over I2P"`
}
