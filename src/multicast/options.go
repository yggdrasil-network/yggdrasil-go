package multicast

import "regexp"

func (m *Multicast) _applyOption(opt SetupOption) {
	switch v := opt.(type) {
	case MulticastInterface:
		m.config._interfaces[v] = struct{}{}
	case GroupAddress:
		m.config._groupAddr = v
	case Discriminator:
		m.config._discriminator = append(m.config._discriminator[:0], v...)
	case DiscriminatorMatch:
		m.config._discriminatorMatch = v
	}
}

type SetupOption interface {
	isSetupOption()
}

type MulticastInterface struct {
	Regex    *regexp.Regexp
	Beacon   bool
	Listen   bool
	Port     uint16
	Priority uint8
}

type GroupAddress string
type Discriminator []byte
type DiscriminatorMatch func([]byte) bool

func (a MulticastInterface) isSetupOption() {}
func (a GroupAddress) isSetupOption()       {}
func (a Discriminator) isSetupOption()      {}
func (a DiscriminatorMatch) isSetupOption() {}
