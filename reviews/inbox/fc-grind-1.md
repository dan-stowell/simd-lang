# fc frozen-build oracle grind

Work in progress. Go 1.27.3 linux/amd64, CGO disabled, stock dependencies with inlining disabled. Frozen compiler: `/tmp/fc-grind/fc`, SHA-256 `5e5c7bf273b5d31120011a788f4a8e68594393cd526a3ce786fcaf2743668f00`. Repository HEAD at start: `6bbcd5be02e54e5fba37bf000bd4d5df3d050e1b`. Existing escape-export compiler is enabled by the harness. No driver or tools edits.

The previous triage is at `reviews/done/fc-triage-1.md` (the requested inbox location has moved). Standard-library universe: 355 packages after excluding vendor/, runtime/race, and */internal/example*. MinIO universe: 84 packages. Input lists and batch transcripts are in `/tmp/fc-grind/`.

Runs use CPUs 4–7. Standard batch: `tools/fc/batch.sh -j 4`. MinIO batch: `FC_TIMEOUT=600 tools/fc/batch.sh -j 3 -C /home/exedev/targets/minio`. Because batch.sh does not accept test flags, a temporary PATH wrapper around go adds `-short -count=1` to MinIO test commands; the repository harness is unchanged.

## Early skip verification

`bytes`: stock fails only TestNewBufferShallow (`allocations occurred while shallow copying`). fc additionally fails TestEqual, TestIndex, TestLastIndex, and TestIndexRune, exclusively on allocation assertions. Added separate stock-inlining and escape-analysis lines; full rerun passes (`/tmp/fc-grind/rerun-bytes.log`).

`crypto/ed25519`: both compilers fail TestAllocations (fc 40 allocations, stock 35, expected zero). Added the stock-inlining line; full rerun passes (`/tmp/fc-grind/rerun-crypto_ed25519.log`).

`context`: fc TestAllocs reports WithCancel 6 allocations rather than 5, and WithTimeout 10 rather than 8; stock does not fail this test. Added the escape-analysis skip. Full rerun still fails ExampleAfterFunc_connection because socket creation is prohibited, as it is with stock. Environmental failures are not mislabeled as inlining dependencies or skipped.

## MinIO completed results

39 PASS / 7 FAIL (all same as stock) / 0 not compiled / 38 without tests. All 84 packages were run with `-short -count=1`, a 600-second outer limit, and CPUs 4–7. No MinIO skips added. No fc-specific differential was observed; early aborts limit coverage of the affected suites.

| Package | Result |
|---|---|
| github.com/minio/minio | NOTEST — no test files |
| github.com/minio/minio/cmd | FAIL — same as stock |
| github.com/minio/minio/docs/debugging/hash-set | NOTEST — no test files |
| github.com/minio/minio/docs/debugging/healing-bin | NOTEST — no test files |
| github.com/minio/minio/docs/debugging/s3-check-md5 | NOTEST — no test files |
| github.com/minio/minio/docs/debugging/xl-meta | NOTEST — no test files |
| github.com/minio/minio/docs/extensions/s3zip/examples/minio-go | NOTEST — no test files |
| github.com/minio/minio/internal/amztime | PASS |
| github.com/minio/minio/internal/arn | PASS |
| github.com/minio/minio/internal/auth | PASS |
| github.com/minio/minio/internal/bpool | PASS |
| github.com/minio/minio/internal/bucket/bandwidth | PASS |
| github.com/minio/minio/internal/bucket/encryption | PASS |
| github.com/minio/minio/internal/bucket/lifecycle | PASS |
| github.com/minio/minio/internal/bucket/object/lock | PASS |
| github.com/minio/minio/internal/bucket/replication | PASS |
| github.com/minio/minio/internal/bucket/versioning | PASS |
| github.com/minio/minio/internal/cachevalue | PASS |
| github.com/minio/minio/internal/color | NOTEST — no test files |
| github.com/minio/minio/internal/config | PASS |
| github.com/minio/minio/internal/config/api | NOTEST — no test files |
| github.com/minio/minio/internal/config/batch | NOTEST — no test files |
| github.com/minio/minio/internal/config/browser | NOTEST — no test files |
| github.com/minio/minio/internal/config/callhome | NOTEST — no test files |
| github.com/minio/minio/internal/config/compress | PASS |
| github.com/minio/minio/internal/config/dns | PASS |
| github.com/minio/minio/internal/config/drive | NOTEST — no test files |
| github.com/minio/minio/internal/config/etcd | PASS |
| github.com/minio/minio/internal/config/heal | NOTEST — no test files |
| github.com/minio/minio/internal/config/identity/ldap | NOTEST — no test files |
| github.com/minio/minio/internal/config/identity/openid | FAIL — same as stock |
| github.com/minio/minio/internal/config/identity/openid/provider | NOTEST — no test files |
| github.com/minio/minio/internal/config/identity/plugin | NOTEST — no test files |
| github.com/minio/minio/internal/config/identity/tls | NOTEST — no test files |
| github.com/minio/minio/internal/config/ilm | NOTEST — no test files |
| github.com/minio/minio/internal/config/lambda | NOTEST — no test files |
| github.com/minio/minio/internal/config/lambda/event | PASS |
| github.com/minio/minio/internal/config/lambda/target | NOTEST — no test files |
| github.com/minio/minio/internal/config/notify | NOTEST — no test files |
| github.com/minio/minio/internal/config/policy/opa | NOTEST — no test files |
| github.com/minio/minio/internal/config/policy/plugin | NOTEST — no test files |
| github.com/minio/minio/internal/config/scanner | NOTEST — no test files |
| github.com/minio/minio/internal/config/storageclass | PASS |
| github.com/minio/minio/internal/config/subnet | NOTEST — no test files |
| github.com/minio/minio/internal/crypto | PASS |
| github.com/minio/minio/internal/deadlineconn | FAIL — same as stock |
| github.com/minio/minio/internal/disk | PASS |
| github.com/minio/minio/internal/dsync | FAIL — same as stock |
| github.com/minio/minio/internal/etag | PASS |
| github.com/minio/minio/internal/event | PASS |
| github.com/minio/minio/internal/event/target | FAIL — same as stock |
| github.com/minio/minio/internal/grid | FAIL — same as stock |
| github.com/minio/minio/internal/handlers | PASS |
| github.com/minio/minio/internal/hash | PASS |
| github.com/minio/minio/internal/hash/sha256 | NOTEST — no test files |
| github.com/minio/minio/internal/http | FAIL — same as stock |
| github.com/minio/minio/internal/init | NOTEST — no test files |
| github.com/minio/minio/internal/ioutil | PASS |
| github.com/minio/minio/internal/jwt | PASS |
| github.com/minio/minio/internal/kms | PASS |
| github.com/minio/minio/internal/lock | PASS |
| github.com/minio/minio/internal/logger | NOTEST — no test files |
| github.com/minio/minio/internal/logger/message/audit | NOTEST — no test files |
| github.com/minio/minio/internal/logger/target/console | NOTEST — no test files |
| github.com/minio/minio/internal/logger/target/http | NOTEST — no test files |
| github.com/minio/minio/internal/logger/target/kafka | NOTEST — no test files |
| github.com/minio/minio/internal/logger/target/loggertypes | NOTEST — no test files |
| github.com/minio/minio/internal/logger/target/testlogger | NOTEST — no test files |
| github.com/minio/minio/internal/lsync | PASS |
| github.com/minio/minio/internal/mcontext | NOTEST — no test files |
| github.com/minio/minio/internal/mountinfo | PASS |
| github.com/minio/minio/internal/net | NOTEST — no test files |
| github.com/minio/minio/internal/once | NOTEST — no test files |
| github.com/minio/minio/internal/pubsub | PASS |
| github.com/minio/minio/internal/rest | PASS |
| github.com/minio/minio/internal/ringbuffer | PASS |
| github.com/minio/minio/internal/s3select | PASS |
| github.com/minio/minio/internal/s3select/csv | PASS |
| github.com/minio/minio/internal/s3select/json | PASS |
| github.com/minio/minio/internal/s3select/jstream | PASS |
| github.com/minio/minio/internal/s3select/parquet | NOTEST — no test files |
| github.com/minio/minio/internal/s3select/simdj | PASS |
| github.com/minio/minio/internal/s3select/sql | PASS |
| github.com/minio/minio/internal/store | PASS |

### github.com/minio/minio/internal/config/identity/openid — ABORT (same as stock; environmental/test failure)

Both compilers exit nonzero with the same failing tests and deciding error. This is not an established fc bug.

```text
--- FAIL: TestJWTHMACType (0.00s)
panic: httptest: failed to listen on a port: listen tcp6 [::1]:0: socket: operation not permitted [recovered, repanicked]

goroutine 36 [running]:
testing.tRunner.func1.2({0x107cef8, 0x13e60d7f50a0})
	/usr/local/go/src/testing/testing.go:2125 +0x23e
testing.tRunner.func1()
	/usr/local/go/src/testing/testing.go:2128 +0x3ef
panic({0x107cef8?, 0x13e60d7f50a0?})
	/usr/local/go/src/runtime/panic.go:859 +0x136
net/http/httptest.newLocalListener()
	/usr/local/go/src/net/http/httptest/server.go:226 +0xd9
```

Full fc log: `/tmp/fcbatch.Pm0XQJ/github.com_minio_minio_internal_config_identity_openid`. Stock log: `/tmp/fc-grind/minio-stock-github.com_minio_minio_internal_config_identity_openid.log` (exit 1).

The sandbox prohibits the listener/network setup required by these tests.

### github.com/minio/minio/cmd — ABORT (same as stock; environmental/test failure)

Both compilers exit nonzero with the same failing tests and deciding error. This is not an established fc bug.

```text
FATAL msg: Unable to get IP addresses of this host
       err:route ip+net: netlinkrib: operation not permitted
FAIL	github.com/minio/minio/cmd	1.456s
FAIL
```

Full fc log: `/tmp/fcbatch.Pm0XQJ/github.com_minio_minio_cmd`. Stock log: `/tmp/fc-grind/minio-stock-github.com_minio_minio_cmd.log` (exit 1).

Initialization exits before unit tests because netlink interface enumeration is prohibited.

### github.com/minio/minio/internal/deadlineconn — BUG (same as stock; environmental/test failure)

Both compilers exit nonzero with the same failing tests and deciding error. This is not an established fc bug.

```text
--- FAIL: TestBuffConnReadTimeout (0.00s)
    deadlineconn_test.go:34: unable to create listener. listen tcp 127.0.0.1:0: socket: operation not permitted
--- FAIL: TestBuffConnReadCheckTimeout (0.00s)
    deadlineconn_test.go:124: unable to create listener. listen tcp 127.0.0.1:0: socket: operation not permitted
FAIL
FAIL	github.com/minio/minio/internal/deadlineconn	0.012s
FAIL
```

Full fc log: `/tmp/fcbatch.Pm0XQJ/github.com_minio_minio_internal_deadlineconn`. Stock log: `/tmp/fc-grind/minio-stock-github.com_minio_minio_internal_deadlineconn.log` (exit 1).

The sandbox prohibits the listener/network setup required by these tests.

### github.com/minio/minio/internal/dsync — ABORT (same as stock; environmental/test failure)

Both compilers exit nonzero with the same failing tests and deciding error. This is not an established fc bug.

```text
panic: httptest: failed to listen on a port: listen tcp6 [::1]:0: socket: operation not permitted

goroutine 1 [running]:
net/http/httptest.newLocalListener()
	/usr/local/go/src/net/http/httptest/server.go:226 +0xd9
net/http/httptest.NewUnstartedServer({0x120a9b0, 0x9981a18f8c0})
	/usr/local/go/src/net/http/httptest/server.go:284 +0x25
net/http/httptest.NewServer({0x120a9b0?, 0x9981a18f8c0?})
	/usr/local/go/src/net/http/httptest/server.go:267 +0x1d
github.com/minio/minio/internal/dsync.startLockServers()
	/home/exedev/targets/minio/internal/dsync/dsync-server_test.go:178 +0x852
github.com/minio/minio/internal/dsync.TestMain()
```

Full fc log: `/tmp/fcbatch.Pm0XQJ/github.com_minio_minio_internal_dsync`. Stock log: `/tmp/fc-grind/minio-stock-github.com_minio_minio_internal_dsync.log` (exit 1).

The sandbox prohibits the listener/network setup required by these tests.

### github.com/minio/minio/internal/grid — ABORT (same as stock; environmental/test failure)

Both compilers exit nonzero with the same failing tests and deciding error. This is not an established fc bug.

```text
--- FAIL: TestDisconnect (0.00s)
panic: runtime error: index out of range [0] with length 0 [recovered, repanicked]

goroutine 6 [running]:
testing.tRunner.func1.2({0x1284898, 0x1d929596e558})
	/usr/local/go/src/testing/testing.go:2125 +0x23e
testing.tRunner.func1()
	/usr/local/go/src/testing/testing.go:2128 +0x3ef
panic({0x1284898?, 0x1d929596e558?})
	/usr/local/go/src/runtime/panic.go:859 +0x136
github.com/minio/minio/internal/grid.TestDisconnect()
	/home/exedev/targets/minio/internal/grid/connection_test.go:52 +0x239
```

Full fc log: `/tmp/fcbatch.Pm0XQJ/github.com_minio_minio_internal_grid`. Stock log: `/tmp/fc-grind/minio-stock-github.com_minio_minio_internal_grid.log` (exit 1).

Both panic at connection_test.go:52 indexing hosts[0] when the host list is empty; test network setup fails in this sandbox.

### github.com/minio/minio/internal/http — BUG (same as stock; environmental/test failure)

Both compilers exit nonzero with the same failing tests and deciding error. This is not an established fc bug.

```text
--- FAIL: TestCheckPortAvailability (0.00s)
    check_port_test.go:36: listen tcp 127.0.0.1:0: socket: operation not permitted
--- FAIL: TestNewHTTPListener (0.00s)
    listener_test.go:162: Test 5:, listenErrs[0] error: expected = <nil>, got = listen tcp 127.0.0.1:65432: socket: operation not permitted
--- FAIL: TestHTTPListenerStartClose (0.00s)
    listener_test.go:104: route ip+net: netlinkrib: operation not permitted.  Unable to get IP addresses of this host.
--- FAIL: TestHTTPListenerAddr (0.00s)
    listener_test.go:104: route ip+net: netlinkrib: operation not permitted.  Unable to get IP addresses of this host.
--- FAIL: TestHTTPListenerAddrs (0.00s)
    listener_test.go:104: route ip+net: netlinkrib: operation not permitted.  Unable to get IP addresses of this host.
--- FAIL: TestNewServer (0.00s)
    listener_test.go:104: route ip+net: netlinkrib: operation not permitted.  Unable to get IP addresses of this host.
```

Full fc log: `/tmp/fcbatch.Pm0XQJ/github.com_minio_minio_internal_http`. Stock log: `/tmp/fc-grind/minio-stock-github.com_minio_minio_internal_http.log` (exit 1).

The sandbox prohibits the listener/network setup required by these tests.

### github.com/minio/minio/internal/event/target — ABORT (same as stock; environmental/test failure)

Both compilers exit nonzero with the same failing tests and deciding error. This is not an established fc bug.

```text
--- FAIL: TestNatsConnPlain (10.01s)
panic: Unable to start NATS Server in Go Routine [recovered, repanicked]

goroutine 23 [running]:
testing.tRunner.func1.2({0x190c918, 0xfce900})
	/usr/local/go/src/testing/testing.go:2125 +0x23e
testing.tRunner.func1()
	/usr/local/go/src/testing/testing.go:2128 +0x3ef
panic({0x190c918?, 0xfce900?})
	/usr/local/go/src/runtime/panic.go:859 +0x136
github.com/nats-io/nats-server/v2/test.RunServerCallback(0x1a1a8a0?, 0x0)
	/home/exedev/go/pkg/mod/github.com/nats-io/nats-server/v2@v2.11.1/test/test.go:115 +0x255
```

Full fc log: `/tmp/fcbatch.Pm0XQJ/github.com_minio_minio_internal_event_target`. Stock log: `/tmp/fc-grind/minio-stock-github.com_minio_minio_internal_event_target.log` (exit 1).

The sandbox prohibits the listener/network setup required by these tests.

### Shared environmental reduction

`/tmp/fc-grind/socketenv` is a git-initialized standalone module with an external test (24 lines total). `Listen` calls `net.Listen("tcp", "127.0.0.1:0")` and closes the listener; the test expects no error. Both stock and fc instead report `socket: operation not permitted`. Reproduce with `FC_BIN=/tmp/fc-grind/fc tools/fc/test.sh -C /tmp/fc-grind/socketenv grind/socketenv -count=1`; stock: `CGO_ENABLED=0 go test -C /tmp/fc-grind/socketenv -count=1 -gcflags=all=-l ./...`. Logs are `fc.log` and `stock.log` in that directory. This is evidence for an environment restriction, not a reduced compiler defect. No differential MinIO reduction is warranted by these results.

## embed/internal/embedtest — ABORT

fc fails before linking: `fc: embed/internal/embedtest.global.files: defined, but not as an indexed symbol`. Full log: `/tmp/fcbatch.IXtQ2k/embed_internal_embedtest`. The reduced case below independently establishes a compiler differential.

## encoding/json/internal/jsontest — ABORT

fc rejects the embedded test-data filesystem: `fc: encoding/json/internal/jsontest.testdataFS.files: defined, but not as an indexed symbol`. Full log: `/tmp/fcbatch.IXtQ2k/encoding_json_internal_jsontest`. Same reduced defect as embed/internal/embedtest.

### Shared standalone embed.FS reduction

Git-initialized module `/tmp/fc-grind/embedfs`, `module grind/embedfs`, `go 1.27`, with data.txt containing `hello\n`:

```go
package embedfs

import "embed"

//go:embed data.txt
var files embed.FS

func Read() (string, error) {
 b, err := files.ReadFile("data.txt")
 return string(b), err
}
```

External test:

```go
package embedfs_test

import (
 "testing"
 "grind/embedfs"
)

func TestRead(t *testing.T) {
 got, err := embedfs.Read()
 if err != nil || got != "hello\n" {
  t.Fatalf("got %q, %v; want hello", got, err)
 }
}
```

Expected: read `hello\n` without error; stock passes. Observed: frozen fc aborts compilation with `fc: grind/embedfs.files.files: defined, but not as an indexed symbol`. Commands: `FC_BIN=/tmp/fc-grind/fc taskset -c 4-7 tools/fc/test.sh -C /tmp/fc-grind/embedfs grind/embedfs -count=1`; `CGO_ENABLED=0 taskset -c 4-7 go test -C /tmp/fc-grind/embedfs -count=1 -gcflags=all=-l ./...`. Logs: `/tmp/fc-grind/embedfs/{fc,stock}.log`.

Likely cause: `driver/fc/embed.go:199` creates the backing `.files` symbol with package `_`. `driver/fc/main.go:alignWithStock` excludes such symbols from its indexed-symbol map, but stock defines this symbol by index. Placeholder lookup encounters the already-defined symbol and terminates at main.go:248. High confidence in the differential and this alignment conflict; no compiler edits made.

## Additional skip evidence

Stock and fc both fail allocation-only tests in crypto/rand (TestAllocations), crypto/sha256 (TestAllocations and the literally named TestAllocatonsWithTypeAsserts), crypto/sha3 (TestAllocations), crypto/sha512 (TestAllocations), crypto/mldsa (TestAllocations), crypto/rsa (TestAllocations), encoding/json/jsontext (TestTokenStringAllocations), and fmt (TestCountMallocs). Stock/fc logs are `/tmp/fc-grind/std-stock-<package_with_slashes_replaced_by_underscores>.log` and `/tmp/fcbatch.IXtQ2k/<package_with_slashes_replaced_by_underscores>`. Added the exact stock-inlining reason. Rerun logs are `/tmp/fc-grind/rerun-<same-name>.log`; passing lists include only completed successful reruns.

crypto/internal/fips140test: TestEdwards25519Allocations, TestNISTECAllocations, and TestXAESAllocations fail allocation assertions under both compilers. Their new skip leaves TestACVP failing to download BoringSSL via proxy.golang.org because sockets are prohibited. TestACVP remains unskipped.

crypto/internal/fips140cache: initial batch timed out with no output. A diagnostic rerun using `-count=1 -timeout=15s` shows `TestCache` stopped at cache_test.go:45, in `waitUnreachable` at line 162 (`runtime.GC()`). That helper registers a cleanup, clears the caller's pointer, and waits for cleanup while the test and helper remain active. Stock passes in 1.371s. This matches fc's function-wide pointer maps and the permitted liveness exemption; added `^TestCache$ waits on object liveness`. Diagnostic log: `/tmp/fc-grind/fips-cache-timeout.log`.

## internal/chacha8rand — ABORT

Stock passes (`ok internal/chacha8rand 0.015s`); fc reports `signal: segmentation fault` in 0.003s. It also segfaults with `-run '^$' -count=1` in 0.001s, before any test body. Logs: `/tmp/fcbatch.IXtQ2k/internal_chacha8rand`, `/tmp/fc-grind/std-stock-internal_chacha8rand.log`, `/tmp/fc-grind/chacha-empty.log`.

Likely startup cause, now supported by generated-code evidence: runtime/proc.go:889 calls randinit before mallocinit. The fc-generated `internal/chacha8rand.(*State).Init` calls `runtime.newobject` at chacha8.go:64 to allocate its address-taken seed parameter. This occurs before the runtime allocator is initialized. The disassembly is `/tmp/fc-grind/chacha-init.asm` (test binary `/tmp/fc-grind/chacha.test`); its first call is `CALL runtime.newobject(SB)`. fc's heap placement for address-taken variables is documented in driver/fc/gen.go. High confidence in the startup allocation hazard; no debugger backtrace was available to prove the faulting instruction.

A 24-line standalone mechanism reduction is git-initialized at `/tmp/fc-grind/seedalloc`:

```go
package seedalloc

func Init(seed [32]byte) uint64 {
 return word(seed[0:8])
}
func word(b []byte) uint64 {
 return uint64(b[0]) | uint64(b[1])<<8
}
```

External test:

```go
package seedalloc_test
import (
 "testing"
 "grind/seedalloc"
)
var result uint64
func TestInit(t *testing.T) {
 n := testing.AllocsPerRun(10, func() {
  result = seedalloc.Init([32]byte{1, 2})
 })
 if result != 513 { t.Fatalf("got %d", result) }
 if n != 0 { t.Fatalf("seed initialization allocated %g times", n) }
}
```

Stock passes with zero allocations; fc returns the correct value but reports `seed initialization allocated 1 times`. Reproduce with `FC_BIN=/tmp/fc-grind/fc tools/fc/test.sh -C /tmp/fc-grind/seedalloc grind/seedalloc -count=1`; stock uses `CGO_ENABLED=0 go test -C /tmp/fc-grind/seedalloc -count=1 -gcflags=all=-l ./...`. Logs are `fc.log` and `stock.log` in that directory. This reduces the unsafe startup allocation mechanism, **not the startup crash itself**: ordinary module code runs after allocator initialization. No allocation skip is appropriate for the original startup abort.

## internal/abi — ABORT (harness export compiler)

Stock passes. The default harness rejects type.go:709 and :710 with `nameLen escapes to heap, not allowed in runtime` and `tagLen escapes to heap, not allowed in runtime`. This is a pre-fc rejection in `build/compile-esc`, not an fc-generated test crash. A diagnostic `FC_ESC_COMPILE= FC_BIN=/tmp/fc-grind/fc tools/fc/test.sh internal/abi -count=1` passes (`/tmp/fc-grind/abi-noesc.log`). The default-harness oracle result remains not compiled. Ordinary standalone modules do not receive runtime-package escape restrictions, so no faithful 10–30-line module reduction of this harness restriction is claimed.

## internal/trace/traceviewer — ABORT

`fc: internal/trace/traceviewer.staticContent.files: defined, but not as an indexed symbol`; see the confirmed shared embed.FS reduction above. Full log: `/tmp/fcbatch.IXtQ2k/internal_trace_traceviewer`.

## archive/zip — initial TIMEOUT, resolved by longer rerun

The initial 300-second outer batch limit terminated the command (`Terminated`). Stock passes in 92.879s. The unmodified fc suite, with a 600-second outer limit and `-count=1`, passes in 227.187s (`/tmp/fc-grind/rerun-archive_zip.log`). Included in passing.txt; no skip added.

## math/big — ABORT (generic method/function symbol collision)

fc aborts during initialization, before tests:

```text
runtime: goroutine stack exceeds 1000000000-byte limit
fatal error: stack overflow
math/rand/v2.N[...]()
    /usr/local/go/src/math/rand/v2/rand.go:337 +0x33
```

The same N frame repeats. Stock completes tests, failing only TestNewIntAllocs (allocation assertions with inlining disabled). That test was skipped under the allowed stock rule; the full fc rerun still aborts. Logs: `/tmp/fcbatch.IXtQ2k/math_big`, `/tmp/fc-grind/std-stock-math_big.log`, `/tmp/fc-grind/rerun-math_big.log`.

Two standalone differentials are confirmed: `/tmp/fc-grind/randn` calls `rand.N(uint(10))` and reproduces the recursion; the smaller fully self-contained `/tmp/fc-grind/genericmethod` needs no math/rand dependency. Both are git-initialized modules with external tests and stock PASS / fc fatal stack overflow.

```go
package genericmethod

type R struct{}
func (r *R) N[T ~uint](n T) T { return n - 1 }
func N[T ~uint](n T) T {
 var r R
 return r.N(n)
}
func Pick() uint { return N(uint(10)) }
```

External test:

```go
package genericmethod_test
import (
 "testing"
 "runtime/debug"
 "grind/genericmethod"
)
func TestPick(t *testing.T) {
 old := debug.SetMaxStack(1 << 20)
 defer debug.SetMaxStack(old)
 if got := genericmethod.Pick(); got != 9 { t.Fatalf("got %d, want 9", got) }
}
```

Expected 9; stock passes. fc infinitely recurses through `grind/genericmethod.N[...]` and aborts at the reduced 1 MiB stack limit. Run `FC_BIN=/tmp/fc-grind/fc taskset -c 4-7 tools/fc/test.sh -C /tmp/fc-grind/genericmethod grind/genericmethod -count=1`; stock: `CGO_ENABLED=0 taskset -c 4-7 go test -C /tmp/fc-grind/genericmethod -count=1 -gcflags=all=-l ./...`. Logs: `fc.log` and `stock.log` in the reduction directory.

Cause visible in driver/fc/generics.go:funcRef: the TypeParams branch (line 186) passes a nil receiver to instantiate even for a generic method on a non-generic receiver. instantiate names both declarations `pkg.N[uint]`, omitting the method receiver, and deduplicates by that name. This turns the method call into a recursive function call. High confidence; exact same-name reduction confirmed. No functional test skip added.

## runtime/pprof — ABORT (iter.Pull2 link targets)

fc fails to link:

```text
iter.Pull2[int,int]: relocation target iter.newcoro not defined
iter.Pull2[int,int].func1.func1: relocation target iter.coroswitch not defined
iter.Pull2[int,int].func2: relocation target iter.coroswitch not defined
iter.Pull2[int,int].func3: relocation target iter.coroswitch not defined
```

Confirmed standalone, git-initialized module `/tmp/fc-grind/pull`:

```go
package pull
import "iter"
func First() (int, int, bool) {
 next, stop := iter.Pull2(func(yield func(int, int) bool) { yield(1, 2) })
 defer stop()
 return next()
}
```

External test:

```go
package pull_test
import (
 "testing"
 "grind/pull"
)
func TestFirst(t *testing.T) {
 a, b, ok := pull.First()
 if a != 1 || b != 2 || !ok {
  t.Fatalf("got %d %d %v, want 1 2 true", a, b, ok)
 }
}
```

Expected `(1, 2, true)`; stock passes. fc fails to link with the four errors above. Commands: `FC_BIN=/tmp/fc-grind/fc taskset -c 4-7 tools/fc/test.sh -C /tmp/fc-grind/pull grind/pull -count=1`; `CGO_ENABLED=0 taskset -c 4-7 go test -C /tmp/fc-grind/pull -count=1 -gcflags=all=-l ./...`. Logs are `fc.log` and `stock.log` in that directory.

Cause: driver/fc/misc.go:Program.linknames handles `//go:linknamestd` without retaining the explicit target; iter's newcoro/coroswitch aliases should reference runtime.newcoro/runtime.coroswitch. The instantiated fc code instead references nonexistent iter symbols. High confidence; same defect independently reduced with the frozen compiler.
