package utils

type Opt[T any] func(*T)

func ResolveOptions[T any](opts ...Opt[T]) T {
	var result T
	for _, opt := range opts {
		opt(&result)
	}
	return result
}
