// Package globals exercises package-level variables, their initialization order,
// init functions and constant declarations.
// Imports: errors (in vars.go, for an error-valued global).
//
// The go command hands the files to the compiler sorted by name (a.go, b.go, c.go,
// consts.go, vars.go); declaration order across files follows that order.
package globals

// order records the order in which initializers ran.
var order []string

func rec(name string, v int) int {
	order = append(order, name)
	return v
}

// Declaration order here is V1 V2 V3 V4; dependencies reach into b.go and c.go.
var V1 = rec("V1", V2+1)      // needs V2 (declared later in this file)
var V2 = rec("V2", V5*2)      // needs V5 (c.go)
var V3 = rec("V3", 3)         // needs nothing
var V4 = rec("V4", viaFunc()) // needs V6 (b.go), but only through a function body

func viaFunc() int { return V6 + 100 }

var initLog []string
var sawAtInit [4]int

func init() {
	initLog = append(initLog, "a1")
	// every package-level variable is initialized before any init function runs
	sawAtInit[0] = V1 + V9 + len(M) + len(Table)
}

func init() {
	initLog = append(initLog, "a2")
	sawAtInit[1] = len(initLog)
	Ring.Next.Next = Ring // close a cycle that a static initializer may not express
}

// Order returns the names of the recorded initializers, in the order they ran.
func Order() []string { return order }

// InitLog returns the init functions' marks in the order they ran.
func InitLog() []string { return initLog }

func SawAtInit() [4]int { return sawAtInit }

func Vs() [9]int { return [9]int{V1, V2, V3, V4, V5, V6, V7, V8, V9} }
