/*
Package errors is a drop-in replacement for the standard library "errors" package
that adds stack trace capture, gRPC status codes, and error notification support.

All functions from the standard library errors package are re-exported:
[Is], [As], [Unwrap], [Join], and [ErrUnsupported].
This allows you to use this package as your sole errors import:

	import "github.com/go-coldbrew/errors"

	// Standard library functions work as expected:
	errors.Is(err, target)
	errors.As(err, &target)
	errors.Unwrap(err)
	errors.Join(err1, err2)

	// ColdBrew extensions add stack traces and gRPC status:
	errors.New("something failed")       // captures stack trace
	errors.Wrap(err, "context")          // wraps with stack trace
	errors.Cause(err)                    // walks Unwrap chain to root cause

# Error Creation

The simplest way to use this package is by calling one of the two functions:

	errors.New(...)
	errors.Wrap(...)

You can also initialize custom error stack by using one of the WithSkip functions. WithSkip allows
skipping the defined number of functions from the stack information.

	New                    — create a new error with stack info
	NewWithSkip            — skip functions on the stack
	NewWithStatus          — add GRPC status
	NewWithSkipAndStatus   — skip functions and add GRPC status
	Wrap                   — wrap an existing error
	WrapWithStatus         — wrap and add GRPC status
	WrapWithSkip           — wrap and skip functions on the stack
	WrapWithSkipAndStatus  — wrap, skip functions, and add GRPC status

Head to https://docs.coldbrew.cloud for more information.
*/
package errors
