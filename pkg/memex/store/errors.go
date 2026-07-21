package store

import "errors"

// ErrLocked reports that a storage backend's data directory is held by another
// process. It is a distinct sentinel because the remedy is distinct: point this
// session at its own root (YCODE_DATA_DIR) rather than repairing a database.
var ErrLocked = errors.New("store locked by another process")

// ErrUnavailable reports that a required backend could not be opened and the
// caller did not opt into a degraded, store-less run.
var ErrUnavailable = errors.New("store unavailable")
