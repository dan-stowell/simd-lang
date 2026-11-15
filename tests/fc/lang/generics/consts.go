package generics

// Constants converted to a type parameter: T(0) is not a constant in the generic body, but it is one in
// each instance (a complex64 is 8 bytes like the int and the float64 it is converted from).

type Complex interface{ ~complex64 | ~complex128 }

func CZero[T Complex]() T   { return T(0) }
func CPi[T Complex]() T     { return T(3.14) }
func CSqrtN1[T Complex]() T { return T(-1i) }

type Floaty interface{ ~float32 | ~float64 }

func FConst[T Floaty]() T { return T(1) + T(0.5) }

// ConstConversions makes the instances in the package (not in the test, which the stock compiler compiles).
func ConstConversions() []any {
	return []any{CZero[complex128](), CPi[complex128](), CSqrtN1[complex128](),
		CZero[complex64](), CPi[complex64](), CSqrtN1[complex64](), FConst[float32](), FConst[float64]()}
}
