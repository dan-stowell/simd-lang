package generics

// Types declared inside a generic function: the function's type parameters are implicit parameters of each
// of them, used or not (each instantiation of the function has its own types), and a generic one has its
// own besides. reflect names them T[<the function's type arguments>;<its own>].

type LI int

func LocalTypes[A any, A2 any]() []any {
	type L int
	type P struct{ a A }
	type G[B any] struct{}
	type H[B any] struct {
		b B
		a A
	}
	type Q[B any] int
	type List[B any] struct {
		next *List[B]
		v    B
		a    A
	}
	return []any{L(0), P{}, G[int]{}, H[int]{}, Q[int](0), G[Q[A]]{}, []L{}, func(L) {}, List[A2]{next: &List[A2]{}}}
}

// LocalTypeSets: the instances are made here, in the package (not in the test, which the stock compiler
// compiles): F[int,string] twice, F[LI,M] with M declared here, F[LI,string].
func LocalTypeSets() [][]any {
	type M int
	return [][]any{LocalTypes[int, string](), LocalTypes[LI, M](), LocalTypes[int, string](), LocalTypes[LI, string]()}
}

func LocalTypesPlain() []any {
	type L int
	type G[B any] struct{}
	return []any{L(0), G[int]{}, G[G[LI]]{}}
}
