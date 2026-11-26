// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package loki

import (
	"testing"
)

// Two streams can interleave lines within one second. The timestamps shown to
// the user have second precision, so ordering must use the nanosecond value
// Loki sent, or lines from the same second come out scrambled.
func TestParseLokiStreamsOrdersLinesWithinTheSameSecondByNanosecond(t *testing.T) {
	// Arrange: stream A's line is 400ms after stream B's, same second.
	resp := lokiQueryResponse{Data: lokiResultData{Result: []lokiStream{
		{Stream: map[string]string{"step": "a"}, Values: [][]string{{"1700000000500000000", "second"}}},
		{Stream: map[string]string{"step": "b"}, Values: [][]string{{"1700000000100000000", "first"}}},
	}}}

	// Act
	entries, err := parseLokiStreams(resp)

	// Assert
	if err != nil {
		t.Fatalf("parseLokiStreams: %v", err)
	}
	if len(entries) != 2 || entries[0].Content != "first" || entries[1].Content != "second" {
		t.Fatalf("order = %+v, want first then second", entries)
	}
}

func TestParseLokiStreamsRejectsATimestampThatIsNotNanoseconds(t *testing.T) {
	resp := lokiQueryResponse{Data: lokiResultData{Result: []lokiStream{
		{Stream: map[string]string{"step": "a"}, Values: [][]string{{"yesterday", "line"}}},
	}}}

	if _, err := parseLokiStreams(resp); err == nil {
		t.Fatal("parseLokiStreams accepted a timestamp Loki never sends")
	}
}
