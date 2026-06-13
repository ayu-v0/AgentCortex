package model

import "errors"

var (
	ErrInvalidConfig       = errors.New("invalid model config")
	ErrUnknownProvider     = errors.New("unknown model provider")
	ErrInvalidRequest      = errors.New("invalid model request")
	ErrEmptyMessages       = errors.New("model messages are empty")
	ErrProviderUnavailable = errors.New("model provider is unavailable")
	ErrInvalidResponse     = errors.New("invalid model response")
)
