package artifact

import "devbox/internal/config"

// SourcePreview binds proposed settings to one explicit source. Other sources
// remain in order; a preview never discovers layers or changes participation.
type SourcePreview struct {
	Path  string
	Layer config.Layer
}

func (p *SourcePreview) layer(source config.Source) *config.Layer {
	if p != nil && p.Path == source.Path {
		return &p.Layer
	}
	return nil
}
