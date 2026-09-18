// Package gonimeid exposes public client APIs and shared sentinel errors.
package gonimeid

import "github.com/KidiXDev/GonimeId/internal/scraper/netx"

// ErrSourceUnavailable indicates that the selected upstream source is
// temporarily unavailable or blocked.
var ErrSourceUnavailable = netx.ErrSourceUnavailable
