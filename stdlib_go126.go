//go:build go1.26

package errors

import stderrors "errors"

// AsType finds the first error in err's tree that matches the type E,
// and if one is found, returns that error value and true.
// Otherwise, it returns the zero value of E and false.
//
// This is a re-export of the standard library [errors.AsType].
func AsType[E error](err error) (E, bool) {
	return stderrors.AsType[E](err)
}
