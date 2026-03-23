package errors_test

import (
	stderrors "errors"
	"fmt"
	"io"

	"github.com/go-coldbrew/errors"
)

func ExampleNew() {
	err := errors.New("something went wrong")
	fmt.Println(err)
	// Output: something went wrong
}

func ExampleWrap() {
	original := io.EOF
	wrapped := errors.Wrap(original, "failed to read config")
	fmt.Println(wrapped)
	fmt.Println("cause:", wrapped.Cause())
	// Output:
	// failed to read config: EOF
	// cause: EOF
}

func ExampleNewf() {
	err := errors.Newf("user %s not found", "alice")
	fmt.Println(err)
	// Output: user alice not found
}

func ExampleErrorExt_StackFrame() {
	err := errors.New("something failed")
	frames := err.StackFrame()
	// Stack frames are captured automatically
	fmt.Println(len(frames) > 0)
	// Output: true
}

// Wrapped errors are compatible with stdlib errors.Is for unwrapping.
func ExampleWrap_errorsIs() {
	original := io.EOF
	wrapped := errors.Wrap(original, "read failed")
	fmt.Println(stderrors.Is(wrapped, io.EOF))
	// Output: true
}
