// sb: one-pass branch sizing for the baseline build. Not part of Go.
//
// span6 assembles a function assuming every forward branch is short (rel8), and assembles the whole
// function again each time one turns out not to fit. Baseline code is bulky (most instructions carry a
// frame offset), so most functions with an if or a loop went round two to four times. Here a forward
// branch whose target is more than sbFarProgs instructions ahead starts out long (rel32). That is always
// valid; it costs 3-4 bytes when the guess was wrong, and span6's loop is still there for the branches
// that start short and do not fit.

package x86

import (
	"cmd/internal/obj"
	"fmt"
	"os"
	"strconv"
)

var sbOnePass = os.Getenv("SB_BASELINE") != "" && os.Getenv("SB_ASM_MULTIPASS") == ""
var sbAsmStat = os.Getenv("SB_ASMSTAT") != ""
var sbFarProgs = func() int64 {
	if n, err := strconv.Atoi(os.Getenv("SB_ASM_FAR")); err == nil {
		return int64(n)
	}
	return 12
}()

func sbPresize(text *obj.Prog) {
	if !sbOnePass {
		return
	}
	i := int64(0)
	for p := text; p != nil; p = p.Link {
		p.Pc = i // an index for now; span6 sets the real value before it reads it
		i++
	}
	for p := text; p != nil; p = p.Link {
		if p.To.Type != obj.TYPE_BRANCH || p.Back&branchBackwards != 0 {
			continue
		}
		q := p.To.Target()
		if q == nil || q == p || q.Pc-p.Pc <= sbFarProgs {
			continue
		}
		switch p.As {
		case AJCXZL, AJCXZQ, AJCXZW, ALOOP, ALOOPEQ, ALOOPNE, AXBEGIN:
			continue // short-only encodings
		}
		p.Back &^= branchShort
	}
}

func sbSpanStat(name string, passes int, size int64) {
	if sbAsmStat {
		fmt.Fprintf(os.Stderr, "sbasm %d %d %s\n", passes, size, name)
	}
}
