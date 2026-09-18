package util

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddOption(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	addOption(&b, "-h", "help text")
	got := b.String()
	assert.Contains(t, got, "-h")
	assert.Contains(t, got, "help text")
}

func TestAddFeature(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	addFeature(&b, "FastSearch", "fuzzy finder")
	got := b.String()
	assert.Contains(t, got, "FastSearch")
	assert.Contains(t, got, "fuzzy finder")
}

func TestAddExample(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	addExample(&b, "gonimeid naruto", "search naruto")
	got := b.String()
	assert.Contains(t, got, "gonimeid naruto")
	assert.Contains(t, got, "search naruto")
}

func TestShowBeautifulHelp_DoesNotPanic(t *testing.T) {
	t.Parallel()
	assert.NotPanics(t, func() { ShowBeautifulHelp() })
}
