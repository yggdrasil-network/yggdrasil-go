package config

// NodeConfig defines all configuration values needed to run a signle yggdrasil node
type NodeConfig struct {
	Listen                      string   `comment:"Listen address for peer connections. Default is to listen for all\nTCP connections over IPv4 and IPv6 with a random port."`
	AdminListen                 string   `comment:"Listen address for admin connections Default is to listen for local\nconnections either on TCP/9001 or a UNIX socket depending on your\nplatform. Use this value for yggdrasilctl -endpoint=X."`
	Peers                       []string `comment:"List of connection strings for static peers in URI format, i.e.\ntcp://a.b.c.d:e or socks://a.b.c.d:e/f.g.h.i:j"`
	ReadTimeout                 int32    `comment:"Read timeout for connections, specified in milliseconds. If less than 6000 and not negative, 6000 (the default) is used. If negative, reads won't time out."`
	AllowedEncryptionPublicKeys []string `comment:"List of peer encryption public keys to allow or incoming TCP\nconnections from. If left empty/undefined then all connections\nwill be allowed by default."`
	EncryptionPublicKey         string   `comment:"Your public encryption key. Your peers may ask you for this to put\ninto their AllowedEncryptionPublicKeys configuration."`
	EncryptionPrivateKey        string   `comment:"Your private encryption key. DO NOT share this with anyone!"`
	SigningPublicKey            string   `comment:"Your public signing key. You should not ordinarily need to share\nthis with anyone."`
	SigningPrivateKey           string   `comment:"Your private signing key. DO NOT share this with anyone!"`
	MulticastInterfaces         []string `comment:"Regular expressions for which interfaces multicast peer discovery\nshould be enabled on. If none specified, multicast peer discovery is\ndisabled. The default value is .* which uses all interfaces."`
	IfName                      string   `comment:"Local network interface name for TUN/TAP adapter, or \"auto\" to select\nan interface automatically, or \"none\" to run without TUN/TAP."`
	IfTAPMode                   bool     `comment:"Set local network interface to TAP mode rather than TUN mode if\nsupported by your platform - option will be ignored if not."`
	IfMTU                       int      `comment:"Maximux Transmission Unit (MTU) size for your local TUN/TAP interface.\nDefault is the largest supported size for your platform. The lowest\npossible value is 1280."`
	//Net                         NetConfig `comment:"Extended options for connecting to peers over other networks."`
}

// NetConfig defines network/proxy related configuration values
type NetConfig struct {
	Tor TorConfig `comment:"Experimental options for configuring peerings over Tor."`
	I2P I2PConfig `comment:"Experimental options for configuring peerings over I2P."`
}
