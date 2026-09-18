package handlers

import (
	"fmt"

	"github.com/KidiXDev/GonimeId/internal/download"
	"github.com/KidiXDev/GonimeId/internal/util"
)

// HandleDownloadRequest processes download requests
func HandleDownloadRequest() error {
	// Initialize logger for download process
	util.InitLogger()

	if util.GlobalDownloadRequest == nil {
		return fmt.Errorf("download request is nil")
	}

	if err := download.HandleDownloadRequest(util.GlobalDownloadRequest); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	return nil
}
