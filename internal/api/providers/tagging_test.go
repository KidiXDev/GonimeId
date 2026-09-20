package providers

import (
	"testing"

	"github.com/KidiXDev/GonimeId/internal/api/source"
	"github.com/KidiXDev/GonimeId/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestTagResults_StampsSourceWithoutLanguageTag(t *testing.T) {
	t.Parallel()
	for _, kind := range []source.SourceKind{source.Otakudesu, source.Samehadaku, source.Nimegami, source.Ylnime, source.Moenime, source.Astronime} {
		res := []*models.Anime{{Name: "Naruto", URL: "id1"}, {Name: "Bleach"}}
		tagResults(res, kind)
		for _, a := range res {
			assert.Equal(t, string(kind), a.Source)
		}
		assert.Equal(t, "Naruto", res[0].Name, "titles are left untouched; the TUI shows the source")
	}
}

func TestSourceDisplayName_MatchesDescriptorExplicit(t *testing.T) {
	t.Parallel()
	for _, kind := range []source.SourceKind{source.Otakudesu, source.Samehadaku, source.Nimegami, source.Ylnime, source.Moenime, source.Astronime} {
		s, ok := source.Registered(kind)
		if !ok {
			t.Fatalf("%s not registered", kind)
		}
		assert.Contains(t, s.Describe().Explicit, sourceDisplayName(kind),
			"a saved anime must resolve back to its source by the stamped name")
	}
}
