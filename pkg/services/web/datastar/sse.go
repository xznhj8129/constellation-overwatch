package datastar

import (
	"net/http"

	ds "github.com/starfederation/datastar-go/datastar"
)

// Re-export the SDK's ServerSentEventGenerator directly.
type ServerSentEventGenerator = ds.ServerSentEventGenerator

// Re-export option types so callers don't need the SDK import.
type PatchElementOption = ds.PatchElementOption
type ExecuteScriptOption = ds.ExecuteScriptOption

// NewSSE upgrades an HTTP response to an SSE stream.
func NewSSE(w http.ResponseWriter, r *http.Request) *ServerSentEventGenerator {
	return ds.NewSSE(w, r)
}

// Re-export element patch option helpers.
var (
	WithSelector        = ds.WithSelector
	WithSelectorf       = ds.WithSelectorf
	WithSelectorID      = ds.WithSelectorID
	WithModeInner       = ds.WithModeInner
	WithModeOuter       = ds.WithModeOuter
	WithModeRemove      = ds.WithModeRemove
	WithModePrepend     = ds.WithModePrepend
	WithModeAppend      = ds.WithModeAppend
	WithModeBefore      = ds.WithModeBefore
	WithModeAfter       = ds.WithModeAfter
	WithModeReplace     = ds.WithModeReplace
	WithViewTransitions = ds.WithViewTransitions
)

// Re-export signal reader.
var ReadSignals = ds.ReadSignals
