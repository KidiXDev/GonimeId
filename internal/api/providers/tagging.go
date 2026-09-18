package providers

import (
	"github.com/KidiXDev/GonimeId/internal/api/source"
	"github.com/KidiXDev/GonimeId/internal/models"
)

// sourceDisplayName is the canonical Source string stamped onto results. It
// must appear in the source's Descriptor.Explicit, or a saved anime will not
// resolve back to it.
func sourceDisplayName(kind source.SourceKind) string {
	return string(kind)
}

// tagResults stamps the canonical Source field onto a source's search results
// in place. Every source is Indonesian-subtitled, so titles carry no language
// tag; the TUI shows the source next to each result instead.
func tagResults(results []*models.Anime, kind source.SourceKind) {
	name := sourceDisplayName(kind)
	for _, anime := range results {
		anime.Source = name
	}
}
