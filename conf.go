package owl

import (
	"embed"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/localizer"
)

type Config struct {
	LocalizerFS        embed.FS
	LocalizerSharedKey string
	LocalizerErrorsKey string

	Cypher core.Cypher
}

func (config Config) newLocalizerStore() *localizer.Store {
	locstore := localizer.NewLocalizerStore(config.LocalizerFS, config.LocalizerSharedKey, config.LocalizerErrorsKey, config.Cypher)
	return &locstore
}
