package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"normal", "Naruto", "Naruto"},
		{"trims", "  Naruto  ", "Naruto"},
		{"forbidden chars", `a/b\c:d*e?f"g<h>i|j`, "a_b_c_d_e_f_g_h_i_j"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, sanitizeFilename(tt.in))
		})
	}
}

// isStdoutTerminal caches via sync.Once. Test runs in CI where stdout is not
// a TTY → should return false. Locally a developer may see true; just verify
// it doesn't panic and result is stable across calls.
func TestIsStdoutTerminal_Stable(t *testing.T) {
	t.Parallel()
	first := isStdoutTerminal()
	second := isStdoutTerminal()
	assert.Equal(t, first, second)
}
