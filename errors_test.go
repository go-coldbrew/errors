package errors

import (
	stderrors "errors"
	"io"
	"testing"

	grpcstatus "google.golang.org/grpc/status"
)

func TestWrap(t *testing.T) {
	var tests = []struct {
		name     string
		err      error
		message  string
		expected string
	}{
		{
			"original error is wrapped",
			io.EOF,
			"read error",
			"read error: EOF",
		},
		{
			"wrapping a wrapped error results in an error wrapped twice",
			Wrap(io.EOF, "read error"),
			"client error",
			"client error: read error: EOF",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Wrap(tt.err, tt.message)
			if err.Error() != tt.expected {
				t.Errorf("(%+v, %+v): expected %+v, got %+v", tt.err, tt.message, tt.expected, err)
			}

			// ensure GRPC status msg has wrapped content no matter you wrap how many times
			if grpcstatus.Convert(err).Message() != tt.expected {
				t.Errorf("GRPC status msg expected %+v, got %+v", tt.expected, grpcstatus.Convert(err).Message())
			}

		})
	}
}

func TestErrorsIs(t *testing.T) {
	// errors.Is should work through the full wrap chain
	base := stderrors.New("base error")
	wrapped1 := Wrap(base, "layer1")
	wrapped2 := Wrap(wrapped1, "layer2")
	wrapped3 := Wrap(wrapped2, "layer3")

	if !stderrors.Is(wrapped1, base) {
		t.Error("wrapped1 should match base via errors.Is")
	}
	if !stderrors.Is(wrapped2, wrapped1) {
		t.Error("wrapped2 should match wrapped1 via errors.Is")
	}
	if !stderrors.Is(wrapped2, base) {
		t.Error("wrapped2 should match base via errors.Is")
	}
	if !stderrors.Is(wrapped3, wrapped2) {
		t.Error("wrapped3 should match wrapped2 via errors.Is")
	}
	if !stderrors.Is(wrapped3, wrapped1) {
		t.Error("wrapped3 should match wrapped1 via errors.Is")
	}
	if !stderrors.Is(wrapped3, base) {
		t.Error("wrapped3 should match base via errors.Is")
	}
}

func TestCauseStillReturnsRoot(t *testing.T) {
	base := stderrors.New("root")
	wrapped1 := Wrap(base, "a")
	wrapped2 := Wrap(wrapped1, "b")

	// Cause() should still return the root error for backward compatibility
	if wrapped1.Cause() != base {
		t.Errorf("wrapped1.Cause() = %v, want %v", wrapped1.Cause(), base)
	}
	if wrapped2.Cause() != base {
		t.Errorf("wrapped2.Cause() = %v, want %v", wrapped2.Cause(), base)
	}
}

func TestNewf(t *testing.T) {
	err := Newf("error %d: %s", 42, "test")
	if err.Error() != "error 42: test" {
		t.Errorf("Newf() = %q, want %q", err.Error(), "error 42: test")
	}
}

func TestWrapf(t *testing.T) {
	base := stderrors.New("base")
	err := Wrapf(base, "context %d", 1)
	expected := "context 1: base"
	if err.Error() != expected {
		t.Errorf("Wrapf() = %q, want %q", err.Error(), expected)
	}
	if !stderrors.Is(err, base) {
		t.Error("Wrapf result should match base via errors.Is")
	}
}

func TestStackDepthCapped(t *testing.T) {
	// With default max of 64, stack should never exceed that
	err := New("test")
	if len(err.StackFrame()) > 64 {
		t.Errorf("stack depth %d exceeds max 64", len(err.StackFrame()))
	}
}

