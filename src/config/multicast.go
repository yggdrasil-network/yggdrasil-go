package config

type MulticastConfig struct {
	MulticastInterfaces []string `comment:"Regular expressions for which interfaces multicast peer discovery\nshould be enabled on. If none specified, multicast peer discovery is\ndisabled. The default value is .* which uses all interfaces."`
}
