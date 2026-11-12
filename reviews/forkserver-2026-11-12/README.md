# Compile-process review, 2026-11-12

Scope: commit `5784bdc`, fork-without-exec process creation only. No implementation or
existing tests changed. Standalone inputs/probes in this directory are review artifacts.

## Existing differentials

After rebuilding ignored `build/sb`, `build/compile-sb` from the current sources and
generating `build/sb.facts` (the supplied binaries were older):

- `tests/sb_zygote.sh "$PWD/reviews/early-export-2026-11-11/cases/baseline"`:
  PASS, 60 export/object files and final binary byte-identical; type error rejected.
- `tests/sb_early.sh "$PWD/reviews/early-export-2026-11-11/cases/baseline"`:
  PASS, idle patched compiler byte-identical to stock; early export has the same
  2,954 symbols, sizes/types, and section sizes.
- `tests/sb_zygote.sh /home/exedev/targets/minio`: PASS, 2,624 export/object files
  and final binary byte-identical between exec and fork, including the sharded
  package; type error rejected.

## Confirmed findings

### 1. Lost wait reply is interpreted as compile success (high)

Minimal input: `cases/wait-eof.go`.

Location: `driver/main.go:214-231`, especially the unchecked `Read` length and
unchecked `fmt.Sscan`. On orderly socket EOF, `Read` returns `(0, nil)`. Scanning the
empty reply leaves `st` zero, so `zygoteCompile` returns a nil error. This is a false
success at the compile-action boundary, not proof that a complete build with a missing
object will link. If artifacts are already complete when a failing/killed compiler
loses its wait reply, later stages cannot detect its nonzero status from this result.

Reproduce:

```
python3 reviews/forkserver-2026-11-12/wait-eof.py
```

Observed:

```
real compiler wait reply (discarded to model zygote death): 2 ...
zygoteCompile error: <nil>
```

The replay extracts the current driver functions verbatim into a temporary Go
program. A socket relay runs the **real** zygote/compiler, forwards the fork request,
descriptors, PID and wait request, then drops the real status-2 reply and closes the
driver-facing socket. This deterministically injects the EOF a zygote dying after
consuming `W` can produce; it does not claim a deterministic natural kill-window
reproduction. The exec path checks `cmd.Wait()` and would reject status 2.
Malformed replies can similarly be accepted because the scan result is ignored.

### 2. Valid exec environment exceeds the fork transport limit (medium)

Minimal input: `cases/socket-limit.go` (empty `main`).

Location: `driver/main.go:142,183,193`: environment and argv are serialized into one
`SOCK_SEQPACKET` message. The requested 4 MiB send buffer is silently capped on this
host: `net.core.wmem_max=212992`, actual `SO_SNDBUF=425984`. Five unused environment
variables, each containing 90,000 `x` bytes, fit the stock exec limits but not this
packet. This is unrelated to source syntax or the generated code.

Reproduce:

```
python3 reviews/forkserver-2026-11-12/probe.py socket-limit
```

Observed: stock compiler exit 0; patched exec exit 0; fork `EMSGSIZE` (errno 90).
Also confirmed end to end using the same source in a temporary module, identical
environment and driver flags (`-pipeline -noearly -compiler ...`):

```
driver [exec] exit 0
driver [-zygote] exit 1: sb: zygote: fork failed: message too long ""
```

Larger argv lists share the same transport limit. Separately, the receiver's fixed
1 MiB buffer ignores `MSG_TRUNC`; this host's smaller send limit prevents reaching
that branch, so truncation is a source-review risk, not a reproduced finding.

## Additional checks

- Existing giant-stdout regression source
  `reviews/early-export-2026-11-11/cases/stdout`, with `-zygote -pipeline -j 2
  -backends 4 -gcflags=-S`: build and binary PASS (6.2 s); no pipe-buffer deadlock.
- `python3 reviews/forkserver-2026-11-12/probe.py corpus`: PASS 173 inputs, comparing
  stock, patched exec, and fork exit status, stdout/stderr, and archive bytes.
  Includes 160 import-free inputs from `/usr/local/go/test`, plus generated generic
  aliases/receivers, type sets, Unicode, large constants, raw strings and line
  directives. Comment lengths exercise 63/64/65, 4095/4096/4097,
  65535/65536/65537 and 1048575/1048576/1048577 bytes. Zygote FD count after **every**
  request stayed at five; no per-request parent FD leak observed.
- `python3 reviews/forkserver-2026-11-12/probe.py signals`: stock/exec killed children
  return signal 9; fork reports nonzero status 137, then successfully compiles the
  next request. No false success when the zygote actually delivers the signal status.
- Additional runtime stress: 648 forked compiler processes, 220 generated
  allocation/loop-heavy functions, `-c 8`, `GOMAXPROCS=8`, four concurrent children
  per zygote. Three settings: `GOGC=1`; `GOGC=1` with
  `GODEBUG=gccheckmark=1,clobberfree=1`; `GOGC=100 GOMEMLIMIT=8MiB`. The longer 600-child
  run added 60 KB of environment data to pressure the parent's allocations.
  All child statuses zero, stderr empty, all 648 archives independently verified
  byte-identical to stock. No GC, scheduler or inherited-lock hang reproduced.

## Runtime/init review and limits

- `SBFork` keeps background goroutines and `allm` records for vanished threads;
  removing idle Ms is not a general-purpose reset of Go runtime state. Reviewed
  scheduler reuse and GC rooting paths, but found no additional **demonstrated
  compiler-path** counterexample. Successful stress does not prove the arbitrary
  fork-without-exec API safe, nor rule out rare runtime-lock races.
- Init-cached compiler/build configuration is not generally refreshed from a
  request's environment. In the current driver, those values are constant across
  the build: `SB_EARLY_EXPORT` is set consistently in zygote and requests;
  `SB_DROP_BODIES` is read dynamically; per-request shard state is explicitly
  refreshed in `sbzygote.go:102-103`. The full MinIO differential exercises changing
  shard environments. Arbitrarily changing GOARCH/GOAMD64/etc. in raw protocol
  requests is outside this driver's current contract, so that alone is not
  reported as a build regression.
- No token, AST, type or successfully emitted binary-content discrepancy found.
  Confirmed failures are at process transport/status handling. No implementation
  changes or existing test edits were made.
