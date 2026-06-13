package client

import (
	"errors"
	"strconv"
	"strings"
)

var (
	ErrServerEndpointRequired = errors.New("server endpoint is required")
	ErrQAStreamRequestFailed  = errors.New("qa stream request failed")
	ErrQAStreamEvent          = errors.New("qa stream event error")
	ErrUnknownEventType       = errors.New("unknown event type")
)

type qaStreamStatusError struct {
	statusCode int
	body       string
}

func (e qaStreamStatusError) Error() string {
	return ErrQAStreamRequestFailed.Error() + ": status " + strconv.Itoa(e.statusCode) + ": " + strings.TrimSpace(e.body)
}

func (e qaStreamStatusError) Is(target error) bool {
	return target == ErrQAStreamRequestFailed
}

type qaStreamEventError struct {
	code    string
	message string
}

func (e qaStreamEventError) Error() string {
	code := strings.TrimSpace(e.code)
	message := strings.TrimSpace(e.message)
	if code == "" {
		return message
	}
	if message == "" {
		return code
	}
	return code + ": " + message
}

func (e qaStreamEventError) Is(target error) bool {
	return target == ErrQAStreamEvent
}

type unknownEventTypeError string

func (e unknownEventTypeError) Error() string {
	return ErrUnknownEventType.Error() + " " + strconv.Quote(string(e))
}

func (e unknownEventTypeError) Is(target error) bool {
	return target == ErrUnknownEventType
}
