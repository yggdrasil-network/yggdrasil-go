//go:build plan9

package config

// Sane defaults for the Plan 9 platform. The "default" options may be
// replaced by the running configuration.
//
// Plan 9 has no /etc; system configuration lives under /lib. There is
// no /var/run either, so the admin socket defaults to a local TCP
// listener (yggdrasilctl speaks the same protocol over it).
func getDefaults() platformDefaultParameters {
	return platformDefaultParameters{
		// Admin
		DefaultAdminListen: "tcp://localhost:9001",

		// Configuration (used for yggdrasilctl)
		DefaultConfigFile: "/lib/yggdrasil.conf",

		// Multicast interfaces. Discovery uses /net/udp directly on Plan 9.
		DefaultMulticastInterfaces: []MulticastInterfaceConfig{
			{Regex: ".*", Beacon: true, Listen: true},
		},

		// TUN. Plan 9 "pkt" packet interfaces default to a 4096-byte
		// MTU; the requested MTU is clamped to what the medium reports
		// (see tun_plan9.go), so 65535 is a safe upper bound here.
		MaximumIfMTU:  65535,
		DefaultIfMTU:  65535,
		DefaultIfName: "auto",
	}
}