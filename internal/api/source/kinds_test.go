package source

// Fake kinds for the registry tests. The registry is kind-agnostic, so the
// tests keep four distinct labels (with fake descriptors) instead of the two
// live sources; none of these is registered in production.
const (
	AniDB     SourceKind = "AniDB"
	AnimeFire SourceKind = "AnimeFire"
	Goyabu    SourceKind = "Goyabu"
	SuperFlix SourceKind = "SuperFlix"
)
