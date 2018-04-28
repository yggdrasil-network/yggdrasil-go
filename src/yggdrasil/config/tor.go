package config

// TorConfig is the configuration structure for Tor Proxy related values
type TorConfig struct {
	OnionKeyfile string // hidden service private key for ADD_ONION (currently unimplemented)
	ControlAddr  string // tor control port address
	Enabled      bool
}
