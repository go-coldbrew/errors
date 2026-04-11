package errors_test

import (
	"fmt"
	"io"

	"github.com/go-coldbrew/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func Example_stackFrame() {
	err := errors.New("something failed")
	frames := err.StackFrame()
	// Stack frames are captured automatically
	fmt.Println(len(frames) > 0)
	// Output: true
}

func ExampleWrapf() {
	err := fmt.Errorf("connection refused")
	wrapped := errors.Wrapf(err, "failed to connect to port %d", 5432)
	fmt.Println(wrapped)
	// Output: failed to connect to port 5432: connection refused
}

// WrapWithStatus attaches a gRPC status code to a wrapped error.
func ExampleWrapWithStatus() {
	original := fmt.Errorf("record not found")
	s := status.New(codes.NotFound, "user not found")
	wrapped := errors.WrapWithStatus(original, "lookup failed", s)

	fmt.Println(wrapped)
	fmt.Println("gRPC code:", wrapped.GRPCStatus().Code())
	// Output:
	// lookup failed: record not found
	// gRPC code: NotFound
}

// Cause returns the root cause of a wrapped error chain.
func Example_cause() {
	root := io.EOF
	first := errors.Wrap(root, "read body")
	second := errors.Wrap(first, "handle request")

	fmt.Println("error:", second)
	fmt.Println("cause:", second.Cause())
	// Output:
	// error: handle request: read body: EOF
	// cause: EOF
}

// Wrapped errors are compatible with errors.Is for unwrapping.
// No separate "errors" import needed — Is is re-exported.
func ExampleWrap_errorsIs() {
	original := io.EOF
	wrapped := errors.Wrap(original, "read failed")
	fmt.Println(errors.Is(wrapped, io.EOF))
	// Output: true
}

func ExampleIs() {
	base := fmt.Errorf("connection refused")
	wrapped := errors.Wrap(base, "dial failed")
	fmt.Println(errors.Is(wrapped, base))
	// Output: true
}

func ExampleAs() {
	grpcErr := errors.NewWithStatus("not found", status.New(codes.NotFound, "not found"))
	wrapped := errors.Wrap(grpcErr, "lookup failed")

	var ext errors.ErrorExt
	if errors.As(wrapped, &ext) {
		fmt.Println("found ErrorExt:", ext.GRPCStatus().Code())
	}
	// Output: found ErrorExt: NotFound
}

func ExampleJoin() {
	err1 := errors.New("first")
	err2 := errors.New("second")
	joined := errors.Join(err1, err2)
	fmt.Println(errors.Is(joined, err1))
	fmt.Println(errors.Is(joined, err2))
	// Output:
	// true
	// true
}

func ExampleUnwrap() {
	base := io.EOF
	wrapped := errors.Wrap(base, "read failed")
	fmt.Println(errors.Unwrap(wrapped))
	// Output: EOF
}

func ExampleCause() {
	root := io.EOF
	first := errors.Wrap(root, "read body")
	second := errors.Wrap(first, "handle request")
	fmt.Println(errors.Cause(second))
	// Output: EOF
}
