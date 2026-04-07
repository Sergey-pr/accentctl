package constants

import "time"

const (
	// ChunkSize is the number of translation keys per upload batch.
	ChunkSize = 250

	// RequestDelay is the pause between consecutive requests.
	RequestDelay = 1500 * time.Millisecond // 1.5 sec
)
