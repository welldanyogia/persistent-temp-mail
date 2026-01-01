package sse

import "errors"

var (
	// ErrStreamingNotSupported is returned when the response writer doesn't support streaming.
	ErrStreamingNotSupported = errors.New("streaming not supported")

	// ErrConnectionLimitExceeded is returned when a user has too many connections.
	ErrConnectionLimitExceeded = errors.New("connection limit exceeded")

	// ErrConnectionClosed is returned when trying to write to a closed connection.
	ErrConnectionClosed = errors.New("connection closed")

	// ErrInvalidToken is returned when authentication fails.
	ErrInvalidToken = errors.New("invalid or missing authentication token")
)
