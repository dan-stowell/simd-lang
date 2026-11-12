package chk

import "reflect"

func reflectValue(x any) reflect.Value { return reflect.ValueOf(x) }
func reflectType(x any) reflect.Type   { return reflect.TypeOf(x) }
