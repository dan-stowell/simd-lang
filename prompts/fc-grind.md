You are running the test oracle of a new Go compiler, `fc` (driver/fc in this repository; header comments of
driver/fc/main.go and driver/fc/gen.go describe it). `tools/fc/test.sh <import path> [go test flags]` runs a
standard-library package's own tests with that one package compiled by fc (everything else by the stock Go
1.27.3 compiler, with inlining off: -gcflags=all=-l). `tools/fc/batch.sh -j 4 <pkgs...>` runs many and prints
PASS / FAIL / NOFC / NOTEST per package. Use `taskset -c 4-7` for heavy runs. First copy build/fc to
/tmp/fc-grind/fc and export FC_BIN=/tmp/fc-grind/fc so a rebuild by the compiler's author does not change
the binary under you.

You may edit ONLY tests/fc/skips.txt and tests/fc/passing.txt. Never edit driver/ or tools/. Do not commit.

Job 1: the oracle count. Run every standard-library package (`CGO_ENABLED=0 go list std`, minus vendor/,
runtime/race and */internal/example*) through tools/fc/batch.sh. Write the sorted list of PASS packages to
tests/fc/passing.txt (one import path per line).

Job 2: the skip list. tests/fc/skips.txt lists tests the dev build is excused from (format and the only
allowed reasons are in its header). For every FAIL package: run the same package with the stock compiler
and the harness's flags (`CGO_ENABLED=0 go test -count=1 -gcflags=all=-l <pkg>`).
 - Tests that fail there too depend on inlining: add a line
   `<pkg> ^(TestA|TestB)$ fails with the stock compiler too when inlining is off (-gcflags=all=-l)`.
 - Tests whose only failing assertions are allocation counts (testing.AllocsPerRun, "allocs", "allocations")
   under fc: add a line with the reason `asserts allocation counts that need escape analysis`.
 - Tests that wait on finalizers / weak pointers / cleanup of objects that fc keeps alive until the function
   returns: reason `waits on object liveness`.
 - Nothing else may be skipped. After adding lines for a package, rerun it with tools/fc/test.sh (it applies
   the skips) and, if it now passes, add it to passing.txt.

Job 3: what is left. For every package that still fails or does not compile, write a section in the report
(path below) as in the previous triage (reviews/inbox/fc-triage-1.md shows the form): class BUG / CRASH /
NOFC / TIMEOUT, the deciding log lines, and for BUG and CRASH a reduced standalone case (a 10-30 line package
with an external _test file under /tmp/fc-grind/<name>, run with `tools/fc/test.sh -C <dir> <module>/<pkg>`;
`git init` the directory first) with expected and observed behaviour and, if you can see it, the cause in
driver/fc. Packages many others depend on first. Reduced cases are the most valuable output.

Job 4: MinIO. /home/exedev/targets/minio is built whole-program by fc like this:
  cd /home/exedev/targets/minio && CGO_ENABLED=0 GOFLAGS=-buildvcs=false GOCACHE=/home/exedev/targets/fc-gocache go list -deps -export -json -gcflags=all=-l . runtime > /tmp/fc-grind/minio.plan.json
  $FC_BIN build -plan /tmp/fc-grind/minio.plan.json -o /tmp/fc-grind/minio-objs -link /tmp/fc-grind/minio.bin github.com/minio/minio
then `tests/minio_smoke.sh /tmp/fc-grind/minio.bin 19200` (from the repository root). The server currently
panics with a nil pointer dereference in github.com/minio/minio/cmd.configRetriableErrors
(cmd/server-main.go:511) after logging `failed to load rebalance metadata ... %!w(<nil>)`. Find the first
point where the fc-built server's behaviour differs from a stock build and reduce it to a standalone case as
in job 3 (a small main package built with `fc build -link` works the same way: `go list -deps -export -json
-gcflags=all=-l . runtime > plan`, then `fc build -plan plan -o objs -link bin <module path>`; setting
FC_KEEP_STOCK=<pkg,pkg> keeps those packages and what they import stock-compiled, which bisects by package).
Report every distinct failure you find on the way to a passing smoke test, each with its reduced case.

Write the report incrementally to the path given below. End it with the counts (pass / fail / not compiled),
the lines you added to skips.txt, and the list of open BUG/CRASH items.
