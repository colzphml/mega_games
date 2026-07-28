package types

type Result struct {
	GameURL     string
	ContentType string
	Image       []byte

	// Degraded marks a fallback card rendered from metadata because the
	// screenshot failed. Without this the pipeline reports success and a
	// broken NeonSportz layout goes unnoticed for weeks.
	Degraded bool
}
