package globals

var V6 = rec("V6", 6)

// a multi-value initializer: both variables are set by one call
var V7, V8 = pair()

func pair() (int, int) {
	order = append(order, "V7V8")
	return V3 + 1, V1 + 1
}

func init() {
	initLog = append(initLog, "b1")
	sawAtInit[2] = len(initLog) * 10
	for i := range Squares {
		Squares[i] = int32(i * i)
	}
}

func init() {
	initLog = append(initLog, "b2")
	M["init"] = len(initLog)
	Big[0].Name = "first"
	Big[len(Big)-1] = Rec{Name: "last", Val: len(Big), Next: &Big[0]}
}
