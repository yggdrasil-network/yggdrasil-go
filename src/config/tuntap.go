package config

type TunTapConfig struct {
	IfName        string        `comment:"Local network interface name for TUN/TAP adapter, or \"auto\" to select\nan interface automatically, or \"none\" to run without TUN/TAP."`
	IfTAPMode     bool          `comment:"Set local network interface to TAP mode rather than TUN mode if\nsupported by your platform - option will be ignored if not."`
	IfMTU         int           `comment:"Maximux Transmission Unit (MTU) size for your local TUN/TAP interface.\nDefault is the largest supported size for your platform. The lowest\npossible value is 1280."`
	TunnelRouting TunnelRouting `comment:"Allow tunneling non-Yggdrasil traffic over Yggdrasil. This effectively\nallows you to use Yggdrasil to route to, or to bridge other networks,\nsimilar to a VPN tunnel. Tunnelling works between any two nodes and\ndoes not require them to be directly peered."`
}

// TunnelRouting contains the crypto-key routing tables for tunneling
type TunnelRouting struct {
	Enable            bool              `comment:"Enable or disable tunnel routing."`
	IPv6RemoteSubnets map[string]string `comment:"IPv6 subnets belonging to remote nodes, mapped to the node's public\nkey, e.g. { \"aaaa:bbbb:cccc::/e\": \"boxpubkey\", ... }"`
	IPv6LocalSubnets  []string          `comment:"IPv6 subnets belonging to this node's end of the tunnels. Only traffic\nfrom these ranges (or the Yggdrasil node's IPv6 address/subnet)\nwill be tunnelled."`
	IPv4RemoteSubnets map[string]string `comment:"IPv4 subnets belonging to remote nodes, mapped to the node's public\nkey, e.g. { \"a.b.c.d/e\": \"boxpubkey\", ... }"`
	IPv4LocalSubnets  []string          `comment:"IPv4 subnets belonging to this node's end of the tunnels. Only traffic\nfrom these ranges will be tunnelled."`
}
