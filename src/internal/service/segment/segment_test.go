package segment

import (
	"sync"
	"testing"
)

func TestGenerator_Next(t *testing.T) {
	gen := New()

	seg1 := gen.Next("int-123")
	if seg1 != "int-123-seg-1" {
		t.Errorf("expected 'int-123-seg-1', got %s", seg1)
	}

	seg2 := gen.Next("int-123")
	if seg2 != "int-123-seg-2" {
		t.Errorf("expected 'int-123-seg-2', got %s", seg2)
	}

	seg3 := gen.Next("int-456")
	if seg3 != "int-456-seg-3" {
		t.Errorf("expected 'int-456-seg-3', got %s", seg3)
	}
}

func TestGenerator_ThreadSafety(t *testing.T) {
	gen := New()
	numGoroutines := 100
	resultsPerGoroutine := 10

	var wg sync.WaitGroup
	results := make(chan string, numGoroutines*resultsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < resultsPerGoroutine; j++ {
				results <- gen.Next("int-concurrent")
			}
		}()
	}

	wg.Wait()
	close(results)

	// Collect all segment IDs
	seen := make(map[string]bool)
	for seg := range results {
		if seen[seg] {
			t.Errorf("duplicate segment ID generated: %s", seg)
		}
		seen[seg] = true
	}

	expectedCount := numGoroutines * resultsPerGoroutine
	if len(seen) != expectedCount {
		t.Errorf("expected %d unique segment IDs, got %d", expectedCount, len(seen))
	}
}

func TestGenerator_CounterMonotonic(t *testing.T) {
	gen := New()

	var prevNum uint64 = 0
	for i := 0; i < 100; i++ {
		seg := gen.Next("int-test")
		// Extract number from segment ID (format: "int-test-seg-N")
		var num uint64
		_, err := parseSegmentNumber(seg, &num)
		if err != nil {
			t.Fatalf("failed to parse segment: %s", seg)
		}
		if num <= prevNum {
			t.Errorf("counter not monotonic: %d <= %d", num, prevNum)
		}
		prevNum = num
	}
}

// Helper to parse segment number
func parseSegmentNumber(seg string, num *uint64) (bool, error) {
	var prefix string
	n, err := parseSegFormat(seg, &prefix, num)
	return n == 2, err
}

func parseSegFormat(seg string, prefix *string, num *uint64) (int, error) {
	var n int
	_, err := scanSegment(seg, prefix, num, &n)
	return n, err
}

func scanSegment(seg string, prefix *string, num *uint64, count *int) (bool, error) {
	// Simple parser for "prefix-seg-N" format
	for i := len(seg) - 1; i >= 0; i-- {
		if seg[i] == '-' {
			// Found last dash, parse number after it
			numStr := seg[i+1:]
			var n uint64
			for _, c := range numStr {
				if c >= '0' && c <= '9' {
					n = n*10 + uint64(c-'0')
				}
			}
			*num = n
			*count = 2
			return true, nil
		}
	}
	return false, nil
}

func TestGenerator_DifferentInteractions(t *testing.T) {
	gen := New()

	// Generate segments for different interactions
	seg1a := gen.Next("int-A")
	seg1b := gen.Next("int-B")
	seg2a := gen.Next("int-A")

	// All should be unique
	if seg1a == seg1b || seg1a == seg2a || seg1b == seg2a {
		t.Error("segment IDs should all be unique across interactions")
	}

	// Counter should be shared (global)
	// seg1a = "int-A-seg-1", seg1b = "int-B-seg-2", seg2a = "int-A-seg-3"
	if seg1a != "int-A-seg-1" {
		t.Errorf("expected 'int-A-seg-1', got %s", seg1a)
	}
	if seg1b != "int-B-seg-2" {
		t.Errorf("expected 'int-B-seg-2', got %s", seg1b)
	}
	if seg2a != "int-A-seg-3" {
		t.Errorf("expected 'int-A-seg-3', got %s", seg2a)
	}
}
