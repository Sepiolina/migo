package app

import (
	"miservice/internal/module"
	"miservice/internal/modules/bom"
	"miservice/internal/modules/system"
)

// DefaultModules is THE list of features this service runs. Adding a module
// is one line here (plus its package under internal/modules).
var DefaultModules = []module.Factory{
	system.New,
	bom.New,
}
