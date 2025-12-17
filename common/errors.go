package common

import "errors"

var (
	ErrKeyNotFound = errors.New("key not found")
	ErrDiskFull    = errors.New("disk full")

	ErrClosed   = errors.New("storage engine closed")
	ErrKeyEmpty = errors.New("key cannot be empty")
)
