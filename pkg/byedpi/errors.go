// Package byedpi provides platform-specific TCP desynchronization wrappers.
package byedpi

import "errors"

// ErrUnsupported reports that the current platform or socket does not support
// the requested desynchronization strategy.
var ErrUnsupported = errors.New("byedpi: unsupported kernel or platform")

// ErrNotClientHello reports that the first non-empty write is not a TLS ClientHello.
var ErrNotClientHello = errors.New("byedpi: first write is not a TLS ClientHello")
