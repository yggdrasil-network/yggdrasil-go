package module

import (
	"github.com/gologme/log"

	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// Module is an interface that defines which functions must be supported by a
// given Yggdrasil module.
type Module interface {
	Init(core *core.Core, state *config.NodeConfig, log *log.Logger, options interface{}) error
	Start() error
	Stop() error
	SetupAdminHandlers(a *admin.AdminSocket)
	IsStarted() bool
}
