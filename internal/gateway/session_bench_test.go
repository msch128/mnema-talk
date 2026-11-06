package gateway

import (
	"encoding/json"
	"testing"
)

func BenchmarkSessionPushFullBuffer(b *testing.B) {
	s := &session{}
	d := json.RawMessage(`{"id":"1"}`)
	for i := 0; i < bufferSize; i++ {
		s.pushLocked("X", d)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.pushLocked("X", d)
	}
}
