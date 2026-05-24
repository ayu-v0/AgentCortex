package ratelimit

import "errors"

var (
	ErrInvalidConfig  = errors.New("invalid rate limit config")
	ErrUnknownType    = errors.New("unknown rate limit type")
	ErrNilFactory     = errors.New("nil rate limit factory")
	ErrLimitStateFull = errors.New("rate limit state is full")
)
