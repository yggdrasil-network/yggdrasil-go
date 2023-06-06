package tun

func (m *TunAdapter) _applyOption(opt SetupOption) {
	switch v := opt.(type) {
	case InterfaceName:
		m.config.name = v
	case InterfaceMTU:
		m.config.mtu = v
	case FileDescriptor:
		m.config.fd = int32(v)
	}
}

type SetupOption interface {
	isSetupOption()
}

type InterfaceName string
type InterfaceMTU uint64
type FileDescriptor int32

func (a InterfaceName) isSetupOption()  {}
func (a InterfaceMTU) isSetupOption()   {}
func (a FileDescriptor) isSetupOption() {}
