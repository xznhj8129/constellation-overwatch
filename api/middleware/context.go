package middleware

import "context"

type contextKey string

const (
	ContextKeyUserID   contextKey = "user_id"
	ContextKeyUserRole contextKey = "user_role"
	ContextKeyOrgID    contextKey = "org_id"
	ContextKeyAPIKey   contextKey = "api_key"
	ContextKeyScopes   contextKey = "api_key_scopes"
)

// UserIDFromContext extracts the authenticated user ID from the request context.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyUserID).(string)
	return v
}

// UserRoleFromContext extracts the authenticated user role from the request context.
func UserRoleFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyUserRole).(string)
	return v
}

// OrgIDFromContext extracts the organization ID from the request context.
func OrgIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyOrgID).(string)
	return v
}

// ScopesFromContext extracts the API key scopes from the request context.
func ScopesFromContext(ctx context.Context) []string {
	v, _ := ctx.Value(ContextKeyScopes).([]string)
	return v
}
