package service

import "errors"

var (
	// ErrSourceNotFound is returned when a source is not found
	ErrSourceNotFound = errors.New("source not found")
	// ErrUserNotFound is returned when a user is not found
	ErrUserNotFound = errors.New("user not found")
	// ErrTeamNotFound is returned when a team is not found
	ErrTeamNotFound = errors.New("team not found")
	// ErrInvalidRequest is returned when the request is invalid
	ErrInvalidRequest = errors.New("invalid request")
	// ErrInvalidSourceConfig is returned when the source configuration is invalid
	ErrInvalidSourceConfig = errors.New("invalid source configuration")
)
