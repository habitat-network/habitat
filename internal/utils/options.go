package utils

type Opt[T any] func(*T)

func ResolveOptions[T any](def T, opts []Opt[T]) T {
	for _, opt := range opts {
		opt(&def)
	}
	return def
}
