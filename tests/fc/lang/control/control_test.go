package control_test

import (
	"testing"

	"fclang/control"
	"fclang/internal/chk"
)

func TestMain(m *testing.M) { chk.Main(m) }

// Every exported function against its stock-compiled copy (control_ref_test.go).  Loop bounds and recursion
// depths get small pools; everything else runs over chk's integer patterns.
func TestAuto(t *testing.T) {
	ns := []any{0, 1, 2, 3, 4, 5, 7, 8, 10, 51, 100, 1000, -1, -5}
	n := []chk.Opt{chk.Arg(0, ns...)}
	nm := []any{0, 1, 2, 3, 5, 10, 30, -1}
	sw := []any{-2, -1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 255, 1000000, 1 << 40}
	chk.All(t, autoFuncs, map[string][]chk.Opt{
		"For3":                n,
		"ForEver":             n,
		"ForRangeInt":         n,
		"ForRangeIntNoVar":    n,
		"ForRangeIntOnce":     n,
		"ForMulti":            n,
		"ForNoPost":           n,
		"ForDown":             {chk.Arg(0, 0, 1, 2, 10, 1000)},
		"ForContinue":         n,
		"ForModifyVar":        n,
		"Goto":                n,
		"ForCond":             {chk.Arg(0, 0, 1, 2, 3, 6, 7, 27, 97, 871, -5, 1<<40)},
		"ForNested":           {chk.Arg(0, nm...), chk.Arg(1, nm...)},
		"LabeledBreak":        {chk.Arg(0, nm...), chk.Arg(1, nm...), chk.Arg(2, 0, 1, 6, 12, 25, 1000, -1), chk.Limit(448)},
		"LabeledContinue":     {chk.Arg(0, nm...), chk.Arg(1, nm...), chk.Arg(2, 0, 1, 2, 5, 100, -3), chk.Limit(384)},
		"LabeledRange3":       {chk.Arg(1, 0, 1, 2, 3, 5, 7, 8, 10, 63, 64, 65, 100, -1), chk.Limit(1500)},
		"Switch":              {chk.Arg(0, sw...)},
		"SwitchDefaultFirst":  {chk.Arg(0, sw...)},
		"SwitchDefaultMiddle": {chk.Arg(0, sw...)},
		"SwitchNoMatch":       {chk.Arg(0, sw...)},
		"SwitchFallthrough":   {chk.Arg(0, sw...)},
		"SwitchDense":         {chk.Arg(0, sw...)},
		"SwitchInit":          {chk.Arg(0, append([]any{51, 52, 53, 54, 55, -6}, sw...)...)},
		"SwitchSparse":        {chk.Arg(0, append([]any{-1 << 63, -1000000007, 65536, 1<<63 - 1, 1<<40 + 1, 254, 256, -1000000006}, sw...)...)},
		"SwitchNonConst":      {chk.Arg(0, 0, 1, 2, 3, 4, -1), chk.Arg(1, 0, 1, 2, 3), chk.Arg(2, 0, 1, 2, 3)},
	})
}

func TestByHand(t *testing.T) {
	if control.ForRangeIntOnce(5) != 10 || control.ForModifyVar(10) != 1+2+3+5+6+7+9 || control.ForRangeInt8(127) != 126*127/2 || control.ForRangeInt(-3) != 0 {
		t.Error("for")
	}
	if control.LabeledBreak(10, 10, 12) != 2*10+6 || control.LabeledContinue(3, 5, 1) != 1+2+3 || control.Goto(5) != 10 || control.GotoForward(-3) != -1 {
		t.Error("labels and goto")
	}
	if control.SwitchFallthrough(0) != 111 || control.SwitchFallthrough(3) != 11000 || control.SwitchFallthrough(9) != 10000 || control.SwitchDefaultMiddle(9) != 19 || control.SwitchDefaultMiddle(2) != 20 || control.SwitchNoMatch(3) != 5 {
		t.Error("switch")
	}
}
