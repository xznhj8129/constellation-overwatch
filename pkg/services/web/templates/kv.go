package templates

import (

	"github.com/a-h/templ"
)

// KVEntry represents a single key-value pair from the KV store
type KVEntry struct {
	Key      string
	Value    string
	Revision string
	Updated  string
}

// KVStateTable renders the KV state table. This is an example component.
func KVStateTable(entries []KVEntry) templ.Component {
	return templ.NopComponent
}