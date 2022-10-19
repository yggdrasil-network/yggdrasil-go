package multicast

import "regexp"

func (m *Multicast) _applyOption(opt SetupOption) {
	switch v := opt.(type) {
	case MulticastInterface:
		m.config._interfaces[v] = struct{}{}
	case GroupAddress:
		m.config._groupAddr = v
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

func (a MulticastInterface) isSetupOption() {}
func (a GroupAddress) isSetupOption()       {}
