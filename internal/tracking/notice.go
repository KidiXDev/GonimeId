package tracking

// HandleTrackingNotice used to print two lines above every search prompt when
// the binary was built without CGO/SQLite. A build-time fact shown on each run
// read like an error, so the notice is gone; `--version` still says which
// build this is. Kept as a no-op so the call site and tests stay stable.
func HandleTrackingNotice() {}
