package errors

import (
	"testing"
)

func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = New("benchmark error")
	}
}

func BenchmarkNewAndStackFrame(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		e := New("benchmark error")
		_ = e.StackFrame()
	}
}

func BenchmarkWrap(b *testing.B) {
	base := New("base error")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = Wrap(base, "wrapped")
	}
}

func BenchmarkNewDeepStack(b *testing.B) {
	b.ReportAllocs()
	var recurse func(depth int) ErrorExt
	recurse = func(depth int) ErrorExt {
		if depth == 0 {
			return New("deep error")
		}
		return recurse(depth - 1)
	}
	for b.Loop() {
		_ = recurse(32)
	}
}
