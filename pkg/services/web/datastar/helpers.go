package datastar

import (
	"fmt"

	ds "github.com/starfederation/datastar-go/datastar"
)

// Re-export SDK helpers for use in templates.
// These generate proper Datastar attribute strings for SSE actions.
//
// Usage in templ files:
//
//	import "github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/web/datastar"
//
//	<div data-init={ datastar.GetSSE("/api/metrics/sse") }>
//	<button data-on:click={ datastar.PostSSE("/api/organizations") }>

var (
	// GetSSE generates a GET SSE call string: @get('/url')
	GetSSE = ds.GetSSE
	// PostSSE generates a POST SSE call string: @post('/url')
	PostSSE = ds.PostSSE
	// PutSSE generates a PUT SSE call string: @put('/url')
	PutSSE = ds.PutSSE
	// DeleteSSE generates a DELETE SSE call string: @delete('/url')
	DeleteSSE = ds.DeleteSSE
	// PatchSSE generates a PATCH SSE call string: @patch('/url')
	PatchSSE = ds.PatchSSE
)

// GetSSEWithQuery generates a GET SSE call with query parameters.
// Example: GetSSEWithQuery("/api/data", "filter", "active") -> "@get('/api/data?filter=active')"
func GetSSEWithQuery(url string, key, value string) string {
	return fmt.Sprintf("@get('%s?%s=%s')", url, key, value)
}

// GetSSEWithParams generates a GET SSE call with multiple query parameters.
// Example: GetSSEWithParams("/api/data", map[string]string{"filter": "active", "limit": "10"})
func GetSSEWithParams(url string, params map[string]string) string {
	if len(params) == 0 {
		return ds.GetSSE(url)
	}

	query := ""
	first := true
	for k, v := range params {
		if first {
			query += "?"
			first = false
		} else {
			query += "&"
		}
		query += k + "=" + v
	}
	return fmt.Sprintf("@get('%s%s')", url, query)
}

// SSEWithIndicator wraps an SSE call with a loading indicator signal.
// Example: SSEWithIndicator(GetSSE("/api/data"), "loading") -> "@get('/api/data'); $loading = true"
func SSEWithIndicator(sseCall string, indicatorSignal string) string {
	return fmt.Sprintf("%s; $%s = true", sseCall, indicatorSignal)
}

// SSEWithContentType wraps an SSE call with a content type option.
// Example: SSEWithContentType(PostSSE("/api/data"), "form") -> "@post('/api/data', {contentType: 'form'})"
func SSEWithContentType(sseCall string, contentType string) string {
	// The SDK generates "@post('/url')" format, we need to inject options
	// Format is: @method('/url') -> @method('/url', {contentType: 'type'})
	if len(sseCall) > 2 && sseCall[len(sseCall)-2:] == "')" {
		return sseCall[:len(sseCall)-1] + fmt.Sprintf(", {contentType: '%s'})", contentType)
	}
	return sseCall
}

// PostSSEForm generates a POST SSE call with form content type.
// This is a common pattern for form submissions.
func PostSSEForm(url string) string {
	return fmt.Sprintf("@post('%s', {contentType: 'form'})", url)
}

// PutSSEForm generates a PUT SSE call with form content type.
func PutSSEForm(url string) string {
	return fmt.Sprintf("@put('%s', {contentType: 'form'})", url)
}

// DeleteSSEWithConfirm generates a DELETE SSE call with a confirmation dialog.
// Example: DeleteSSEWithConfirm("/api/items/123", "Are you sure?")
func DeleteSSEWithConfirm(url string, message string) string {
	return fmt.Sprintf("if (confirm('%s')) @delete('%s')", message, url)
}

// GetSSEWithRetry generates a GET SSE call with retry options for reliable connections.
// Useful for long-lived SSE connections that should reconnect on failure.
func GetSSEWithRetry(url string, maxRetries int, intervalMs int) string {
	return fmt.Sprintf("@get('%s', {retryMaxCount: %d, retryInterval: %d})", url, maxRetries, intervalMs)
}
