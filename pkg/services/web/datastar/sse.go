package datastar

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ServerSentEventGenerator handles SSE communication with the browser
type ServerSentEventGenerator struct {
	w http.ResponseWriter
	r *http.Request
}

// NewServerSentEventGenerator creates a new SSE generator and sets up the response headers
func NewServerSentEventGenerator(w http.ResponseWriter, r *http.Request) *ServerSentEventGenerator {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Flush headers
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	return &ServerSentEventGenerator{
		w: w,
		r: r,
	}
}

// PatchElementMode defines how elements should be patched
type PatchElementMode string

const (
	ElementPatchModeMorph  PatchElementMode = "morph"
	ElementPatchModeInner  PatchElementMode = "inner"
	ElementPatchModeOuter  PatchElementMode = "outer"
	ElementPatchModePrepend PatchElementMode = "prepend"
	ElementPatchModeAppend PatchElementMode = "append"
	ElementPatchModeBefore PatchElementMode = "before"
	ElementPatchModeAfter  PatchElementMode = "after"
	ElementPatchModeRemove PatchElementMode = "remove"
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
func (sse *ServerSentEventGenerator) PatchElements(html string, opts ...PatchElementsOption) error {
	options := &PatchElementsOptions{
		Mode: ElementPatchModeMorph,
	}
	for _, opt := range opts {
		opt(options)
	}

	// Build the event data
	var dataLines []string
	dataLines = append(dataLines, "event: datastar-patch-elements")

	// Add selector if provided
	if options.Selector != "" {
		dataLines = append(dataLines, fmt.Sprintf("data: selector %s", options.Selector))
	}

	// Add mode if not default
	if options.Mode != ElementPatchModeMorph {
		dataLines = append(dataLines, fmt.Sprintf("data: mode %s", options.Mode))
	}

	// Add view transition if enabled
	if options.ViewTransition {
		dataLines = append(dataLines, "data: vt true")
	}

	// Add the HTML content (may be multiple lines)
	for _, line := range strings.Split(html, "\n") {
		dataLines = append(dataLines, fmt.Sprintf("data: %s", line))
	}

	// Write the event
	eventData := strings.Join(dataLines, "\n") + "\n\n"
	_, err := fmt.Fprint(sse.w, eventData)
	if err != nil {
		return err
	}

	// Flush to send immediately
	if flusher, ok := sse.w.(http.Flusher); ok {
		flusher.Flush()
	}

	return nil
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

// PatchSignals sends signal updates to the browser
func (sse *ServerSentEventGenerator) PatchSignals(signals map[string]interface{}, opts ...PatchSignalsOption) error {
	options := &PatchSignalsOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Marshal signals to JSON
	signalsJSON, err := json.Marshal(signals)
	if err != nil {
		return err
	}

	// Build the event
	var dataLines []string
	dataLines = append(dataLines, "event: datastar-patch-signals")

	if options.OnlyIfMissing {
		dataLines = append(dataLines, "data: onlyIfMissing true")
	}

	dataLines = append(dataLines, fmt.Sprintf("data: %s", string(signalsJSON)))

	// Write the event
	eventData := strings.Join(dataLines, "\n") + "\n\n"
	_, err = fmt.Fprint(sse.w, eventData)
	if err != nil {
		return err
	}

	// Flush to send immediately
	if flusher, ok := sse.w.(http.Flusher); ok {
		flusher.Flush()
	}

	return nil
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
func (sse *ServerSentEventGenerator) ExecuteScript(script string, opts ...ExecuteScriptOption) error {
	options := &ExecuteScriptOptions{
		AutoRemove: true,
	}
	for _, opt := range opts {
		opt(options)
	}

	var dataLines []string
	dataLines = append(dataLines, "event: datastar-execute-script")

	if !options.AutoRemove {
		dataLines = append(dataLines, "data: autoRemove false")
	}

	if len(options.Attributes) > 0 {
		for key, value := range options.Attributes {
			dataLines = append(dataLines, fmt.Sprintf("data: %s %s", key, value))
		}
	}

	dataLines = append(dataLines, fmt.Sprintf("data: %s", script))

	// Write the event
	eventData := strings.Join(dataLines, "\n") + "\n\n"
	_, err := fmt.Fprint(sse.w, eventData)
	if err != nil {
		return err
	}

	// Flush to send immediately
	if flusher, ok := sse.w.(http.Flusher); ok {
		flusher.Flush()
	}

	return nil
}

// ReadSignals parses incoming signal data from the browser
func ReadSignals(r *http.Request, target interface{}) error {
	var data []byte
	var err error

	if r.Method == http.MethodGet {
		// For GET requests, read from query parameter
		datastarParam := r.URL.Query().Get("datastar")
		if datastarParam == "" {
			return fmt.Errorf("missing datastar query parameter")
		}
		data = []byte(datastarParam)
	} else {
		// For other methods, read from body
		data = make([]byte, r.ContentLength)
		_, err = r.Body.Read(data)
		if err != nil && err.Error() != "EOF" {
			return err
		}
	}

	// Unmarshal JSON into target
	return json.Unmarshal(data, target)
}
