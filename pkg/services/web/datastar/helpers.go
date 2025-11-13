package datastar

import (
	"bytes"
	"context"

	"github.com/a-h/templ"
)

// RenderComponent renders a templ component to HTML string
func RenderComponent(ctx context.Context, component templ.Component) (string, error) {
	var buf bytes.Buffer
	err := component.Render(ctx, &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// PatchComponent is a convenience method that renders a templ component and patches it
func (sse *ServerSentEventGenerator) PatchComponent(ctx context.Context, component templ.Component, opts ...PatchElementsOption) error {
	html, err := RenderComponent(ctx, component)
	if err != nil {
		return err
	}
	return sse.PatchElements(html, opts...)
}
