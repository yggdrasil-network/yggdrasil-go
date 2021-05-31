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
	typeSessionProto
)

// Protocol packet types
const (
	typeProtoDummy = iota
	typeProtoNodeInfoRequest
	typeProtoNodeInfoResponse
	typeProtoDebug = 255
)
