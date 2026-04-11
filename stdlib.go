package errors

import stderrors "errors"

// --- Re-exports from the standard library errors package ---
//
// These allow github.com/go-coldbrew/errors to be used as a drop-in
// replacement for the standard "errors" package.

// Is reports whether any error in err's tree matches target.
//
// An error is considered a match if it is equal to the target or if
// it implements an Is(error) bool method such that Is(target) returns true.
//
// This is a re-export of the standard library [errors.Is].
func Is(err, target error) bool {
	return stderrors.Is(err, target)
}

// As finds the first error in err's tree that matches target,
// and if one is found, sets target to that error value and returns true.
//
// This is a re-export of the standard library [errors.As].
func As(err error, target any) bool {
	return stderrors.As(err, target)
}

// Unwrap returns the result of calling the Unwrap method on err,
// if err's type contains an Unwrap method returning error.
// Otherwise, Unwrap returns nil.
//
// This is a re-export of the standard library [errors.Unwrap].
func Unwrap(err error) error {
	return stderrors.Unwrap(err)
}

// Join returns an error that wraps the given errors.
// Any nil error values are discarded. Join returns nil if every value
// in errs is nil.
//
// This is a re-export of the standard library [errors.Join].
func Join(errs ...error) error {
	return stderrors.Join(errs...)
}

// ErrUnsupported indicates that a requested operation cannot be performed,
// because it is unsupported.
//
// This is a re-export of the standard library [errors.ErrUnsupported].
var ErrUnsupported = stderrors.ErrUnsupported

// Cause walks the [Unwrap] chain of err and returns the innermost
// (root cause) error. If err does not implement Unwrap, err itself
// is returned. If err is nil, nil is returned.
//
// For [ErrorExt] errors, this produces the same result as calling
// the Cause method, but this function works on any error that
// implements the standard Unwrap interface.
//
// Note: for multi-errors (errors implementing Unwrap() []error, such as
// those created by [Join]), the single-error Unwrap returns nil, so
// Cause returns the multi-error itself.
func Cause(err error) error {
	for {
		unwrapped := stderrors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}
