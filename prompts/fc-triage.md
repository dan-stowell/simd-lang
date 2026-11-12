You are triaging test failures of a new Go compiler, `fc` (driver/fc in this repository; read the header
comments of driver/fc/main.go and driver/fc/gen.go first). fc is tested one package at a time in "mixed
mode": `tools/fc/test.sh <import path> [go test flags]` runs a standard-library package's own tests with that
one package compiled by fc and everything else by the stock Go 1.27.3 compiler. Do NOT edit anything under
driver/ or tools/; your output is a report.

Input: the file named at the end of this prompt lists packages whose tests FAIL (the package compiled, the
tests did not pass) and where each log is. For each package:

1. Re-run it (`taskset -c 4-7 tools/fc/test.sh <pkg> -count=1 2>&1 | head -80`; add `-run <Test>` to narrow).
   Confirm the same package passes with the stock compiler (`CGO_ENABLED=0 go test -count=1 -gcflags=all=-l <pkg>`);
   if it fails there too, classify it ENV and move on.
2. Classify the failure as exactly one of:
   - ALLOC: the failing assertions are about allocation counts or escape analysis (testing.AllocsPerRun,
     "allocs", "expected no allocations", benchmark-derived limits) and nothing else fails. Give the regular
     expression of the failing test names, for tests/fc/skips.txt.
   - LIVENESS: waits on finalizers, weak pointers being cleared, or objects being collected at a point where
     fc keeps them alive (fc keeps every local alive until the function returns).
   - INLINE/OPT: asserts something only an optimizing compiler does (inlining, bounds-check elimination,
     code size, stack frame size, exact stack depth, exact traceback text of wrappers).
   - BUG: fc generates wrong code or wrong metadata. For these, reduce: find the smallest function in the
     package (or write a 10-30 line standalone package under /tmp with an external _test file, run with
     `tools/fc/test.sh -C <dir> <module>/<pkg>`) whose behaviour differs between fc and stock, and say what
     differs (inputs, expected, got). If you can see the cause in driver/fc, name the file and function.
   - CRASH: the test binary dies (segfault, fatal error, runtime throw) or does not link. Give the first 15
     lines of the failure and, if you can, the reduced case as for BUG.
   - TIMEOUT/SLOW: passes but far slower than stock, or runs past the timeout. Give both times.
3. One section per package in the report: package, class, the evidence (the few log lines that decide it),
   the reduced case if any (inline the source), your confidence.

Order: start with the packages many others depend on (bytes, strings, strconv, sort, sync, reflect, fmt,
os, io, time, errors, unicode, math, encoding/*), then the rest. Write the report incrementally (append a
section as soon as a package is done) to the path given below, and end it with a table of counts per
class and the proposed tests/fc/skips.txt lines (format: `<import path> <regexp> <reason>`). Reduced BUG
and CRASH cases are the most valuable part: prefer depth on those over finishing every package.
