package config

// NodeConfig defines all configuration values needed to run a signle yggdrasil node
type NodeConfig struct {
	Listen         string    `comment:"Listen address for peer connections (default is to listen for all\nconnections over IPv4 and IPv6)"`
	AdminListen    string    `comment:"Listen address for admin connections (default is to listen only\nfor local connections)"`
	Peers          []string  `comment:"List of connection strings for static peers (i.e. tcp://a.b.c.d:e)"`
	AllowedBoxPubs []string  `json:"AllowedEncryptionPublicKeys" comment:"List of peer encryption public keys to allow UDP incoming TCP connections from\n(if left empty/undefined then connections will be allowed by default)"`
	BoxPub         string    `json:"EncryptionPublicKey" comment:"Your public encryption key (your peers may ask you for this to put\ninto their AllowedEncryptionPublicKeys configuration)"`
	BoxPriv        string    `json:"EncryptionPrivateKey" comment:"Your private encryption key (do not share this with anyone!)"`
	SigPub         string    `json:"SigningPublicKey" comment:"Your public signing key"`
	SigPriv        string    `json:"SigningPrivateKey" comment:"Your private signing key (do not share this with anyone!)"`
	Multicast      bool      `json:"MulticastEnabled,omitempty" comment:"Enable or disable automatic peer discovery on the same LAN using multicast"`
	LinkLocal      []string  `json:"MulticastInterfaces" comment:"Regexes for which interfaces multicast peer discovery should be enabled\non. If none specified, multicast peer discovery is disabled"`
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
