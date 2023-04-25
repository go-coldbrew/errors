/*
Package errors provides an implementation of golang error with stack strace information attached to it,
the error objects created by this package are compatible with https://golang.org/pkg/errors/

How To Use
The simplest way to use this package is by calling one of the two functions

	errors.New(...)
	errors.Wrap(...)

You can also initialize custom error stack by using one of the `WithSkip` functions. `WithSkip` allows
skipping the defined number of functions from the stack information.

	if you want to create a new error use New
	if you want to skip some functions on the stack use NewWithSkip
	if you want to add GRPC status use NewWithStatus
	if you want to skip some functions on the stack and add GRPC status use NewWithSkipAndStatus
	if you want to wrap an existing error use Wrap
	if you want to wrap an existing error and add GRPC status use WrapWithStatus
	if you want to wrap an existing error and skip some functions on the stack use WrapWithSkip
	if you want to wrap an existing error, skip some functions on the stack and add GRPC status use WrapWithSkipAndStatus
	if you want to wrap an existing error and add notifier options use WrapWithNotifier
	if you want to wrap an existing error, skip some functions on the stack and add notifier options use WrapWithSkipAndNotifier

Head to https://docs.coldbrew.cloud for more information.
*/
package errors
