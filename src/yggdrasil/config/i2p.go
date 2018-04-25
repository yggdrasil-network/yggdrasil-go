package config

// I2PConfig is the configuration structure for i2p related configuration
type I2PConfig struct {
	Keyfile string // private key file or empty string for ephemeral keys
	Addr    string // address of i2p api connector
	Enabled bool
}
