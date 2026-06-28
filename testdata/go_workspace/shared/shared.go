package shared

func Normalize(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
