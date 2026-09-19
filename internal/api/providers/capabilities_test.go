package providers

import (
	"testing"

	"github.com/KidiXDev/GonimeId/internal/api/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestModelC_NoSourceIsBrowserGatedOrSeasoned pins the Model C capability
// wiring on the live registry: every source is a flat, pure-HTTP anime catalog,
// so none may carry browser or season methods it does not need.
func TestModelC_NoSourceIsBrowserGatedOrSeasoned(t *testing.T) {
	t.Parallel()
	for _, kind := range []source.SourceKind{source.Otakudesu, source.Samehadaku, source.Nimegami} {
		s, ok := source.Registered(kind)
		require.True(t, ok, "source %s must be registered", kind)
		assert.False(t, source.IsBrowserGated(s), "%s is pure-HTTP and must not be browser-gated", kind)
		assert.False(t, source.IsSeasoned(s), "%s is a flat anime catalog, not seasoned", kind)
	}
}
