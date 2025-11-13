package datastar

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/a-h/templ"
	ds "github.com/starfederation/datastar-go/datastar"
)

// ServerSentEventGenerator wraps the official Datastar SSE functionality
// This wrapper maintains API compatibility while using the official library
type ServerSentEventGenerator struct {
	sse *ds.ServerSentEventGenerator
}

// NewServerSentEventGenerator creates a new SSE generator using the official Datastar library
func NewServerSentEventGenerator(w http.ResponseWriter, r *http.Request) *ServerSentEventGenerator {
	return &ServerSentEventGenerator{
		sse: ds.NewSSE(w, r),
	}
}

// PatchElementMode defines how elements should be patched
type PatchElementMode string

const (
	ElementPatchModeMorph   PatchElementMode = "morph" // Maps to outer
	ElementPatchModeInner   PatchElementMode = "inner"
	ElementPatchModeOuter   PatchElementMode = "outer"
	ElementPatchModePrepend PatchElementMode = "prepend"
	ElementPatchModeAppend  PatchElementMode = "append"
	ElementPatchModeBefore  PatchElementMode = "before"
	ElementPatchModeAfter   PatchElementMode = "after"
	ElementPatchModeRemove  PatchElementMode = "remove"
)

// PatchElementsOptions contains options for patching elements
type PatchElementsOptions struct {
	Selector       string
	Mode           PatchElementMode
	ViewTransition bool
}

// PatchElementsOption is a function that configures PatchElementsOptions
type PatchElementsOption func(*PatchElementsOptions)

// WithSelector sets the selector for element patching
func WithSelector(selector string) PatchElementsOption {
	return func(o *PatchElementsOptions) {
		o.Selector = selector
	}
}

// WithMode sets the patch mode
func WithMode(mode PatchElementMode) PatchElementsOption {
	return func(o *PatchElementsOptions) {
		o.Mode = mode
	}
}

// WithViewTransition enables view transitions
func WithViewTransition(enable bool) PatchElementsOption {
	return func(o *PatchElementsOptions) {
		o.ViewTransition = enable
	}
}

// PatchElements sends HTML elements to the browser for DOM manipulation
// Uses the official Datastar library under the hood
func (s *ServerSentEventGenerator) PatchElements(html string, opts ...PatchElementsOption) error {
	options := &PatchElementsOptions{
		Mode: ElementPatchModeMorph, // Default to morph/outer
	}
	for _, opt := range opts {
		opt(options)
	}

	// Convert to official Datastar options
	var dsOpts []ds.PatchElementOption

	if options.Selector != "" {
		dsOpts = append(dsOpts, ds.WithSelector(options.Selector))
	}

	// Map mode to official Datastar mode
	var dsMode ds.ElementPatchMode
	switch options.Mode {
	case ElementPatchModeInner:
		dsMode = ds.ElementPatchModeInner
	case ElementPatchModePrepend:
		dsMode = ds.ElementPatchModePrepend
	case ElementPatchModeAppend:
		dsMode = ds.ElementPatchModeAppend
	case ElementPatchModeBefore:
		dsMode = ds.ElementPatchModeBefore
	case ElementPatchModeAfter:
		dsMode = ds.ElementPatchModeAfter
	case ElementPatchModeRemove:
		dsMode = ds.ElementPatchModeRemove
	default:
		dsMode = ds.ElementPatchModeOuter // Default and morph both map to outer
	}

	if dsMode != ds.ElementPatchModeOuter {
		dsOpts = append(dsOpts, ds.WithMode(dsMode))
	}

	if options.ViewTransition {
		dsOpts = append(dsOpts, ds.WithViewTransitions())
	}

	return s.sse.PatchElements(html, dsOpts...)
}

// PatchSignalsOptions contains options for patching signals
type PatchSignalsOptions struct {
	OnlyIfMissing bool
}

// PatchSignalsOption is a function that configures PatchSignalsOptions
type PatchSignalsOption func(*PatchSignalsOptions)

// WithOnlyIfMissing sets the onlyIfMissing flag
func WithOnlyIfMissing(enable bool) PatchSignalsOption {
	return func(o *PatchSignalsOptions) {
		o.OnlyIfMissing = enable
	}
}

// PatchSignals sends signal updates to the browser using the official Datastar library
func (s *ServerSentEventGenerator) PatchSignals(signals map[string]interface{}, opts ...PatchSignalsOption) error {
	options := &PatchSignalsOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Convert to official Datastar options
	var dsOpts []ds.PatchSignalsOption
	if options.OnlyIfMissing {
		dsOpts = append(dsOpts, ds.WithOnlyIfMissing(true))
	}

	return s.sse.MarshalAndPatchSignals(signals, dsOpts...)
}

// ExecuteScriptOptions contains options for executing scripts
type ExecuteScriptOptions struct {
	AutoRemove bool
	Attributes map[string]string
}

// ExecuteScriptOption is a function that configures ExecuteScriptOptions
type ExecuteScriptOption func(*ExecuteScriptOptions)

// WithAutoRemove sets whether the script tag should be auto-removed
func WithAutoRemove(enable bool) ExecuteScriptOption {
	return func(o *ExecuteScriptOptions) {
		o.AutoRemove = enable
	}
}

// WithAttributes sets custom attributes for the script tag
func WithAttributes(attrs map[string]string) ExecuteScriptOption {
	return func(o *ExecuteScriptOptions) {
		o.Attributes = attrs
	}
}

// ExecuteScript sends JavaScript code to be executed in the browser
// Note: The official library doesn't have ExecuteScript, so we'll implement it manually
func (s *ServerSentEventGenerator) ExecuteScript(script string, opts ...ExecuteScriptOption) error {
	// For now, we'll use PatchElements to inject a script tag
	// This is a common pattern when ExecuteScript is not available
	scriptHTML := `<script>` + script + `</script>`
	return s.sse.PatchElements(scriptHTML, ds.WithSelector("head"), ds.WithModeAppend())
}

// RemoveElement removes an element from the DOM
func (s *ServerSentEventGenerator) RemoveElement(selector string) error {
	return s.sse.RemoveElement(selector)
}

// Redirect redirects the client to a new page
// Note: The official library may not have Redirect, so we'll use ExecuteScript
func (s *ServerSentEventGenerator) Redirect(url string) error {
	script := `window.location.href = "` + url + `";`
	return s.ExecuteScript(script)
}

// PatchComponent is a convenience method that renders a templ component and patches it
func (s *ServerSentEventGenerator) PatchComponent(ctx context.Context, component templ.Component, opts ...PatchElementsOption) error {
	// Convert options
	options := &PatchElementsOptions{
		Mode: ElementPatchModeMorph,
	}
	for _, opt := range opts {
		opt(options)
	}

	// Convert to official Datastar options
	var dsOpts []ds.PatchElementOption

	if options.Selector != "" {
		dsOpts = append(dsOpts, ds.WithSelector(options.Selector))
	}

	// Map mode
	var dsMode ds.ElementPatchMode
	switch options.Mode {
	case ElementPatchModeInner:
		dsMode = ds.ElementPatchModeInner
	case ElementPatchModePrepend:
		dsMode = ds.ElementPatchModePrepend
	case ElementPatchModeAppend:
		dsMode = ds.ElementPatchModeAppend
	case ElementPatchModeBefore:
		dsMode = ds.ElementPatchModeBefore
	case ElementPatchModeAfter:
		dsMode = ds.ElementPatchModeAfter
	case ElementPatchModeRemove:
		dsMode = ds.ElementPatchModeRemove
	default:
		dsMode = ds.ElementPatchModeOuter
	}

	if dsMode != ds.ElementPatchModeOuter {
		dsOpts = append(dsOpts, ds.WithMode(dsMode))
	}

	if options.ViewTransition {
		dsOpts = append(dsOpts, ds.WithViewTransitions())
	}

	// Use the official Datastar PatchElementTempl method
	return s.sse.PatchElementTempl(component, dsOpts...)
}

// ReadSignals parses incoming signal data from the browser using the official Datastar library
func ReadSignals(r *http.Request, target interface{}) error {
	return ds.ReadSignals(r, target)
}

// Helper function to read signals as a map
func ReadSignalsMap(r *http.Request) (map[string]interface{}, error) {
	var signals map[string]interface{}
	err := ds.ReadSignals(r, &signals)
	return signals, err
}

// Helper function to read form data and merge with signals
func ReadFormWithSignals(r *http.Request) (map[string]interface{}, error) {
	// First try to read signals
	signals, err := ReadSignalsMap(r)
	if err != nil {
		signals = make(map[string]interface{})
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		return signals, err
	}

	// Merge form data into signals
	for key, values := range r.Form {
		if len(values) > 0 {
			signals[key] = values[0]
		}
	}

	return signals, nil
}

// MarshalAndPatchSignals is a convenience wrapper around the official Datastar method
func MarshalAndPatchSignals(sse *ServerSentEventGenerator, data interface{}, opts ...PatchSignalsOption) error {
	// Convert data to map if it's not already
	var signals map[string]interface{}

	switch v := data.(type) {
	case map[string]interface{}:
		signals = v
	default:
		// Marshal and unmarshal to convert struct to map
		jsonData, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(jsonData, &signals); err != nil {
			return err
		}
	}

	return sse.PatchSignals(signals, opts...)
}
