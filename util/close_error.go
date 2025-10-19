package util

import "io"

func Close(c io.Closer, errorHandlers ...func(error)) {
	if err := c.Close(); err != nil {
		for _, f := range errorHandlers {
			f(err)
		}
	}
}
