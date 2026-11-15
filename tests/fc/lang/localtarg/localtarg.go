// Package localtarg: a type declared in a function, used as a type argument. The instance's name (with the
// type's "·N") is asked for when the call is compiled, which in a package this small is before any type
// descriptor is written. (Its own package: in a larger one something else writes a descriptor first.)
package localtarg

type SelfFunc[T any] interface{ ~func() T }

func CallSelf[T SelfFunc[T]]() T {
	var t T
	t = func() T { return t }
	return t
}

func LocalSelfFunc() bool {
	type Bar func() Bar
	return CallSelf[Bar]() != nil
}
