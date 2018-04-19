package config

/**
* This is a very crude wrapper around src/yggdrasil
* It can generate a new config (--genconf)
* It can read a config from stdin (--useconf)
* It can run with an automatic config (--autoconf)
 */

type NodeConfig struct {
	Listen      string
	AdminListen string
	Peers       []string
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

type NetConfig struct {
	Tor TorConfig
	I2P I2PConfig
}
