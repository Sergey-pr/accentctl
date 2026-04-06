package constants

import "time"

const (
	// ChunkSize is the number of translation keys per upload batch.
	ChunkSize = 250

	// RequestDelay is the pause between consecutive requests.
	RequestDelay = time.Second
)
