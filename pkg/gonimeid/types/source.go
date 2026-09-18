package types

import (
	"fmt"
	"strings"

	"github.com/KidiXDev/GonimeId/internal/scraper"
)

// Source represents an anime scraper source
type Source int

const (
	// SourceOtakudesu is otakudesu (Indonesian-subtitled).
	SourceOtakudesu Source = iota
	// SourceSamehadaku is samehadaku (Indonesian-subtitled).
	SourceSamehadaku
)

// String returns the canonical spelling the registry stamps onto
// models.Anime.Source.
func (s Source) String() string {
	switch s {
	case SourceOtakudesu:
		return "Otakudesu"
	case SourceSamehadaku:
		return "Samehadaku"
	default:
		return "Unknown"
	}
}

// ToScraperType converts the public Source type to internal ScraperType
func (s Source) ToScraperType() scraper.ScraperType {
	switch s {
	case SourceSamehadaku:
		return scraper.SamehadakuType
	default:
		return scraper.OtakudesuType
	}
}

// ParseSource parses a string into a Source type
func ParseSource(s string) (Source, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "otakudesu":
		return SourceOtakudesu, nil
	case "samehadaku":
		return SourceSamehadaku, nil
	default:
		return SourceOtakudesu, fmt.Errorf("unknown source: %s", s)
	}
}
