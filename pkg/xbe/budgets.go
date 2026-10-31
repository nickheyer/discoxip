package xbe

// Steps, paths and retries a derivation's runs share, blocks an exploration may run and paths it may hold open at
// once, the thunk hops followed and the opening instructions checked
const (
	DeriveSteps      = 20000
	DerivePaths      = 64
	ExploreSteps     = 20000000
	ExplorePaths     = 4096
	libraryRetries   = 4
	methodRetries    = 6
	drawRetries      = 8
	thunkHops        = 8
	plausibleOpening = 8
)
