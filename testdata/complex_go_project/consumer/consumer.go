package consumer

import "example.com/diffwhat-fixture/compute"

func Direct(value int) int {
	return compute.Transform(value)
}

func Method(value int) int {
	accumulator := &compute.Accumulator{}
	return accumulator.Add(value)
}

type EmbeddedAccumulator struct {
	*compute.Accumulator
}

func PromotedMethod(value int) int {
	accumulator := EmbeddedAccumulator{Accumulator: &compute.Accumulator{}}
	return accumulator.Add(value)
}

func Generic(values []int) []int {
	return compute.Map(values, compute.Transform)
}

func ThroughInterface(transformer compute.Transformer, value int) int {
	return transformer.Transform(value)
}

func Concrete(value int) int {
	return compute.Doubler{}.Transform(value)
}
