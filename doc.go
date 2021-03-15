/*Package errors provides an implementation of golang error with stack strace information attached to it,
the error objects created by this package are compatible with https://golang.org/pkg/errors/

How To Use

The simplest way to use this package is by calling one of the two functions

	errors.New(...)
	errors.Wrap(...)

You can also initialize custom error stack by using one of the `WithSkip` functions. `WithSkip` allows
skipping the defined number of functions from the stack information.

*/
package errors
