package logs

import "errors"

// Common errors for the logs package
var (
	// ErrTeamQueryNotFound is returned when a team query cannot be found
	ErrTeamQueryNotFound = errors.New("team query not found")
)