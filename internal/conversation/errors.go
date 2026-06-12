package conversation

import "errors"

var (
	ErrInvalidID       = errors.New("invalid conversation id")
	ErrInvalidLimit    = errors.New("invalid conversation limit")
	ErrInvalidConfig   = errors.New("invalid conversation config")
	ErrNilStore        = errors.New("nil conversation store")
	ErrInvalidSchedule = errors.New("invalid conversation cleanup schedule")
)
