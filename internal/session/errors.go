package session

import "errors"

var (
	ErrInvalidConfig    = errors.New("invalid session config")
	ErrManagerClosed    = errors.New("session manager is closed")
	ErrSessionExists    = errors.New("session already exists")
	ErrSessionNotFound  = errors.New("session not found")
	ErrTooManySessions  = errors.New("too many active sessions")
	ErrInvalidSessionID = errors.New("invalid session id")
)
