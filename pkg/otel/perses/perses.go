// Package perses re-exports Perses dashboarding types,
// isolating the rest of the codebase from the upstream dependency.
package perses

import (
	persesembed "github.com/perses/perses/embed"
	"github.com/perses/perses/pkg/model/api/config"
	"github.com/perses/perses/pkg/model/api/v1/secret"
)

type (
	Server   = persesembed.Server
	Config   = config.Config
	Database = config.Database
	File     = config.File
	Security = config.Security
	Plugin   = config.Plugin
	Hidden   = secret.Hidden
)

var Start = persesembed.Start
