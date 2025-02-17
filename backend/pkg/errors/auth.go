package errors

import "fmt"

type AuthErrorCode string

const (
	// OIDC Errors
	ErrOIDCProviderNotConfigured AuthErrorCode = "OIDC_PROVIDER_NOT_CONFIGURED"
	ErrOIDCInvalidState          AuthErrorCode = "OIDC_INVALID_STATE"
	ErrOIDCInvalidToken          AuthErrorCode = "OIDC_INVALID_TOKEN"
	ErrOIDCEmailNotVerified      AuthErrorCode = "OIDC_EMAIL_NOT_VERIFIED"

	// Session Errors
	ErrSessionNotFound AuthErrorCode = "SESSION_NOT_FOUND"
	ErrSessionExpired  AuthErrorCode = "SESSION_EXPIRED"
	ErrTooManySessions AuthErrorCode = "TOO_MANY_SESSIONS"

	// User Errors
	ErrUserNotFound      AuthErrorCode = "USER_NOT_FOUND"
	ErrUserNotAuthorized AuthErrorCode = "USER_NOT_AUTHORIZED"
	ErrAdminNotFound     AuthErrorCode = "ADMIN_NOT_FOUND"
	ErrUserInactive      AuthErrorCode = "USER_INACTIVE"

	// Team Errors
	ErrTeamNotFound       AuthErrorCode = "TEAM_NOT_FOUND"
	ErrTeamAccessDenied   AuthErrorCode = "TEAM_ACCESS_DENIED"
	ErrTeamNameTaken      AuthErrorCode = "TEAM_NAME_TAKEN"
	ErrTeamMemberNotFound AuthErrorCode = "TEAM_MEMBER_NOT_FOUND"
	ErrTeamMemberExists   AuthErrorCode = "TEAM_MEMBER_EXISTS"
	ErrInvalidTeamRole    AuthErrorCode = "INVALID_TEAM_ROLE"

	// Space Errors
	ErrSpaceNotFound           AuthErrorCode = "SPACE_NOT_FOUND"
	ErrSpaceAccessDenied       AuthErrorCode = "SPACE_ACCESS_DENIED"
	ErrSpaceDataSourceNotFound AuthErrorCode = "SPACE_DATA_SOURCE_NOT_FOUND"
	ErrSpaceDataSourceExists   AuthErrorCode = "SPACE_DATA_SOURCE_EXISTS"
	ErrSpaceTeamAccessNotFound AuthErrorCode = "SPACE_TEAM_ACCESS_NOT_FOUND"
	ErrInvalidSpacePermission  AuthErrorCode = "INVALID_SPACE_PERMISSION"

	// Query Errors
	ErrQueryNotFound     AuthErrorCode = "QUERY_NOT_FOUND"
	ErrQueryAccessDenied AuthErrorCode = "QUERY_ACCESS_DENIED"

	// Permission Errors
	ErrInsufficientPermissions AuthErrorCode = "INSUFFICIENT_PERMISSIONS"
	ErrInvalidPermissionLevel  AuthErrorCode = "INVALID_PERMISSION_LEVEL"
)

// AuthError represents an authentication/authorization error
type AuthError struct {
	Code    AuthErrorCode          `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func (e *AuthError) Error() string {
	if len(e.Details) > 0 {
		return fmt.Sprintf("%s: %s (details: %v)", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewAuthError creates a new AuthError
func NewAuthError(code AuthErrorCode, message string, details map[string]interface{}) *AuthError {
	return &AuthError{
		Code:    code,
		Message: message,
		Details: details,
	}
}
