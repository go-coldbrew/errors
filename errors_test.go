package errors

import (
	stderrors "errors"
	"io"
	"testing"

	grpcstatus "google.golang.org/grpc/status"
)

func TestWrap(t *testing.T) {
	tests := []struct {
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

	if !Is(wrapped1, base) {
		t.Error("wrapped1 should match base via errors.Is")
	}
	if !Is(wrapped2, wrapped1) {
		t.Error("wrapped2 should match wrapped1 via errors.Is")
	}
	if !Is(wrapped2, base) {
		t.Error("wrapped2 should match base via errors.Is")
	}
	if !Is(wrapped3, wrapped2) {
		t.Error("wrapped3 should match wrapped2 via errors.Is")
	}
	if !Is(wrapped3, wrapped1) {
		t.Error("wrapped3 should match wrapped1 via errors.Is")
	}
	if !Is(wrapped3, base) {
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
	if !Is(err, base) {
		t.Error("Wrapf result should match base via errors.Is")
	}
}

func TestStackDepthCapped(t *testing.T) {
	err := New("test")
	// Assert on Callers (PCs) since StackFrame may expand due to inlining
	if len(err.Callers()) > defaultStackDepth {
		t.Errorf("callers depth %d exceeds max %d", len(err.Callers()), defaultStackDepth)
	}
	// StackFrame is also capped at len(pcs)
	if len(err.StackFrame()) > defaultStackDepth {
		t.Errorf("frame depth %d exceeds max %d", len(err.StackFrame()), defaultStackDepth)
	}
}

func TestStackDepthCappedDeep(t *testing.T) {
	var createDeepError func(depth int) ErrorExt
	createDeepError = func(depth int) ErrorExt {
		if depth == 0 {
			return New("deep error")
		}
		return createDeepError(depth - 1)
	}

	err := createDeepError(100)
	if len(err.Callers()) > defaultStackDepth {
		t.Errorf("callers depth %d exceeds max %d", len(err.Callers()), defaultStackDepth)
	}
	if len(err.Callers()) == 0 {
		t.Error("callers should not be empty")
	}
}

func TestSetMaxStackDepth(t *testing.T) {
	// Save and restore
	prev := atomicStackDepth.Load()
	defer atomicStackDepth.Store(prev)

	SetMaxStackDepth(8)
	if got := atomicStackDepth.Load(); got != 8 {
		t.Fatalf("expected depth 8, got %d", got)
	}

	// Zero is ignored
	SetMaxStackDepth(0)
	if got := atomicStackDepth.Load(); got != 8 {
		t.Fatalf("expected depth 8 (unchanged), got %d", got)
	}

	// Negative is ignored
	SetMaxStackDepth(-1)
	if got := atomicStackDepth.Load(); got != 8 {
		t.Fatalf("expected depth 8 (unchanged), got %d", got)
	}

	// Over 256 is ignored
	SetMaxStackDepth(1000)
	if got := atomicStackDepth.Load(); got != 8 {
		t.Fatalf("expected depth 8 (unchanged), got %d", got)
	}

	// 256 is accepted
	SetMaxStackDepth(256)
	if got := atomicStackDepth.Load(); got != 256 {
		t.Fatalf("expected depth 256, got %d", got)
	}
}

func TestIs(t *testing.T) {
	base := stderrors.New("base")
	wrapped := Wrap(base, "wrapped")

	if !Is(wrapped, base) {
		t.Error("Is should find base through ColdBrew wrap")
	}
	if Is(wrapped, stderrors.New("other")) {
		t.Error("Is should not match unrelated error")
	}
	if !Is(wrapped, wrapped) {
		t.Error("Is should match error against itself")
	}
}

func TestAs(t *testing.T) {
	grpcErr := NewWithStatus("not found", grpcstatus.New(5, "not found"))
	wrapped := Wrap(grpcErr, "lookup failed")

	var ext ErrorExt
	if !As(wrapped, &ext) {
		t.Fatal("As should find ErrorExt in chain")
	}
	if ext.Error() != "lookup failed: not found" {
		t.Errorf("As target = %q, want %q", ext.Error(), "lookup failed: not found")
	}
}

func TestUnwrap(t *testing.T) {
	base := stderrors.New("base")
	wrapped := Wrap(base, "ctx")

	unwrapped := Unwrap(wrapped)
	if unwrapped == nil {
		t.Fatal("Unwrap should return non-nil for wrapped error")
	}
	if unwrapped != base {
		t.Errorf("Unwrap = %v, want %v", unwrapped, base)
	}

	// Unwrap on a non-wrapping error returns nil
	plain := stderrors.New("plain")
	if Unwrap(plain) != nil {
		t.Error("Unwrap on non-wrapping error should return nil")
	}
}

func TestJoin(t *testing.T) {
	err1 := New("first")
	err2 := stderrors.New("second")

	joined := Join(err1, err2)
	if joined == nil {
		t.Fatal("Join should return non-nil")
	}
	if !Is(joined, err1) {
		t.Error("Join result should contain first error")
	}
	if !Is(joined, err2) {
		t.Error("Join result should contain second error")
	}

	// All nils returns nil
	if Join(nil, nil) != nil {
		t.Error("Join of all nils should return nil")
	}
}

func TestErrUnsupported(t *testing.T) {
	err := stderrors.ErrUnsupported
	if !Is(err, ErrUnsupported) {
		t.Error("ErrUnsupported should match stdlib ErrUnsupported via Is")
	}
}

func TestCauseStandalone(t *testing.T) {
	// Works on ColdBrew error chain
	root := stderrors.New("root")
	a := Wrap(root, "a")
	b := Wrap(a, "b")

	if got := Cause(b); got != root {
		t.Errorf("Cause(b) = %v, want %v", got, root)
	}
	if got := Cause(a); got != root {
		t.Errorf("Cause(a) = %v, want %v", got, root)
	}

	// Works on plain stdlib errors (no Unwrap)
	plain := stderrors.New("plain")
	if got := Cause(plain); got != plain {
		t.Errorf("Cause(plain) = %v, want %v", got, plain)
	}

	// Returns nil for nil
	if got := Cause(nil); got != nil {
		t.Errorf("Cause(nil) = %v, want nil", got)
	}

	// Works on stdlib fmt.Errorf chains
	import_io_err := io.EOF
	fmtWrapped := stderrors.Join(import_io_err)
	// Join uses Unwrap() []error, not Unwrap() error, so Cause returns itself
	if got := Cause(fmtWrapped); got != fmtWrapped {
		t.Errorf("Cause(joinedErr) = %v, want %v", got, fmtWrapped)
	}
}

func TestStackFrameConsistency(t *testing.T) {
	err := New("consistency test")

	// First call resolves lazily
	frames1 := err.StackFrame()
	// Second call returns cached result
	frames2 := err.StackFrame()

	if len(frames1) != len(frames2) {
		t.Fatalf("frame count changed: %d vs %d", len(frames1), len(frames2))
	}
	for i := range frames1 {
		if frames1[i] != frames2[i] {
			t.Fatalf("frame %d differs: %+v vs %+v", i, frames1[i], frames2[i])
		}
	}

	// Callers should be non-empty
	pcs := err.Callers()
	if len(pcs) == 0 {
		t.Fatal("Callers() should not be empty")
	}
	// Frames should not exceed PCs (inlining can expand, but resolveFrames caps)
	if len(frames1) > len(pcs) {
		t.Fatalf("StackFrame count %d exceeds Callers count %d", len(frames1), len(pcs))
	}
}
