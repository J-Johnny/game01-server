package reflectutil

import (
	"errors"
	"reflect"
	"server/common/util/panicutil"
)

// GenericTyper 泛型类型
// 通过该方法可以获取到某个泛型type中 T 的反射type
type GenericTyper interface {
	GenericType() []reflect.Type
}

func GetStructGenericType(rt reflect.Type) ([]reflect.Type, error) {
	var err error
	rt, err = GetStructTyp(rt)
	panicutil.Must(err)
	g, ok := reflect.New(rt).Interface().(GenericTyper)
	if !ok {
		return nil, errors.New("get wrong generic typer")
	}
	return g.GenericType(), nil
}
