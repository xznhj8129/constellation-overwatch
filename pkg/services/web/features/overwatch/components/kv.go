package components

// KVEntry represents a single key-value pair from the KV store
type KVEntry struct {
	Key      string
	Value    string
	Revision string
	Updated  string
}
