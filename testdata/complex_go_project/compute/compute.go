package compute

func Transform(value int) int {
	return value * 2
}

type Accumulator struct {
	total int
}

func (a *Accumulator) Add(value int) int {
	a.total += value
	return a.total
}

func Map[T any, R any](values []T, transform func(T) R) []R {
	result := make([]R, len(values))
	for index, value := range values {
		result[index] = transform(value)
	}
	return result
}

type Transformer interface {
	Transform(int) int
}

type Doubler struct{}

func (Doubler) Transform(value int) int {
	return Transform(value)
}
