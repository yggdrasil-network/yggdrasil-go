package config

// TorConfig is the configuration structure for Tor Proxy related values
type TorConfig struct {
	OnionKeyfile string // hidden service private key for ADD_ONION (currently unimplemented)
	SocksAddr    string // tor socks address
	UseForAll    bool   // use tor proxy for all connections?
	Enabled      bool   // use tor at all ?
}
