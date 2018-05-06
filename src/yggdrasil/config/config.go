package config

// NodeConfig defines all configuration values needed to run a signle yggdrasil node
type NodeConfig struct {
	Listen      string
	AdminListen string
	Peers       []string
	PeerBoxPubs []string
	BoxPub      string
	BoxPriv     string
	SigPub      string
	SigPriv     string
	Multicast   bool
	LinkLocal   string
	IfName      string
	IfTAPMode   bool
	IfMTU       int
	Net         NetConfig
}

// NetConfig defines network/proxy related configuration values
type NetConfig struct {
	Tor TorConfig
	I2P I2PConfig
}
