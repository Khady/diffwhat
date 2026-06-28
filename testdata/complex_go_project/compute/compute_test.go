package compute_test

import (
	"testing"

	"example.com/diffwhat-fixture/compute"
)

func TestTransform(t *testing.T) {
	if compute.Transform(2) != 4 {
		t.Fatal("unexpected result")
	}
}

func TestAccumulator(t *testing.T) {
	accumulator := &compute.Accumulator{}
	if accumulator.Add(2) != 2 {
		t.Fatal("unexpected total")
	}
}
