package tun

func (m *TunAdapter) _applyOption(opt SetupOption) {
	switch v := opt.(type) {
	case InterfaceName:
		m.config.name = v
	case InterfaceMTU:
		m.config.mtu = v
	}
}

type SetupOption interface {
	isSetupOption()
}

type InterfaceName string
type InterfaceMTU uint64

func (a InterfaceName) isSetupOption() {}
func (a InterfaceMTU) isSetupOption()  {}
