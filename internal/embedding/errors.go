package embedding

import "errors"

var (
	ErrInvalidConfig       = errors.New("invalid embedding config")
	ErrUnknownProvider     = errors.New("unknown embedding provider")
	ErrEmptyInput          = errors.New("embedding input is empty")
	ErrInvalidVector       = errors.New("invalid embedding vector")
	ErrInvalidVectorValue  = errors.New("embedding vector values must be finite")
	ErrProviderUnavailable = errors.New("embedding provider is unavailable")
)
