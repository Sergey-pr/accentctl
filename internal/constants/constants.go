package constants

import "time"

const (
	// ChunkSize is the number of translation keys per upload batch.
	ChunkSize = 250

	// DefaultRequestDelay is the pause between consecutive requests, overridable
	// with the requestDelay config key or ACCENT_REQUEST_DELAY.
	DefaultRequestDelay = 1500 * time.Millisecond
)
