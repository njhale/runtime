package helper

import (
	"errors"
)

func Must(errs ...error) {
	if err := errors.Join(errs...); err != nil {
		panic(err)
	}
}

func MustReturn[T any](f func() (T, error)) T {
	o, err := f()
	Must(err)
	return o
}

func MustBe[T any](o T, err error) T {
	Must(err)
	return o
}
