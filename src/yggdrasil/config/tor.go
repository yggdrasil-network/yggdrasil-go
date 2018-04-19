package config

/**
*tor specific configuration
 */
type TorConfig struct {
	OnionKeyfile string
	SocksAddr    string
	UseForAll    bool
	Enabled      bool
}
