package version

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasVersionArg(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"--version", []string{"gonimeid", "--version"}, true},
		{"-version", []string{"gonimeid", "-version"}, true},
		{"-v", []string{"gonimeid", "-v"}, true},
		{"--v", []string{"gonimeid", "--v"}, true},
		{"space version", []string{"gonimeid", " version"}, true},
		{"no args", []string{"gonimeid"}, false},
		{"unrelated arg", []string{"gonimeid", "play"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Args = tt.args
			assert.Equal(t, tt.want, HasVersionArg())
		})
	}
}

func TestShowVersion(t *testing.T) {
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	ShowVersion()
	require.NoError(t, w.Close())

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	output := string(out)

	assert.Contains(t, output, "GonimeId v"+Version)
	assert.True(t,
		strings.Contains(output, "with SQLite tracking") || strings.Contains(output, "without SQLite tracking"),
		"output must mention SQLite tracking state: %q", output)
}
