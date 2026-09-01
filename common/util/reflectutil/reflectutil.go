package reflectutil

import (
	"errors"
	"reflect"
	"server/common/util/panicutil"
)

func GetNonPtrTyp(rt reflect.Type) reflect.Type {
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	return rt
}

func MustGetStructTyp(typ reflect.Type) reflect.Type {
	t, err := GetStructTyp(typ)
	panicutil.Must(err)
	return t
}
func GetStructTyp(typ reflect.Type) (reflect.Type, error) {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
		return GetStructTyp(typ)
	}
	if typ.Kind() != reflect.Struct {
		return nil, errors.New("not struct type")
	}
	return typ, nil
}

func MustGetStructValue(val reflect.Value) reflect.Value {
	v, err := GetStructValue(val)
	panicutil.Must(err)
	return v
}
func GetStructValue(val reflect.Value) (reflect.Value, error) {
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
		return GetStructValue(val)
	}
	if val.Kind() != reflect.Struct {
		return reflect.Value{}, errors.New("not struct type")
	}
	return val, nil
}

func GetStructFieldValue(val reflect.Value, fieldName string) (fieldValue reflect.Value, ok bool, err error) {
	val, err = GetStructValue(val)
	if err != nil {
		return
	}
	fieldValue = val.FieldByName(fieldName)
	if fieldValue.IsZero() {
		return
	}
	return fieldValue, true, nil
}
