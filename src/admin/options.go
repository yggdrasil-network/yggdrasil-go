package admin

func (c *AdminSocket) _applyOption(opt SetupOption) {
	switch v := opt.(type) {
	case ListenAddress:
		c.config.listenaddr = v
	}
}

type SetupOption interface {
	isSetupOption()
}

type ListenAddress string

func (a ListenAddress) isSetupOption() {}
