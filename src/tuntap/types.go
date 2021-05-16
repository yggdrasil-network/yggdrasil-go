package tuntap

// Out-of-band packet types
const (
	typeKeyDummy = iota
	typeKeyLookup
	typeKeyResponse
)

// In-band packet types
const (
	typeSessionDummy = iota
	typeSessionTraffic
	typeSessionNodeInfoRequest
	typeSessionNodeInfoResponse
)
