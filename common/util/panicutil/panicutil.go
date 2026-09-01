package panicutil

import "errors"

func Must(err error) {
	if err != nil {
		panic(err)
	}
}

func Must2(str string, err error) {
	if err != nil {
		panic(str + " " + err.Error())
	}
}

func MustTrue(ok bool, err string) {
	if !ok {
		panic(errors.New(err))
	}
}
func MustTrue2(ok bool, err error) {
	if !ok {
		panic(err)
	}
}

func MustExist(v interface{}) {
	if v == nil {
		panic(errors.New("nil"))
	}
}

func MustT1[T any](arg T, err error) T {
	if err != nil {
		panic(err)
	}
	return arg
}

func MustT2[T1, T2 any](arg1 T1, arg2 T2, err error) (T1, T2) {
	if err != nil {
		panic(err)
	}
	return arg1, arg2
}
