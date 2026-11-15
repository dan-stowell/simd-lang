Reading additional input from stdin...
OpenAI Codex v0.166.0
--------
workdir: /home/exedev/strand-w
model: gpt-6-astra
provider: exe-llm
approval: never
sandbox: read-only
reasoning effort: none
reasoning summaries: none
session id: 01a1e041-e70e-78f3-ab2e-49b222810ed6
--------
user
You are reviewing the Strand repository (current directory), working toward the goal in CHARTER.md:
cold builds of real projects an order of magnitude or more faster on the critical path. Read CHARTER.md, PLAN.md, STATUS.md,
TRACE.md and the tail of JOURNAL.md. Do not edit code or tests.

Answer briefly and concretely, citing files and numbers:
1. What serializes the current critical path that the agent is not attacking?
2. Which phase's "floor" in TRACE.md is wrong or optimistic, and what is the real limit?
3. What is the strongest reason the current direction is a dead end, and what would you build instead?
4. Which intermediate output is still materialized (written and read back) that a fused
   build would never create, and what does it cost in TRACE.md?
5. Quote any recent JOURNAL.md passage that declines or shrinks work because of time, usage or a
   session ending. Did PLAN.md's oracle number move since the last milestone? What is the largest
   blocker in the campaign that is not being attacked?
Skip praise and skip auditing benchmark claims.
codex
I’ll read the campaign documents and recent journal entries, then identify the critical-path constraints and stalled work with file and number citations.
exec
/bin/bash -lc "pwd && rg --files -g 'CHARTER.md' -g 'PLAN.md' -g 'STATUS.md' -g 'TRACE.md' -g 'JOURNAL.md' -g 'AGENTS.md'" in /home/exedev/strand-w
 succeeded in 0ms:
/home/exedev/strand-w
STATUS.md
TRACE.md
CHARTER.md
JOURNAL.md
PLAN.md

exec
/bin/bash -lc 'wc -l CHARTER.md PLAN.md STATUS.md TRACE.md JOURNAL.md' in /home/exedev/strand-w
 succeeded in 0ms:
   269 CHARTER.md
   155 PLAN.md
    11 STATUS.md
   368 TRACE.md
  1652 JOURNAL.md
  2455 total

exec
/bin/bash -lc 'nl -ba CHARTER.md; nl -ba PLAN.md; nl -ba STATUS.md' in /home/exedev/strand-w
 succeeded in 0ms:
     1	# CHARTER
     2	
     3	Only the user edits this file. Agents read it at the start of every session. The current
     4	campaign is in `PLAN.md`; the procedure is in `LOOP.md`.
     5	
     6	## North star
     7	
     8	A cold build of a real project whose stock build has a minutes-long critical path
     9	produces a working binary that passes the project's own tests, in **2 seconds of wall
    10	clock**, by saturating every core and squeezing every instruction and every byte moved
    11	(network, disk, DRAM, L3, L2, L1).
    12	
    13	What the user is after: **one to two orders of magnitude off the critical path of cold
    14	builds**, shown across a corpus of real Go projects (plain `go build` and Bazel/rules_go) with
    15	critical paths of different sizes, and with strong evidence that it scales with cores up to a
    16	point of diminishing returns. 2 seconds on this 8-vCPU VM is the concrete target. Prove what is
    17	possible first; make it general second.
    18	
    19	**The demo** (the user's, for the BuildBuddy team; every row a cold build, wall clock, critical
    20	path and core-seconds): stock Bazel locally on 8 cores; stock Bazel on BuildBuddy RBE with no
    21	cache hits (`--noremote_accept_cached`; the user runs this row); ours on 8 cores. Fewer
    22	resources is half the point.
    23	
    24	### The approach: fuse the build DAG
    25	
    26	A build is a DAG of actions (Bazel's, or `go build`'s action graph). Today every action
    27	is its own process: it gathers its inputs, runs, and materializes its outputs (export
    28	data, objects, archives) for the next action to read back. **Compile the DAG instead:**
    29	treat the whole build as one program, executed in one address space across all cores
    30	(later: a few machines, cut at edges where little data crosses). Intermediate outputs are
    31	not materialized: they're handed over in memory, streamed across edges before their
    32	producer finishes, and deduplicated across the graph. These are HPC techniques (fusion,
    33	streaming, work-stealing, locality) applied to a build.
    34	
    35	**The speed limit is reading the sources and writing the binary.** Everything between those
    36	is overhead to be fused away or work to be parallelized.
    37	
    38	The night of 2026-11-11 measured why this is the direction (JOURNAL iterations 9-14):
    39	parsing and type-checking all of MinIO in one process takes 1.4 s wall and 7.4
    40	core-seconds, against ~30 core-seconds across 1,312 compiler processes (the difference
    41	is export data written and read back, and IR conversions). On top of that come 1,312
    42	process starts, millions of page faults on fresh heaps, ~600 MB of objects written for the
    43	linker to read again, and 8% of code compiled twice by different packages. Early export
    44	(streaming one edge) took the chain from 38.5 s to ~12 s.
    45	
    46	### SIMD and Strand
    47	
    48	SIMD and data parallelism are means, not the goal. Strand is the tool for phases that are
    49	data-parallel-shaped (scans over source bytes, layout by prefix sums, relocation blends,
    50	histograms, joins over dense IDs). The trace decides where those are; don't force a phase
    51	into Strand that the trace says is elsewhere (lexing+parsing is ~1% of compile CPU). When a
    52	fused design creates a new data-parallel-shaped phase (e.g. one pass over all sources, a
    53	whole-program symbol table, a layout pass instead of a linker), Strand is the default for it,
    54	and leaving it needs an entry in `SCALAR_LEDGER.md`.
    55	
    56	## The one number
    57	
    58	Every report leads with the **build trace** (`TRACE.md`, regenerated by one script): wall
    59	clock per phase of the end-to-end build, the critical path, and core utilization.
    60	
    61	    phase            ms    cores busy   bytes moved   floor ms   % of floor
    62	    fetch (separate)
    63	    read inputs
    64	    lex/parse
    65	    resolve/types
    66	    lower/codegen
    67	    link
    68	    write output
    69	    TOTAL / crit path
    70	
    71	A change matters if it moves this table, `SCOREBOARD.md`, or the current campaign's oracle
    72	number (`PLAN.md`). Microbenchmarks
    73	are diagnostics and are never reported as results.
    74	
    75	Two more things the trace must show:
    76	
    77	- **A fair stock row.** Next to plain stock `go build`, a cold stock `go build` with the same
    78	  relaxations we use: `GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l'
    79	  -ldflags='-s -w'`. Our gain is measured against *that* row, so "new technique" is kept
    80	  apart from "turned things off".
    81	- **The materialization tax.** Per build: bytes of intermediates written and read back (export
    82	  data, objects, archives), process starts, page faults, core-seconds outside the compiler's
    83	  own phases. This is what fusion removes; it should fall rung by rung.
    84	
    85	## Physics, not baselines
    86	
    87	For each phase, compute the floor from measured machine limits (`HARDWARE.md`): bytes moved
    88	divided by the bandwidth of each level, and instructions per byte divided by (cores × IPC ×
    89	clock). **% of floor** is the score. Stock toolchains are a reference point for the
    90	end-to-end number only. Beating gcc/go on a kernel is not a goal.
    91	
    92	State the scale honestly: this VM has 8 vCPUs, so 2 s is 16 core-seconds. Rung 0 computes
    93	whether the target's irreducible work fits in that; if it doesn't, say so, and report
    94	"2 s on N cores" with N derived from the floor.
    95	
    96	## The critical path is the enemy
    97	
    98	Minutes-long builds come from dependency chains (package/crate graph, header re-parsing,
    99	global type inference, generic instantiation, linking), not from slow inner loops. For
   100	every phase, answer in writing: **what serializes this, and can the problem be reshaped so
   101	it doesn't?** Don't assume; reshape before giving up. Duplicated work counts as a
   102	critical-path bug.
   103	
   104	## May relax
   105	
   106	Optimization level, debug info, incremental builds, separate compilation, object-file
   107	formats, the linker as a separate step, diagnostic quality beyond "error + location", any
   108	internal format. Whole-program is fine. Changing Strand is fine.
   109	
   110	### This is a dev build (decided by the user, 2026-11-12)
   111	
   112	Accepted for the 2-second build: unoptimized code, a binary up to ~2x the stock size, no
   113	DWARF, no inlining, and the baseline code generator's longer object liveness (an object
   114	can stay alive until its function returns, so a test waiting for a finalizer may hang;
   115	skip those tests and list them). Huge pages via `sudo sysctl vm.nr_hugepages` are allowed
   116	as machine configuration (put the old value back after). Report every such difference in
   117	`MORNING.md`; never hide one.
   118	
   119	## May not relax
   120	
   121	- **Cold:** no caches survive between timed runs (drop the page cache too, if a phase reads from disk).
   122	- **Output:** a binary that passes the project's tests.
   123	- **Correctness:** each replaced phase is differential-tested against the stock toolchain
   124	  (same tokens, same AST, same types, same binary behaviour) and fuzzed at chunk
   125	  boundaries. Bootstrap fixed point and the existing suite stay green.
   126	
   127	## Ladder (walking skeleton first)
   128	
   129	### Targets
   130	
   131	- **Target: MinIO** (`github.com/minio/minio`, the `minio` server binary,
   132	  `CGO_ENABLED=0`). Pure Go, one `go build`, a huge dependency tree (cloud SDKs, generated
   133	  code), hand-written Go assembly (`sha256-simd`, `reedsolomon`), a big serial link.
   134	  Tests: start the server and do S3 put/get/list/delete against it, plus the unit tests
   135	  of the packages we touch.
   136	- **Comparison point: `kube-apiserver`** (Kubernetes, `CGO_ENABLED=0`). Profiled at rung 0
   137	  next to MinIO; once the pipeline works on MinIO, run it here too, so we don't overfit to
   138	  one codebase. Smoke test: it starts and answers `/healthz`.
   139	- **Fast inner loop: the Go toolchain** (`cmd/compile`, `cmd/link`, `cmd/go`, and the
   140	  standard library). It's the quick differential-test corpus. Every phase change is validated
   141	  here in seconds and then confirmed on MinIO. It is not the target: never optimize for
   142	  its build time.
   143	- Full MinIO traces run at campaign milestones. During development, time single packages or
   144	  subtrees and say so.
   145	
   146	### Corpus (the scoreboard)
   147	
   148	`SCOREBOARD.md`, regenerated by one script: every project cold, stock, fair stock and ours, at 1,
   149	2, 4 and 8 cores (CPU-restricted, e.g. `taskset` or a cgroup cpuset), with wall clock, critical
   150	path and core-seconds. Pin every commit. Graph shapes from `go list` on 2026-11-12 (non-std
   151	packages; longest import chain by source bytes):
   152	
   153	| project | why it's here | shape |
   154	|---|---|---|
   155	| buildifier (`bazelbuild/buildtools`) | small, rules_go; the standard library is most of it | 51 pkgs; chain 5.3 MB / 31 pkgs |
   156	| gVisor `runsc` | Bazel-native, pure Go, assembly and Linux-specific code | 169 pkgs; 6.4 MB / 23 |
   157	| caddy (`cmd/caddy`) | the deepest package chain | 794 pkgs; 9.3 MB / 50 |
   158	| tailscaled | mid-size plain go | 552 pkgs; 9.3 MB / 32 |
   159	| MinIO | one fat package on the critical path | 1,048 pkgs; 29.1 MB / 15 |
   160	| kube-apiserver | wide | 1,835 pkgs; 14.3 MB / 31 |
   161	| prometheus | the most source; scaling with cores | 1,283 pkgs; 20.5 MB / 32 |
   162	| BuildBuddy executor and `bb` CLI | the demo; Bazel only (generated protos) | 1,701 pkgs; 11.6 MB / 38 |
   163	
   164	Bazel projects build from their Bazel DAG (`bazel aquery`), generated code included, from the
   165	start: BuildBuddy can't be built with plain `go build`.
   166	
   167	### What "cold" means
   168	
   169	**Fresh cold build every time; nothing else counts** (the user, 2026-11-12). Nothing survives
   170	between timed runs: no GOCACHE, no Bazel disk cache, remote cache or output base, no daemon or
   171	warm process from an earlier run. Anything that warms up (a fork-server, a worker pool, the
   172	Bazel server) starts inside the timer.
   173	
   174	- An empty `GOCACHE` (fresh temp dir per run), so the standard library and runtime are part
   175	  of the build.
   176	- Modules are downloaded beforehand (`go mod download`) into a `GOMODCACHE` that is kept
   177	  between runs. Fetching is timed separately, as its own **fetch** row in the trace.
   178	- The page cache is dropped before timed runs (`sync; echo 3 | sudo tee /proc/sys/vm/drop_caches`),
   179	  so reading inputs from disk is honest. Also report the warm-page-cache number next to it.
   180	- Pin versions: record the MinIO and Kubernetes commits and the Go version in `TRACE.md`.
   181	
   182	### Rungs
   183	
   184	Rungs 0-5 were climbed on 2026-11-11 (JOURNAL iterations 1-14): MinIO cold 54.2 s ->
   185	12.3-13.3 s, kube-apiserver 86.3 s -> 24.1-25.2 s, via our own driver and planner, early
   186	export, a baseline code generator inside `cmd/compile`, huge pages, and splitting `minio/cmd`
   187	across processes. The record of those rungs, kept for reference:
   188	
   189	0. **Profile.** (Done for MinIO and kube-apiserver at the time; the
   190	   1/2/4/8-core matrix is now part of the corpus scoreboard.) Build MinIO and `kube-apiserver` cold with the stock toolchain on 1, 2, 4 and 8
   191	   CPUs. Use `go build -debug-trace=trace.json` (a Chrome trace of the action graph) and
   192	   `-debug-actiongraph` to get per-package compile time, the critical path through the
   193	   package graph, idle cores over time, and the link's share. Add bytes read and the total
   194	   source size (Go, assembly, generated). Compute the floor for each phase. Commit
   195	   `TRACE.md` and the script that regenerates it. This is the gap.
   196	1. **Skeleton.** A driver builds MinIO end to end, with every phase present. Phases we
   197	   haven't built yet delegate to the stock toolchain (e.g. Strand's exact Go lexer runs over
   198	   every file, then `go tool compile`/`asm`/`link` do the rest). The trace exists from day one.
   199	2. **And on.** Replace or reshape the phase with the most wall clock on the critical path, one
   200	   at a time. A rung is done when the MinIO trace shrinks, the toolchain corpus and MinIO
   201	   tests still pass, and the differential tests are green.
   202	
   203	### Fusion rungs (the map; PLAN.md's campaign says which one is being built)
   204	
   205	A rung is finished when the real MinIO build consumes it, passes its tests, and shows the
   206	materialization tax falling in TRACE.md. Getting there may take days, with the trace flat or
   207	worse in between; the campaign's oracle number measures progress until then.
   208	
   209	F0. **The fair stock row and the tax, measured.** Add both to TRACE.md (see "The one
   210	    number"). Attribute the current 80 core-seconds: compiling vs moving data between actions
   211	    vs process and memory setup.
   212	F1. **Persistent workers, intermediates in memory.** Compiler processes stay alive across
   213	    packages, and export data and objects pass through shared memory (memfd/tmpfs mappings)
   214	    instead of being written and re-read. `cmd/compile` keeps global state, so a worker may
   215	    need resetting between packages, or a fork-server (one warm process forked per package)
   216	    may be the cheaper way to get the same effect. Measure both; pick by the trace.
   217	F2. **One address space for the front end.** Whole-program parse and type check in one process
   218	    (measured at 1.4 s wall), handing typed packages to the back end in memory, with no export
   219	    data at all. The second compiler (`driver/sbc`) and the whole-program spike
   220	    (`explore/wholeprog`) are the start of this. Fall back to the stock path per package.
   221	F3. **The DAG as a schedule, not a set of processes.** Function bodies (not packages) are the
   222	    unit of work, on a work-stealing queue across all cores; package boundaries matter only
   223	    for initialization order. Deduplicate shared functions (wrappers, instantiations) across
   224	    the graph.
   225	F4. **No linker as a separate step.** Dense symbol IDs from the compiler; layout by one prefix
   226	    sum over sizes; code emitted straight into the final image; relocation as one data-parallel
   227	    pass. (Strand-shaped: see LINKING.md.)
   228	F5. **From Bazel.** Take the DAG from Bazel (`bazel aquery`, rules_go first) instead of from our
   229	    own planner, so the fused executor isn't Go-specific at the graph level. (Reading the DAG
   230	    is pulled forward into the corpus scoreboard; this rung is the executor consuming it.) Then think about
   231	    cutting the DAG across a few machines at narrow edges (package interfaces are small next
   232	    to objects).
   233	
   234	Later (record in `IDEAS.md`, don't start yet): a second language through the same fused
   235	executor (Rust or C++ is the boss level); the network/disk end of the pipeline.
   236	
   237	## How to work
   238	
   239	- **Never stop to ask.** Pick the most ambitious step you can justify, write why in
   240	  `JOURNAL.md`, and do it. Decisions that are the user's go in STATUS.md; keep working.
   241	- **Horizon.** This work runs for days. A session ending (usage limit, context, restart) is not
   242	  an event; the next one continues from `PLAN.md`. Time left, usage left and "the next session"
   243	  are never reasons to decline or shrink a step. The only valid reason is a measurement that
   244	  shows it cannot pay.
   245	- **Calibrate on your own record, not human estimates.** The baseline code generator reached
   246	  97% of MinIO's functions in under an hour; the link spike came within 46 of the stock linker's
   247	  122,860 functions in 33 minutes. A front end or a linker is days of work for you, not months.
   248	- **One campaign at a time**, named in `PLAN.md` by the user. You decide how, not whether. It
   249	  ends when its exit test passes, or when its kill test, written before work began, fails by
   250	  measurement. The trace may stay flat or get worse during a campaign; progress is then the
   251	  campaign's oracle number.
   252	- **Negative results are results.** Write them up and move on.
   253	- **Commit and push often** with real messages. The history is part of the deliverable.
   254	- Report **absolute times** (ms, ns, GB/s), never ratio tables. Take timestamps from `date -u`,
   255	  not from memory.
   256	- **Don't ration usage.** The harness resumes after a limit; committing often means an abrupt end
   257	  costs nothing. Prefer Codex and sol for reviews (separate budgets), cheaper models for
   258	  subagents that only search or summarize, and Fable for the hard parts.
   259	- **The host is noisy.** Compare configurations interleaved in the same window
   260	  (`tools/trace/ab.sh`), not against numbers from hours earlier.
   261	
   262	## Anti-goals
   263	
   264	- Meta work: "discovering" speedups, frameworks for finding speedups, benchmark harness polish.
   265	- Wins that don't move the trace (e.g. counting declarations faster).
   266	- Defending claims. Make fewer claims; show the trace.
   267	- Waiting on reviews.
   268	- Declining or shrinking work because of time left, usage, or a session ending.
   269	- Polishing a frozen path: once a campaign freezes a baseline, it stays frozen.
     1	# PLAN
     2	
     3	The campaign is chosen by the user. Sessions **do not edit** the Campaign, Exit test, Kill
     4	test or Rules sections; they append to Milestones and keep the Now line current. A session
     5	cannot switch campaigns. If you think the campaign is wrong, write the evidence in STATUS.md
     6	under "Decisions for Dan" and keep working on it.
     7	
     8	## Campaign 1 (from 2026-11-12): our own compiler for whole packages, fused
     9	
    10	The stock-compiler path is **frozen** at its 2026-11-12 result: MinIO cold 11.2 s median,
    11	kube-apiserver 19.8-21.3 s (configuration `13-noweakptr`; MORNING.md, TRACE.md). It is the
    12	baseline to beat and the per-package fallback. No more work goes into making it faster.
    13	
    14	Why this campaign: the 2026-11-12 CPU table says fusing the stock compiler's work removes at
    15	most about 27 of 74 core-seconds; what is left (~47 core-s, 5.9 s on 8 cores) is the stock
    16	compiler's own work. Two seconds needs the compiling itself to be about three times cheaper:
    17	whole packages compiled without the stock front end, with no export-IR round trip, no
    18	`obj.Prog` assembler path and no separate linker. `05-sbc` showed that taking single
    19	functions does not pay; whole packages leaving the stock compiler is the seam that does.
    20	
    21	How is yours to decide: whole-program type checking once in one address space (go/types or
    22	your own), a lowering of your own, direct emission (the baseline generator's knowledge of
    23	frames, stack maps, write barriers and the calling convention is reusable), and the F4 link
    24	spike in `explore/f4link` as the place code lands. Stock fallback per package for anything
    25	not yet handled.
    26	
    27	**The trace may stay flat or get worse for days during this campaign.** That is expected and
    28	not a reason to retreat. Progress is the oracle number below.
    29	
    30	## Oracle number (moves hourly; report it at every milestone)
    31	
    32	1. **Packages compiled entirely by our compiler, whose tests pass**: standard-library packages
    33	   first (their own `go test` with the binary built our way, plus the differential suite
    34	   against stock), then corpus packages (MinIO first). Report `N of M` per corpus project.
    35	2. **Core-seconds per MB of source** on the packages we compile, next to the stock baseline
    36	   mode's number on the same packages.
    37	
    38	## Kill test (written before work began)
    39	
    40	When the first 50 standard-library packages compile our way and pass, measure (2). If our
    41	compiler costs more than **one third** of the stock baseline mode's core-seconds on the same
    42	packages, with no measured path to one third, stop the campaign: write up why in JOURNAL.md
    43	and STATUS.md, and ask for the next campaign under "Decisions for Dan". Only this measurement
    44	ends the campaign early; time, usage and session ends never do.
    45	
    46	## Exit test
    47	
    48	A fresh cold MinIO build (CHARTER.md's cold rule) in **3.3 s wall or less on 8 cores** (an
    49	order of magnitude under the fair stock row's 32.5 s), binary passing MinIO's tests, with
    50	the corpus scoreboard showing the same technique on every corpus project.
    51	
    52	## Rules
    53	
    54	- Commit and push every working step, with tests for what it touches, so an abrupt end loses
    55	  nothing.
    56	- Use subagents as builders: split work into oracle-checked parts, one git worktree each.
    57	- The 8-vCPU VM is the machine (decided by the user). Scaling is reported at 1, 2, 4 and 8 cores.
    58	
    59	## Milestones
    60	
    61	M0. **Corpus scoreboard** (bounded: one day at most; then back to the compiler even if parts are
    62	    missing). Pin and fetch the corpus in CHARTER.md; one script builds each cold, stock and fair
    63	    stock, at 1, 2, 4 and 8 cores, and records wall clock, critical path and core-seconds in
    64	    `SCOREBOARD.md`. Bazel projects build from their Bazel DAG (`bazel aquery`), generated code
    65	    included. Our column starts as "stock fallback" and fills in as the campaign lands.
    66	M1. Our compiler compiles a first standard-library package end to end, linked into a binary that
    67	    passes that package's tests.
    68	M2. 50 standard-library packages: run the kill test.
    69	M3. The whole standard library and runtime our way.
    70	M4. MinIO entirely our way, passing its tests.
    71	M5. No separate linker: code emitted into the image (F4).
    72	M6. The exit test; the corpus scoreboard filled in.
    73	
    74	Log (append one line per milestone reached, with the oracle number):
    75	
    76	- M0, 2026-11-12: SCOREBOARD.md: every corpus project cold, stock and fair stock (and Bazel from its own DAG),
    77	  at 1/2/4/8 cores (`tools/scoreboard/run.sh`, `mkscoreboard.py`). One run per cell; fc builds overlapped on
    78	  cores 4-7 during parts of the 8-core pass (rerun `tools/scoreboard/run.sh . 8` on a quiet machine before quoting).
    79	- M1, 2026-11-12 19:57 UTC: `unicode/utf8` compiled entirely by fc (driver/fc) passes its own tests in a binary
    80	  whose other packages are stock-compiled (`tools/fc/test.sh unicode/utf8`; one allocation-count test skipped,
    81	  tests/fc/skips.txt). Oracle: standard library 1 package; corpus 0. Core-seconds per MB: not measured yet (M2).
    82	- M2, 2026-11-12 23:23 UTC: **205 standard-library packages** compiled entirely by fc pass their own tests in mixed mode
    83	  (tests/fc/passing.txt; 53 compile but fail, with Codex triaging; 91 have no tests), and all 21 packages of the
    84	  language corpus (tests/fc/lang). Corpus: MinIO builds and links in whole-program mode (1,283 packages by fc, the
    85	  runtime's 30 from stock) but does not run yet. **Kill test measured** (`tools/fc/cost.sh`, the 205 packages, 8.23 MB):
    86	  stock baseline mode 6.18 core-s (0.75 per MB); fc 2.80 core-s attributable (0.34 per MB), ratio **0.45**: above one
    87	  third. Not stopped, because the path is measured (JOURNAL): the profile is flat with named items, and on MinIO the
    88	  whole fc build is 26-27 core-s against the frozen path's 74.
    89	- M4 in part, 2026-11-13 03:43 UTC: **MinIO built by fc passes its smoke test, cold**: 1,283 packages by fc in one process,
    90	  the runtime's 29-30 packages by the stock compiler inside the timer, planning by the driver's own planner,
    91	  stock link: 7.7-8.7 s wall, 34-36 core-s on a machine two other agents were using (`tools/fc/cold.sh minio`;
    92	  frozen stock path 11.2 s, fair stock 29.5 s / 150 core-s). kube-apiserver the same way: 8.7-9.1 s, 42-43 core-s,
    93	  smoke passes (frozen path 19.8-21.3 s). buildifier, runsc, caddy and tailscaled build, link and run `--version`
    94	  (`tools/fc/corpus.sh`); prometheus stops in one generic instance. Not M4 yet: MinIO's unit tests have not been
    95	  run against fc-built packages, and the runtime is stock (M3).
    96	- M4 in part, 2026-11-13 05:00 UTC: **MinIO's own tests under fc: 45 of 46 packages pass; the 46th (cmd) fails one
    97	  test that fails with the stock compiler too** (package under test by fc; the all-packages-by-fc run,
    98	  `FC_ALL=1 tools/fc/test.sh`, is in /home/exedev/targets/fc-minio-all.log). std: 228 packages pass. Escape analysis
    99	  (driver/fc/escape*.go) and fclink (driver/fclink) are merged into main, not yet used by `fc build`.
   100	- M3 in part, 2026-11-13 10:40 UTC: **a program compiled entirely by fc, the runtime included, and linked by
   101	  fclink runs**: hello and three of tools/fclink/progs match stock output; MinIO built that way (1,313 fc objects,
   102	  no stock archive) passes its smoke test (`FC_RUNTIME=1`, off by default). Not M3: with an fc-built runtime the
   103	  collector loses live objects when it marks concurrently on more than two CPUs (deterministic reproduction:
   104	  reflect calls under allocation pressure); `FC_ALL=rt tools/fc/lang.sh` (test binaries wholly by fc) is 17-19 of 23.
   105	  SCOREBOARD.md's fc column is filled (cold, idle machine, fc + fclink, stock-compiled runtime): MinIO 6.8 s on 8
   106	  cores, before the two-pass front end (warm 5.5 -> 4.8 s).
   107	- M3 nearly, 2026-11-13 13:30 UTC: **std with test binaries built wholly by fc, runtime included, linked by fclink
   108	  (`FC_ALL=rt tools/fc/test.sh`): 221 of 226 packages pass their own tests**; the language corpus 23 of 23 in that
   109	  mode; MinIO and kube-apiserver built that way pass their smoke tests. The lost-object bug had two causes (JOURNAL
   110	  12:10): stack checks in helpers the stock compiler inlines, and BP after the runtime's context switches.
   111	- **M4, 2026-11-13 23:45 UTC: MinIO entirely our way passes its tests.** Every Go package by fc, the runtime included,
   112	  linked by fclink (`fc build`'s defaults now; no go command in the build; assembly by the stock assembler): MinIO's own
   113	  tests with wholly-fc test binaries: 45 of 46 packages pass and cmd fails one test that stock fails too
   114	  (fc-minio-rt2.log, fc-minio-cmd-flake.log); smoke test passes (25 of 25 starts). Cold, 8 cores, machine shared with a
   115	  builder (load 4-5): **3.6-4.1 s wall, 20-23 core-s** (this morning 6.8 s; frozen stock path 11.2 s; exit test 3.3 s).
   116	  What moved it: huge pages for fc's heap (-8 core-s), biggest-first slots, declarations checked once with bodies in
   117	  shards (go/types CheckDecls/Bodies), a function's type information in its own maps. The build is CPU-bound now
   118	  (6-7 of 8 cores busy from 0.2 s to the link).
   119	- M3 nearly, 2026-11-13 23:45 UTC: **std wholly by fc: 258 of the 262 packages with tests pass** (tests/fc/passing.txt;
   120	  skips with evidence in tests/fc/skips.txt). Left: runtime (9 tests) and runtime/pprof with a builder; runtime/cgo and
   121	  internal/runtime/cgobench need cgo (not buildable with stock either at CGO_ENABLED=0). Bugs found by the last 36:
   122	  one-case select through selectgo (tickers deadlocked), fclink's function table order with mixed alignment,
   123	  go:fipsinfo, two data races in fc.
   124	- **M3, 2026-11-14 00:08 UTC: the standard library and the runtime our way.** With every package of the test binary
   125	  compiled by fc, the runtime included, and linked by fclink (`FC_ALL=rt tools/fc/test.sh`): **260 of the 262 std
   126	  packages that have tests pass their own tests** (tests/fc/passing.txt), the runtime's own suite among them (284 s).
   127	  The two left need cgo (runtime/cgo, internal/runtime/cgobench: not buildable by stock either at CGO_ENABLED=0).
   128	  54 skip lines (tests/fc/skips.txt): allocation counts and inlining assertions (most fail with stock at -l too),
   129	  liveness waits, one environment skip. Language corpus 23 of 23. MinIO's own tests rerun with this fc: 45 of 46,
   130	  cmd as stock (fc-minio-rt3.log). Oracle 2: MinIO 20-23 core-s for 75.5 MB (0.27-0.30 per MB).
   131	
   132	Now (2026-11-15 17:30 UTC): **MinIO cold 2.4-2.7 s / 13.7-15.5 core-s on 8 cores, idle machine, smoke passing**
   133	(exit test: 3.3 s); corpus cold at 8 cores: buildifier 0.5 s, runsc 0.6, tailscaled 1.25, caddy 1.6, kube-apiserver 2.8,
   134	prometheus 4.9. What moved it since the last scoreboard (3.9 s): checker results in the syntax nodes, the descriptor
   135	cuts, the in-process link (M5's first step), and the bump allocator (JOURNAL 11-15 17:17). Left for the exit test:
   136	(1) **MinIO's own tests on this fc**: running through the whole-program path
   137	(`FC_TIMEOUT=2400 tools/fc/wpbatch.sh -j 2 -C /home/exedev/targets/minio <packages with tests>` ->
   138	/home/exedev/targets/fc-minio-wp1.log; 45 internal packages pass, cmd was still running at 17:26). When done: log it,
   139	then std the same way (`tools/fc/wpbatch.sh` over tests/fc/passing.txt) since the bump allocator, slots and the
   140	in-process link have not been through the std suite.
   141	(2) **The Bazel rows**: buildifier and runsc build from Bazel's DAG (tools/bazel/cold.sh; 9.6 s and 26 s cold, of which
   142	fc is 0.4-0.6 s and Bazel's own analysis 6 and 22 s). A builder (Opus, worktree, FC_TAG=bb) has bb-cli and bb-executor;
   143	merge when `tools/bazel/cold.sh <row> 8` gives rc 0 and the binary's check passes. Then the question the Bazel rows
   144	raise: the cold cost is Bazel's analysis, so read the DAG cheaper (what does aquery need that a cold server makes slow?).
   145	(3) **On a quiet machine** (nothing else running; check `uptime`): regenerate SCOREBOARD.md
   146	(`tools/scoreboard/ours.sh . 8 4 2 1`, then mkscoreboard.py) and TRACE.md; then verdicts owed, order-balanced:
   147	FC_FUSE=1 (fused check) and FC_LAZYFUNCS=1 (by demand) against the default (scratch: 6 pairs, user+sys and wall, MinIO
   148	warm). Under load neither showed a gain; by demand compiles 13% fewer functions and is not cheaper, cause unknown
   149	(profile both on a quiet machine before any more work on it).
   150	(4) Then toward 2.0 s: the profile is flat (code generation 40%, bodies check 15%, descriptors 12%, declarations 9%,
   151	parse 9%, link 6%); candidates with a mechanism: per-function scratch reuse (growslice+memmove 1.3 s of 14),
   152	demand()'s sync.Map load per reference (0.5 s), ast.Walk passes per function (0.9 s: findHeapVars and friends in one
   153	walk), NewUnit (0.6 s). tools/fc/timeline.py for where cores idle (the link is 280 ms).
   154	Housekeeping: `df -h /` before long test runs (13 GB free at 17:20); nr_hugepages was found at 4090 (a killed run) and
   155	set back to 0.
     1	# STATUS (at most ten lines; updated at every milestone and whenever Dan is needed)
     2	
     3	Campaign 1, 2026-11-15 17:30 UTC. **MinIO built entirely by fc and linked in process: cold 2.4-2.7 s on 8 cores, 13.7-15.5 core-s, idle machine, smoke test passing** (exit test 3.3 s; last scoreboard 3.9 s; frozen stock path 11.2 s; fair stock 29.5 s). Corpus cold on 8 cores: buildifier 0.5 s, runsc 0.6, tailscaled 1.25, caddy 1.6, kube-apiserver 2.8, prometheus 4.9; every binary passes its check.
     4	What moved it: type-checker results stored in the syntax nodes, dead descriptors skipped, objects handed to the linker in memory, and a bump allocator in fc's runtime (no collector runs, so nothing is freed).
     5	Not yet the exit test: MinIO's own tests are rerunning on this fc (45 of 46 packages pass so far, cmd running); SCOREBOARD.md is not regenerated; of the Bazel rows buildifier and runsc build from Bazel's DAG, BuildBuddy's two are with a builder.
     6	From Bazel's DAG the cold build is Bazel itself: buildifier 9.6 s (Bazel's start and analysis 6.1 s, fc 0.4 s), runsc 26 s (Bazel 22 s, fc 0.6 s).
     7	
     8	## Decisions for Dan
     9	
    10	1. The Bazel rows: cold, Bazel's own server start, repository extraction and analysis cost 6-22 s before fc sees the graph. Is reading the DAG without a cold Bazel server (e.g. an aquery result treated as a fetched input, like GOMODCACHE) within "cold" for the demo, or must Bazel start inside the timer as CHARTER says now? I keep it inside the timer until told otherwise.
    11	2. Huge pages (10 GB pool during the build), the 54 skipped tests, and the race detector's stack guard in an fc-built runtime: as in the previous STATUS; unchanged.

exec
/bin/bash -lc 'nl -ba TRACE.md' in /home/exedev/strand-w
 succeeded in 0ms:
     1	# TRACE
     2	
     3	Generated by `tools/trace/mktrace.py` on 2026-11-12 15:49 UTC from the instrumented builds made by `tools/trace/stock_all.sh`. Do not edit by hand.
     4	
     5	Machine: 8 vCPUs (AMD EPYC 9554P under KVM), 31 GiB RAM; 2 s of wall clock is 16 core-seconds.
     6	
     7	Measured limits (`hw/limits.c`, `hw/limits.out.txt`): memcpy 1t 9.88 GB/s, memcpy 8t 43.09 GB/s, disk write 0.90 GB/s, disk read cold 3.11 GB/s, pagecache read 1t 4.99 GB/s, smallfiles cold 8t 0.27 GB/s, smallfiles warm 8t 2.11 GB/s.
     8	
     9	## minio: the build trace (driver `sb`, 8 CPUs, cold)
    10	
    11	Run `13-noweakptr` (driver at `5e509a4`, flags `GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout`). Result: PASS minio smoke (put/get/list/delete, 3 MB object round trip). Stock `go build` on the same machine, cold: 54,233 ms.
    12	
    13	Wall clock of 4 cold runs of this configuration, ms: 11,009, 11,344, 10,916, 10,794. The table is the first of them.
    14	
    15	| phase | ms | cores busy | core-s | bytes moved | floor ms | ms / floor | who does it |
    16	|---|---|---|---|---|---|---|---|
    17	| fetch (separate, not in total) | 6,675 | | | 1,120 MB | | | `go mod download` |
    18	| plan (package graph) + read inputs | 1,092 | 3.9 | 0.0 | 76 MB | 374 | 3x | driver; go.mod, build constraints |
    19	| &nbsp;&nbsp;of which lex + package-level declarations | 372 | 3.1 | 1.1 | 76 MB | 6 | 61x | Strand godecl |
    20	| head start | 1,409 | | | 0 kB | | | runtime closure compiled during the plan |
    21	| parse + resolve/types + lower/codegen | 8,470 | 7.6 | 62.9 | 526 MB | 268 | 32x | go tool compile/asm |
    22	| link | 1,071 | 1.9 | 1.7 | 159 MB | 16 | 67x | go tool link |
    23	| write output | 111 | 4.1 | 0.0 | 159 MB | 176 | 1x | driver |
    24	| **TOTAL** | **11,009** | **6.25** | **69** | read 223 MB, wrote 160 MB (disk) | **841** | **13x** | |
    25	| critical path (13 actions) | 3,429 | | | | | | |
    26	
    27	**Inside the compile row, by profile** (`tools/trace/phases.sh`: a CPU profile of every compiler process of run `08-huge-shard`, merged; 42 s of samples. Read the shares, not the seconds: the sampler misses about a third of the row's CPU. The exact per-phase table is further down):
    28	
    29	| compiler phase | core-s | share | the charter's row |
    30	|---|---|---|---|
    31	| lex + parse (syntax.Parse) | 0.3 | 0.8% | lex/parse |
    32	| type check (types2) | 4.8 | 11.4% | resolve/types |
    33	| typed tree -> export IR (write) | 3.0 | 7.2% | resolve/types |
    34	| export IR -> compiler IR (read), imports | 6.0 | 14.3% | resolve/types |
    35	| escape analysis, wrappers, runtime types, init | 5.6 | 13.3% | lower/codegen |
    36	| walk + build SSA | 4.5 | 10.8% | lower/codegen |
    37	| SSA passes (regalloc 0.5) | 2.2 | 5.2% | lower/codegen |
    38	| emit machine code (genssa, assembler) | 8.0 | 19.2% | lower/codegen |
    39	| write objects and export data | 3.6 | 8.5% | write objects |
    40	| not under any of these (scheduler, runtime start, flag parsing) | 3.9 | 9.2% | |
    41	
    42	Across all phases, 5.0 s is the allocator and 1.4 s is clearing memory. Lexing and parsing all of the build's Go is 0.34 core-s: 0.8% of the compiler's work.
    43	
    44	**The fair stock row.** Stock `go build`, cold, with the relaxations this build uses (`GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l' -ldflags='-s -w'`): **32,512 ms** (169 core-s). Plain stock in the same hour: 49,953 ms. This build: 11,009 ms. The gain to claim is against the fair row.
    45	
    46	**The materialization tax** (what the build pays for being separate processes that hand each other files; `tools/trace/tax.py`, driver counters):
    47	
    48	| | stock | fair stock | this build |
    49	|---|---|---|---|
    50	| wall ms, cold | 49,953 | 32,512 | 11,009 |
    51	| core-s | 257 | 169 | 69 |
    52	| tasks created, whole machine (processes + threads) | 11,404 | 10,585 | 6,380 |
    53	| tool processes started (exec) | 1,483 | 1,483 | 172 + 1,315 forked from one warm compiler |
    54	| page faults, whole machine | 11,923,241 | 11,007,650 | 1,343,950 |
    55	| intermediates written (archives, export data) | 1,899 MB | 1,383 MB | 618 MB |
    56	| intermediates read back (importers, linker, cache copy) | 2,731 MB | 1,996 MB | 1,143 MB |
    57	| of which export data | 107 MB | 91 MB |  |
    58	
    59	Stock writes every archive twice (work directory, then GOCACHE) to disk; this build writes each once to tmpfs. "Read back" counts the export data each importer maps and every archive the linker reads.
    60	
    61	**Where the compile row's CPU goes, exactly** (`tools/trace/cpuphases.sh`: every compiler process logs its own CPU time and page faults at each phase boundary; warm run, 1,315 compiler processes, 68.1 core-s in the row):
    62	
    63	| compiler phase | user s | sys s | page faults | what it is |
    64	|---|---|---|---|---|
    65	| be:compilefuncs | 24.3 | 1.7 | 111,536 | walk, baseline code generation (SSA for the rest), assembler: **compiling** |
    66	| fe:irread | 9.1 | 1.2 | 74,184 | build the compiler's IR by reading that back, plus imported bodies and types: **moving data** |
    67	| fe:typecheck | 7.7 | 1.6 | 132,859 | types2, including reading imported packages' export data on demand: compiling (+ moving data) |
    68	| fe:irwrite | 4.6 | 0.7 | 57,626 | serialize the checked package as export IR: **moving data** |
    69	| be:dumpobj | 2.8 | 0.6 | 6,788 | write the object file: **moving data** |
    70	| fe:earlyexport | 2.4 | 0.4 | 29,548 | write export data for importers: **moving data** |
    71	| fe:devirtualize-and-inline | 2.2 | 0.2 | 11,067 | inlining is off; what is left under this timer is method wrappers and small per-function passes: compiling |
    72	| fe:escapes | 2.1 | 0.2 | 7,394 | escape analysis: compiling |
    73	| fe:parse | 1.4 | 0.6 | 71,704 | read and parse the source files: compiling |
    74	| (before main) | 0.9 | 0.2 | 126,091 | exec, runtime start, package init: **process setup** |
    75	| fe:init | 0.8 | 0.3 | 134,437 | flags, universe: process setup |
    76	| fe:loadsys | 0.5 | 0.2 | 26,153 | declare the runtime's functions: process setup |
    77	| (exit, asm, pack) | 1.6 | 0.0 | 0 | process exit, the assembler, joining shard objects |
    78	
    79	Notes: plan + read: 1312 packages, 7531 Go files, 10 rounds; declarations: package clauses and import paths of every file, consumed by the plan; head start: 29 of its 29 packages adopted by the plan; their CPU is in the compile row; compile: bytes = object archives written to tmpfs; write output: the linker wrote the file in place; this row is the sync.
    80	
    81	```
    82	8 |▁▃▅▄▄▆▇█████████████████████████████████████▇█▇▅▄▄▇█▃▁▁▁▁▂▄▁|   cores busy; one column = 183 ms
    83	0 +------------------------------------------------------------+
    84	```
    85	
    86	The chain that ends the build (each action was the last thing the next one waited for):
    87	
    88	| package | action | start ms | ms until it released the next | waited for a CPU token, ms | Go source |
    89	|---|---|---|---|---|---|
    90	| `github.com/minio/minio/cmd` | compile | 7,842 | 1,716 | 0 | 5 MB |
    91	| `github.com/minio/minio` | link | 9,567 | 1,071 | 9,567 | 0 kB |
    92	| `github.com/prometheus/procfs` | compile | 2,003 | 167 | 12 | 297 kB |
    93	| `syscall` | compile+asm | 1,161 | 102 | 3 | 244 kB |
    94	| `os` | compile | 1,399 | 80 | 0 | 179 kB |
    95	| `internal/poll` | compile | 1,319 | 80 | 0 | 66 kB |
    96	| `fmt` | compile | 1,480 | 67 | 1 | 99 kB |
    97	| `time` | compile | 1,263 | 55 | 0 | 168 kB |
    98	
    99	### minio: every rung so far (8 CPUs, cold)
   100	
   101	Single cold runs. The host is shared: the same build has cost between 139 and 157 compile core-seconds at different times of the night, so differences under about 2 s between rows are not evidence; compare the phase columns.
   102	
   103	| run | wall ms | core-s | cores busy | plan | read | lex or declarations | compile | link | write | smoke test |
   104	|---|---|---|---|---|---|---|---|---|---|---|
   105	| stock `go build` | 54,233 | 282 | 5.21 | | | | | 3,251 | | |
   106	| `01-skeleton`  | 47,553 | 265 | 5.58 | 428 | 216 | 213 | 43,083 | 3,077 | 272 | pass |
   107	| `02-pipeline` (-compiler build/compile-sb -pipeline -backends 2000) | 39,885 | 273 | 6.84 | 623 | 299 | 207 | 35,221 | 2,925 | 283 | pass |
   108	| `03-relaxed` (GOGC=off -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 21,957 | 144 | 6.58 | 509 | 202 | 225 | 19,053 | 1,532 | 192 | pass |
   109	| `04-selfplan` (GOGC=off -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 22,111 | 147 | 6.65 | 663 | (in plan) | 210 | 19,365 | 1,634 | 183 | pass |
   110	| `05-sbc` (GOGC=off -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -sbc build/sbc -gcflags=-dwarf=false -l -ldflags=-s -w) | 24,016 | 156 | 6.48 | 924 | (in plan) | 245 | 19,976 | 1,618 | 202 | pass |
   111	| `06-baseline` (GOGC=off SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 16,259 | 92 | 5.65 | 814 | (in plan) | 245 | 13,459 | 1,425 | 297 | pass |
   112	| `07-baseline-float` (GOGC=off SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 16,403 | 91 | 5.58 | 743 | (in plan) | 276 | 13,684 | 1,404 | 302 | pass |
   113	| `08-huge-shard` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 13,262 | 80 | 6.06 | 932 | (in plan) | 256 | 10,438 | 1,313 | 282 | pass |
   114	| `09-forkserver` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote) | 15,616 | 97 | 6.18 | 686 | (in plan) | 233 | 12,911 | 1,397 | 277 | pass |
   115	| `10-onepass` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote) | 11,699 | 67 | 5.74 | 945 | (in plan) | 263 | 8,938 | 1,194 | 310 | pass |
   116	| `11-headstart` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart) | 13,813 | 86 | 6.22 | 1,013 | (in plan) | 442 | 11,069 | 1,191 | 276 | pass |
   117	| `12-directout` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout) | 11,531 | 71 | 6.16 | 1,048 | (in plan) | 367 | 8,853 | 1,227 | 94 | pass |
   118	| `13-noweakptr` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout) | 11,009 | 69 | 6.25 | 1,092 | (in plan) | 372 | 8,470 | 1,071 | 111 | pass |
   119	
   120	## kube-apiserver: the build trace (driver `sb`, 8 CPUs, cold)
   121	
   122	Run `13-noweakptr` (driver at `5e509a4`, flags `GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout`). Result: PASS kube-apiserver smoke (/healthz ok; ConfigMap create/get/delete). Stock `go build` on the same machine, cold: 86,260 ms.
   123	
   124	Wall clock of 2 cold runs of this configuration, ms: 23,634, 20,536. The table is the first of them.
   125	
   126	| phase | ms | cores busy | core-s | bytes moved | floor ms | ms / floor | who does it |
   127	|---|---|---|---|---|---|---|---|
   128	| fetch (separate, not in total) | 0 (vendored) | | | | | | |
   129	| plan (package graph) + read inputs | 807 | 4.2 | 0.0 | 89 MB | 334 | 2x | driver; go.mod, build constraints |
   130	| &nbsp;&nbsp;of which lex + package-level declarations | 402 | 4.4 | 1.3 | 89 MB | 7 | 56x | Strand godecl |
   131	| head start | 5,570 | | | 0 kB | | | runtime closure compiled during the plan |
   132	| parse + resolve/types + lower/codegen | 20,591 | 7.7 | 151.9 | 1,043 MB | 318 | 65x | go tool compile/asm |
   133	| link | 1,666 | 1.6 | 2.5 | 182 MB | 28 | 59x | go tool link |
   134	| write output | 95 | 1.0 | 0.0 | 182 MB | 201 | 0x | driver |
   135	| **TOTAL** | **23,634** | **6.82** | **161** | read 201 MB, wrote 184 MB (disk) | **888** | **27x** | |
   136	| critical path (36 actions) | 8,984 | | | | | | |
   137	
   138	**The fair stock row.** Stock `go build`, cold, with the relaxations this build uses (`GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l' -ldflags='-s -w'`): **51,151 ms** (329 core-s). Plain stock in the same hour: 77,091 ms. This build: 23,634 ms. The gain to claim is against the fair row.
   139	
   140	**The materialization tax** (what the build pays for being separate processes that hand each other files; `tools/trace/tax.py`, driver counters):
   141	
   142	| | stock | fair stock | this build |
   143	|---|---|---|---|
   144	| wall ms, cold | 77,091 | 51,151 | 23,634 |
   145	| core-s | 511 | 329 | 161 |
   146	| tasks created, whole machine (processes + threads) | 18,018 | 16,510 | 9,278 |
   147	| tool processes started (exec) | 2,180 | 2,180 | 102 + 2,081 forked from one warm compiler |
   148	| page faults, whole machine | 26,907,866 | 26,418,964 | 2,299,864 |
   149	| intermediates written (archives, export data) | 5,466 MB | 4,283 MB | 1,762 MB |
   150	| intermediates read back (importers, linker, cache copy) | 9,909 MB | 7,993 MB | 4,781 MB |
   151	| of which export data | 764 MB | 713 MB |  |
   152	
   153	Stock writes every archive twice (work directory, then GOCACHE) to disk; this build writes each once to tmpfs. "Read back" counts the export data each importer maps and every archive the linker reads.
   154	
   155	Notes: plan + read: 2079 packages, 8940 Go files, 11 rounds; declarations: package clauses and import paths of every file, consumed by the plan; head start: 29 of its 29 packages adopted by the plan; their CPU is in the compile row; compile: bytes = object archives written to tmpfs; write output: the linker wrote the file in place; this row is the sync.
   156	
   157	```
   158	8 |▄▄▆█████████████████████████████████████████████████▅▂▁▁▁▁▃▁|   cores busy; one column = 394 ms
   159	0 +------------------------------------------------------------+
   160	```
   161	
   162	The chain that ends the build (each action was the last thing the next one waited for):
   163	
   164	| package | action | start ms | ms until it released the next | waited for a CPU token, ms | Go source |
   165	|---|---|---|---|---|---|
   166	| `k8s.io/kubernetes/cmd/kube-apiserver` | link | 21,408 | 1,666 | 21,408 | 0 kB |
   167	| `k8s.io/kubernetes/pkg/controlplane/apiserver` | compile | 19,887 | 757 | 4 | 68 kB |
   168	| `net/http/internal/http2` | compile | 2,488 | 447 | 0 | 370 kB |
   169	| `k8s.io/kubernetes/cmd/kube-apiserver/app` | compile | 20,953 | 440 | 0 | 20 kB |
   170	| `net/http` | compile | 2,935 | 439 | 0 | 562 kB |
   171	| `k8s.io/client-go/informers/core/v1` | compile | 8,380 | 434 | 0 | 180 kB |
   172	| `crypto/internal/fips140/nistec` | compile+asm | 1,318 | 396 | 0 | 624 kB |
   173	| `k8s.io/client-go/kubernetes` | compile | 7,626 | 387 | 2 | 35 kB |
   174	
   175	### kube-apiserver: every rung so far (8 CPUs, cold)
   176	
   177	Single cold runs. The host is shared: the same build has cost between 139 and 157 compile core-seconds at different times of the night, so differences under about 2 s between rows are not evidence; compare the phase columns.
   178	
   179	| run | wall ms | core-s | cores busy | plan | read | lex or declarations | compile | link | write | smoke test |
   180	|---|---|---|---|---|---|---|---|---|---|---|
   181	| stock `go build` | 86,260 | 554 | 6.43 | | | | | 4,974 | | |
   182	| `03-relaxed` (GOGC=1000 -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 53,910 | 346 | 6.42 | 1,114 | 176 | 243 | 49,219 | 2,453 | 203 | pass |
   183	| `04-selfplan` (GOGC=1000 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 44,406 | 320 | 7.22 | 601 | (in plan) | 228 | 40,657 | 2,465 | 194 | pass |
   184	| `06-baseline` (GOGC=1000 SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 28,974 | 196 | 6.77 | 723 | (in plan) | 247 | 24,974 | 2,404 | 373 | pass |
   185	| `07-baseline-float` (GOGC=1000 SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 29,561 | 201 | 6.80 | 739 | (in plan) | 354 | 25,523 | 2,429 | 366 | pass |
   186	| `08-huge-shard` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 24,090 | 158 | 6.56 | 609 | (in plan) | 260 | 20,521 | 2,119 | 334 | pass |
   187	| `09-forkserver` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote) | 24,517 | 162 | 6.60 | 622 | (in plan) | 224 | 21,031 | 2,031 | 330 | pass |
   188	| `10-onepass` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote) | 21,130 | 137 | 6.49 | 628 | (in plan) | 244 | 17,882 | 1,822 | 308 | pass |
   189	| `11-headstart` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart) | 23,800 | 154 | 6.46 | 827 | (in plan) | 420 | 20,110 | 1,983 | 308 | pass |
   190	| `12-directout` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout) | 20,097 | 135 | 6.70 | 758 | (in plan) | 378 | 16,998 | 1,810 | 104 | pass |
   191	| `13-noweakptr` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout) | 23,634 | 161 | 6.82 | 807 | (in plan) | 402 | 20,591 | 1,666 | 95 | pass |
   192	
   193	# The stock toolchain (rung 0: the gap)
   194	
   195	## minio
   196	
   197	Commit `7aac2a2c5b7c`, `go1.27.3 linux/amd64`, `CGO_ENABLED=0`, `-trimpath`. 1,312 packages (264 from the standard library), 7,531 Go files, 75.5 MB and 2,379,958 lines of Go, 109 assembly files (5.4 MB), 20.1 MB embedded. Output binary: 146.8 MB.
   198	
   199	### Stock toolchain, cold, by CPU count
   200	
   201	| CPUs | wall ms | core-seconds (user+sys) | cores busy (avg) | link wall ms | disk read | disk written |
   202	|---|---|---|---|---|---|---|
   203	| 1 | 234,039 | 229 | 0.98 | 4,195 | 544 MB | 2,074 MB |
   204	| 2 | 121,704 | 220 | 1.80 | 3,424 | 634 MB | 1,938 MB |
   205	| 4 | 72,561 | 237 | 3.27 | 3,280 | 598 MB | 1,835 MB |
   206	| 8 | 54,233 | 282 | 5.21 | 3,251 | 504 MB | 1,651 MB |
   207	| 8, warm page cache | 49,937 | 262 | 5.25 | 3,128 | 293 MB | 1,598 MB |
   208	
   209	### The build trace (8 CPUs, cold)
   210	
   211	Wall clock is given where a phase owns a stretch of the build; inside the parallel compile stretch the phases interleave across 1,300+ processes, so they are given in core-seconds (the compiler's own `-bench` timers, summed over all packages, scaled to the cold run's compile CPU).
   212	
   213	| phase | wall ms | core-s | cores busy | bytes moved | floor ms | stock / floor |
   214	|---|---|---|---|---|---|---|
   215	| fetch (separate, not in total) | 6,675 | | | 1,120 MB on disk | | |
   216	| read inputs | 4,296 (cold minus warm) | | | 504 MB read from disk; sources are 101 MB | 374 | 11x |
   217	| load package graph (`go build` before the first compile) | 3,255 | | 1.8 | | | |
   218	| **compile stretch** (first compile to link start) | 47,383 | 267 | 5.9 | 81 MB source in, 957 MB of objects out | | |
   219	| &nbsp;&nbsp;process start (init, load runtime decls) | | 2.1 | | | 0 (not needed) | |
   220	| &nbsp;&nbsp;lex/parse + resolve/types | | 50.4 | | 76 MB of Go | 152 | 41x (core-s / 8 against the floor) |
   221	| &nbsp;&nbsp;inline + escape analysis | | 26.4 | | | 0 (not needed) | |
   222	| &nbsp;&nbsp;lower/codegen | | 169.0 | | 76 MB of Go | 122 | 173x (core-s / 8 against the floor) |
   223	| &nbsp;&nbsp;write objects | | 18.8 | | | 0 (not needed) | |
   224	| link | 3,251 | 4.8 | 1.7 | 147 MB binary | 3 | 954x |
   225	| write output | (inside link) | | | 147 MB; the build wrote 1,651 MB in all | 163 | |
   226	| after link (install, exit) | 344 | | | | | |
   227	| **TOTAL** | **54,233** | **282** | **5.21** | | **815** | **67x** |
   228	| critical path (longest dependency chain, 30 actions) | 38,503 | | | | | |
   229	
   230	The compiler's own timers, seconds of wall clock inside the compiler processes summed over all packages (`-bench` run, 8 CPUs, warm page cache): be:compilefuncs 92.1, fe:parse 50.4, fe:devirtualize-and-inline 19.3, be:dumpobj 18.8, fe:escapes 7.0, fe:loadsys 1.2, fe:init 0.9, fe:pgo-load-profile 0.0. Its `parse` timer covers lexing, parsing, type checking and building the IR; it does not split them.
   231	
   232	### Cores busy over time (8 CPUs, cold)
   233	
   234	```
   235	8 |▂▂▂▃▃▄▅██████▇▇█▇█▇▇█████▇█▇██████▇▆▇▇▆▄▂▄▅▆▄▄▃▂▂▂▂▂██▄▁▁▁▂▂|   one column = 904 ms
   236	0 +------------------------------------------------------------+   load graph ends 3,255 ms, link 50,638-53,889 ms
   237	```
   238	Share of wall clock by number of busy cores: 1: 19%, 2: 11%, 3: 3%, 4: 4%, 5: 3%, 6: 4%, 7: 21%, 8: 35%. Idle core-seconds: 151 of 434.
   239	
   240	### Critical path through the package graph (8 CPUs, cold)
   241	
   242	The longest chain of actions, each waiting for the one before: 38,503 ms of the 54,233 ms wall clock. With unlimited cores and this toolchain the build could not be shorter than this.
   243	
   244	| # | package | action | start ms | ms | waited for a core, ms | Go source |
   245	|---|---|---|---|---|---|---|
   246	| 5 | `runtime` | build | 3,783 | 1,853 | 0 | 3 MB |
   247	| 9 | `syscall` | build | 5,829 | 403 | 0 | 244 kB |
   248	| 13 | `os` | build | 6,806 | 474 | 0 | 179 kB |
   249	| 16 | `encoding/json/v2` | build | 7,683 | 1,173 | 0 | 237 kB |
   250	| 18 | `github.com/goccy/go-json/internal/encoder` | build | 21,205 | 602 | 0 | 161 kB |
   251	| 19 | `github.com/goccy/go-json/internal/encoder/vm_color_indent` | build | 21,816 | 2,599 | 0 | 147 kB |
   252	| 20 | `github.com/goccy/go-json/internal/encoder/vm_color` | build | 24,416 | 2,806 | 0 | 147 kB |
   253	| 21 | `github.com/goccy/go-json/internal/encoder/vm_indent` | build | 27,226 | 4,985 | 0 | 145 kB |
   254	| 22 | `github.com/goccy/go-json/internal/encoder/vm` | build | 32,213 | 4,751 | 0 | 145 kB |
   255	| 25 | `github.com/minio/madmin-go/v3` | build | 37,218 | 1,419 | 0 | 1 MB |
   256	| 26 | `github.com/minio/mc/cmd` | build | 38,666 | 2,901 | 0 | 2 MB |
   257	| 27 | `github.com/minio/console/api` | build | 41,570 | 424 | 0 | 285 kB |
   258	| 28 | `github.com/minio/minio/cmd` | build | 42,040 | 8,546 | 0 | 5 MB |
   259	| 30 | `github.com/minio/minio` | link | 50,638 | 3,251 | 0 | 1 kB |
   260	
   261	(30 actions on the path; the 14 longest shown.)
   262	
   263	### The ten most expensive actions (8 CPUs, cold)
   264	
   265	| package | action | wall ms | core-s | Go source |
   266	|---|---|---|---|---|
   267	| `github.com/minio/minio/cmd` | build | 8,546 | 26.5 | 5 MB |
   268	| `github.com/elastic/go-elasticsearch/v7/esapi` | build | 8,460 | 17.6 | 3 MB |
   269	| `github.com/goccy/go-json/internal/encoder/vm` | build | 4,751 | 6.7 | 145 kB |
   270	| `github.com/minio/mc/cmd` | build | 2,901 | 6.3 | 2 MB |
   271	| `github.com/minio/minio` | link | 3,251 | 4.8 | 1 kB |
   272	| `runtime` | build | 1,853 | 4.5 | 3 MB |
   273	| `github.com/minio/madmin-go/v3` | build | 1,419 | 3.9 | 1 MB |
   274	| `github.com/goccy/go-json/internal/encoder/vm_indent` | build | 4,985 | 3.8 | 145 kB |
   275	| `github.com/IBM/sarama` | build | 2,424 | 3.1 | 880 kB |
   276	| `github.com/envoyproxy/go-control-plane/envoy/config/core/v3` | build | 2,455 | 2.9 | 1 MB |
   277	
   278	## kube-apiserver
   279	
   280	Commit `2cb26b34ab73`, `go1.27.3 linux/amd64`, `CGO_ENABLED=0`, `-trimpath`. 2,079 packages (244 from the standard library), 8,940 Go files, 89.3 MB and 2,500,778 lines of Go, 62 assembly files (0.8 MB), 0.0 MB embedded. Output binary: 172.8 MB.
   281	
   282	### Stock toolchain, cold, by CPU count
   283	
   284	| CPUs | wall ms | core-seconds (user+sys) | cores busy (avg) | link wall ms | disk read | disk written |
   285	|---|---|---|---|---|---|---|
   286	| 1 | 451,576 | 448 | 0.99 | 6,101 | 457 MB | 5,703 MB |
   287	| 2 | 233,876 | 441 | 1.89 | 5,466 | 768 MB | 5,513 MB |
   288	| 4 | 128,596 | 461 | 3.59 | 4,762 | 434 MB | 5,228 MB |
   289	| 8 | 86,260 | 554 | 6.43 | 4,974 | 813 MB | 4,863 MB |
   290	| 8, warm page cache | 73,522 | 485 | 6.60 | 4,898 | 74 kB | 4,677 MB |
   291	
   292	### The build trace (8 CPUs, cold)
   293	
   294	Wall clock is given where a phase owns a stretch of the build; inside the parallel compile stretch the phases interleave across 1,300+ processes, so they are given in core-seconds (the compiler's own `-bench` timers, summed over all packages, scaled to the cold run's compile CPU).
   295	
   296	| phase | wall ms | core-s | cores busy | bytes moved | floor ms | stock / floor |
   297	|---|---|---|---|---|---|---|
   298	| fetch (separate, not in total) | 0 (vendored in the repository) | | | | | |
   299	| read inputs | 12,738 (cold minus warm) | | | 813 MB read from disk; sources are 90 MB | 334 | 38x |
   300	| load package graph (`go build` before the first compile) | 4,037 | | 1.5 | | | |
   301	| **compile stretch** (first compile to link start) | 76,515 | 528 | 7.3 | 90 MB source in, 2,739 MB of objects out | | |
   302	| &nbsp;&nbsp;process start (init, load runtime decls) | | 3.5 | | | 0 (not needed) | |
   303	| &nbsp;&nbsp;lex/parse + resolve/types | | 148.6 | | 89 MB of Go | 180 | 103x (core-s / 8 against the floor) |
   304	| &nbsp;&nbsp;inline + escape analysis | | 61.2 | | | 0 (not needed) | |
   305	| &nbsp;&nbsp;lower/codegen | | 226.3 | | 89 MB of Go | 144 | 196x (core-s / 8 against the floor) |
   306	| &nbsp;&nbsp;write objects | | 88.0 | | | 0 (not needed) | |
   307	| link | 4,974 | 7.2 | 1.6 | 173 MB binary | 4 | 1,240x |
   308	| write output | (inside link) | | | 173 MB; the build wrote 4,863 MB in all | 192 | |
   309	| after link (install, exit) | 734 | | | | | |
   310	| **TOTAL** | **86,260** | **554** | **6.43** | | **854** | **101x** |
   311	| critical path (longest dependency chain, 54 actions) | 62,744 | | | | | |
   312	
   313	The compiler's own timers, seconds of wall clock inside the compiler processes summed over all packages (`-bench` run, 8 CPUs, warm page cache): be:compilefuncs 192.9, fe:parse 148.6, be:dumpobj 88.0, fe:devirtualize-and-inline 47.2, fe:escapes 14.0, fe:loadsys 2.1, fe:init 1.5, fe:pgo-load-profile 0.0. Its `parse` timer covers lexing, parsing, type checking and building the IR; it does not split them.
   314	
   315	### Cores busy over time (8 CPUs, cold)
   316	
   317	```
   318	8 |▃▁▂▃▆▇███████████████████▇███▇█████████████████▇████▇▅▅▃▁▁▃▁|   one column = 1,438 ms
   319	0 +------------------------------------------------------------+   load graph ends 4,037 ms, link 80,551-85,526 ms
   320	```
   321	Share of wall clock by number of busy cores: 0: 0%, 1: 10%, 2: 3%, 3: 2%, 4: 1%, 5: 1%, 6: 1%, 7: 26%, 8: 56%. Idle core-seconds: 136 of 690.
   322	
   323	### Critical path through the package graph (8 CPUs, cold)
   324	
   325	The longest chain of actions, each waiting for the one before: 62,744 ms of the 86,260 ms wall clock. With unlimited cores and this toolchain the build could not be shorter than this.
   326	
   327	| # | package | action | start ms | ms | waited for a core, ms | Go source |
   328	|---|---|---|---|---|---|---|
   329	| 27 | `k8s.io/apimachinery/pkg/apis/meta/v1` | build | 16,532 | 2,565 | 0 | 527 kB |
   330	| 28 | `k8s.io/api/core/v1` | build | 19,675 | 6,910 | 66 | 3 MB |
   331	| 29 | `k8s.io/api/resource/v1` | build | 27,493 | 2,015 | 0 | 703 kB |
   332	| 37 | `k8s.io/client-go/informers/core/v1` | build | 37,139 | 1,897 | 0 | 180 kB |
   333	| 39 | `k8s.io/client-go/informers` | build | 43,240 | 3,141 | 0 | 53 kB |
   334	| 42 | `k8s.io/apiserver/pkg/admission/plugin/policy/generic` | build | 50,300 | 2,075 | 0 | 81 kB |
   335	| 44 | `k8s.io/apiserver/pkg/admission/plugin/policy/validating` | build | 52,436 | 2,476 | 0 | 61 kB |
   336	| 45 | `k8s.io/apiserver/pkg/admission/plugin/policy/mutating` | build | 54,912 | 2,551 | 0 | 28 kB |
   337	| 46 | `k8s.io/apiserver/pkg/server` | build | 57,465 | 2,913 | 0 | 175 kB |
   338	| 47 | `k8s.io/apiserver/pkg/server/options` | build | 60,450 | 2,800 | 0 | 164 kB |
   339	| 48 | `k8s.io/kubernetes/pkg/kubeapiserver/options` | build | 67,895 | 1,954 | 0 | 67 kB |
   340	| 49 | `k8s.io/kubernetes/pkg/controlplane/apiserver/options` | build | 69,849 | 1,970 | 0 | 26 kB |
   341	| 50 | `k8s.io/kubernetes/pkg/controlplane/apiserver` | build | 74,875 | 1,847 | 0 | 68 kB |
   342	| 54 | `k8s.io/kubernetes/cmd/kube-apiserver` | link | 80,551 | 4,974 | 0 | 1 kB |
   343	
   344	(54 actions on the path; the 14 longest shown.)
   345	
   346	### The ten most expensive actions (8 CPUs, cold)
   347	
   348	| package | action | wall ms | core-s | Go source |
   349	|---|---|---|---|---|
   350	| `k8s.io/api/core/v1` | build | 6,910 | 7.9 | 3 MB |
   351	| `k8s.io/kubernetes/cmd/kube-apiserver` | link | 4,974 | 7.2 | 1 kB |
   352	| `k8s.io/kubernetes/pkg/generated/openapi` | build | 2,360 | 5.5 | 4 MB |
   353	| `cel.dev/cel-go/common/types` | build | 4,375 | 4.8 | 330 kB |
   354	| `k8s.io/kubernetes/pkg/apis/core/validation` | build | 3,515 | 4.5 | 486 kB |
   355	| `runtime` | build | 1,720 | 4.2 | 3 MB |
   356	| `k8s.io/client-go/kubernetes/typed/core/v1/fake` | build | 3,035 | 3.3 | 48 kB |
   357	| `k8s.io/apiserver/pkg/server` | build | 2,913 | 3.1 | 175 kB |
   358	| `k8s.io/apiextensions-apiserver/pkg/apiserver` | build | 3,080 | 3.0 | 91 kB |
   359	| `k8s.io/kubernetes/pkg/apis/core/v1` | build | 2,518 | 2.9 | 655 kB |
   360	
   361	## How the floors are computed
   362	
   363	A floor is the larger of (bytes moved / measured bandwidth of the level they move through) and (bytes x modelled ticks per byte / (8 cores x 3.095e9 ticks/s)). The bandwidths are measured. The ticks per byte are budgets for a data-parallel implementation, set in `tools/trace/mktrace.py`: lex 2, parse 8, types 40, codegen 40. They are the weakest numbers in this file. The link floor is one pass over the objects in and the binary out at memory bandwidth: a copy floor, which says nothing about symbol resolution or relocation (no model for those yet). For scale: the Strand Go lexer measures 15 ticks/byte today (GOLEX.md), and the stock toolchain spends about 11,578 ticks per source byte end to end on MinIO.
   364	
   365	## Does 2 seconds fit on this machine?
   366	
   367	By the floors above, MinIO's irreducible work is about 6.5 core-seconds (815 ms on 8 cores), inside the 16 core-seconds that 2 s on this VM allows, so physics does not rule the target out here. The stock toolchain does 282 core-seconds of work for the same build: with it doing the work, 2 s would need 141 perfectly used cores, and its dependency chain (38,503 ms) would still be in the way. So the gap is 18x in work done and 19x in the critical path; both have to go.
   368	

exec
/bin/bash -lc 'nl -ba JOURNAL.md | tail -n 330' in /home/exedev/strand-w
 succeeded in 0ms:
  1323	0x8000000000000000, 0x3fd0000000000000 and 0xbfd0000000000000: **a float64 and its negation.** A short
  1324	content-addressed symbol's "hash" is its content; my key was content x odd constant, which moves a
  1325	top-bit difference only into the top bit; and I forced the top bit to 1 to keep keys non-zero. In a
  1326	real linker that is +0.25 silently linked as -0.25. Fixed (a full mixer for short hashes; zero is
  1327	remapped instead of a bit being forced): 0 collisions, 960,375 distinct keys, the same as the
  1328	string-keyed reference. Timing unchanged: 7-11 + 82-84 + 20-24 + 4-6 + 33-38 ms, **about 0.16 s** on 8
  1329	cores (0.71 s on one).
  1330	
  1331	Worth keeping from this: the string-keyed version was slow and was the oracle that caught the fast
  1332	one. F4 needs that pair permanently: a slow, obviously-right resolver to diff the fast one against on
  1333	every build, exactly as the stock toolchain is for the compiler phases.
  1334	
  1335	## 2026-11-12 18:40 UTC — iteration 28: reachability in the link spike: 14-32 ms
  1336	
  1337	The spike lacked the two serial algorithms of a link. This adds the first: reachability as a
  1338	frontier-parallel flood over relocation and aux edges, from the entry points and every package's init
  1339	task (821 roots), marking with compare-and-swap on a bitmap.
  1340	
  1341	    MinIO, 8 cores                               ms     levels   symbols live   bytes live   edges followed
  1342	    method tables followed like any edge       31-32      35       703,853        159 MB        2.14 M
  1343	    method tables not followed                 14-19      40       334,818        100 MB        1.10 M
  1344	    (one core: 154 and 73 ms. Stock linker's dead-code pass: 246 ms.)
  1345	
  1346	The two rows bracket the Go linker's rule, which keeps a method only if some reachable interface or
  1347	reflection can call it: its live set lies between them, and its algorithm is the same flood with a
  1348	deferred queue for methods. It is **not** implemented here, so this is a cost, not a correct pass.
  1349	
  1350	**The spike's link core, all together** (map 7-13, claim 82-106, resolve 24, reachability about 30,
  1351	layout 4, copy + relocate 32-37): **about 0.19 s on 8 cores** for what takes the stock linker about
  1352	0.9 s of its 1.1 (loadlib 440, deadcode 246, dodata + asmb 230). Still missing: pclntab (114 ms in
  1353	stock; per-function tables concatenated with offsets by prefix sum: Strand-shaped), the Go method
  1354	rule, symbol table and ELF headers (small), and the hard part, which is not speed: being right.
  1355	An honest reading for the ladder: F4 could take the link from 1.1 s to perhaps 0.3 s, about 0.8 s of an
  1356	11.5 s build, and it is the only item left on this machine that is both large and not "replace the
  1357	compiler".
  1358	
  1359	**Scope check.** Most ambitious: make the spike emit a binary that runs. That is F4 itself, and it is
  1360	where the next session should start; the measurements say it is worth it and roughly what it costs.
  1361	
  1362	## 2026-11-12 19:05 UTC — kube-apiserver soak, thirty more
  1363	
  1364	30 of 30 cold builds pass their smoke test; wall min 19.8 s, median 21.3 s, max 23.8 s. The run of
  1365	twenty an hour earlier had a median of 19.8 s: the same binary-identical configuration, a host 1.5 s
  1366	slower. Totals for the final configuration today: MinIO 80 of 80, kube-apiserver 56 of 56, no hang and
  1367	no failed fork in either.
  1368	
  1369	## 2026-11-12 19:30 UTC — correction to iteration 28: the reachability claim, checked against the stock linker
  1370	
  1371	Iteration 28 said the stock linker's live set "lies between" the spike's two variants. I had not
  1372	checked it. Checked now (functions kept by a stock link of the same objects, `go tool nm`, against the
  1373	names of the spike's live functions, `F4LINK_DUMPLIVE`):
  1374	
  1375	- As written in iteration 28 the claim was **false**: the stock linker keeps 121,052 functions and the
  1376	  spike's wider variant reached 106,976, missing 26,659 of the stock's. The roots were wrong: I had
  1377	  used the symbols flagged as package init *functions*; the things that make inits reachable are the
  1378	  `..inittask` records.
  1379	- With init tasks as roots (2,034 roots): the wider variant reaches 132,616 functions and contains all
  1380	  but **2,376 (2%)** of the stock's 121,052: 1,530 plain functions (runtime builtins referenced by
  1381	  number, some linknames and ABI wrappers: edges the spike does not follow), 408 generic
  1382	  instantiations and 363 methods (partly naming differences between the two lists), 66 inits, 9
  1383	  assembly-local. It keeps 13,940 the stock linker drops (methods no interface can reach). The narrow
  1384	  variant is not a subset of the stock set (1,960 extra, mostly package `init` functions the stock
  1385	  linker proves dead), so "bracket" was the wrong word for it too.
  1386	- Timing with the right roots: **41-42 ms** following method tables (821 k symbols live, 2.57 M edges),
  1387	  **23-24 ms** without. The link core is then about **0.20 s** on 8 cores, not 0.19.
  1388	
  1389	So: the cost estimate stands within 10 ms; the pass is 98% of a correct one from above and needs the
  1390	Go method rule, builtin references and linkname edges before its live set can be diffed to zero
  1391	against the stock linker's, which is the test F4 has to pass.
  1392	
  1393	## 2026-11-12 19:50 UTC — the spike's live set against the stock linker's: 99.7%
  1394	
  1395	Two more steps on the comparison of the previous entry. (1) The spike now follows references to
  1396	runtime builtins (the compiler refers to them by number). (2) Most of the 2,376 "misses" were
  1397	artifacts of the comparison: `go tool nm` prints ABI0 wrappers with an `.abi0` suffix, and FIPS code
  1398	lives in its own text kind that my dump skipped.
  1399	
  1400	With both fixed: the stock linker keeps 121,013 functions; the spike's flood (method tables followed)
  1401	reaches 133,733 and **misses 346 of the stock's (0.3%)**: 301 generic instantiations (to be checked:
  1402	probably how the two lists spell shape types), 35 others (linker-made markers such as
  1403	`go:textfipsstart`, assembly reached through jump tables, methods of function-local types), 6 methods,
  1404	4 unnamed. It keeps 13,066 the stock linker drops: methods that no reachable interface can call, which
  1405	is exactly what the Go method rule removes. Timing unchanged (reachability 40-45 ms, link core about
  1406	0.20 s).
  1407	
  1408	This is the test F4 has to drive to zero in both directions, and it took an afternoon to get within
  1409	0.3% from above. That is the best evidence so far that F4 is a rung of weeks, not months.
  1410	
  1411	## 2026-11-12 20:05 UTC — the suite on the final tree
  1412	
  1413	`./test.sh` (bootstrap stages, fuzzers, pool, the Strand linker and Go lexer against their references):
  1414	ALL PASS. Driver and compiler tests on the final compiler: `tests/sb_zygote.sh` (fork-server, head
  1415	start, direct output, oversized request, no `-w`), `tests/sb_early.sh`, `tests/sb_ptrto.sh`,
  1416	`tests/sb_same.sh`: pass. Standard-library oracle: 129 of 141 (accounted for in iteration 22's
  1417	addendum). MinIO unit tests: 45 packages, the one known failure. Soaks: MinIO 80 of 80,
  1418	kube-apiserver 56 of 56.
  1419	
  1420	## 2026-11-12 20:20 UTC — the 301 "missing" generics were my awk: 46 of 122,860
  1421	
  1422	The previous entry left 346 functions the stock linker keeps and the spike does not, 301 of them
  1423	generic instantiations "to be checked". Checked: the comparison split symbol names at the first space,
  1424	and instantiated names contain spaces (`slices.SortFunc[go.shape.struct { ... }]`). With whole names:
  1425	the stock linker keeps **122,860** functions; the spike's flood reaches 133,733 and misses **46
  1426	(0.04%)**: about 25 assembly routines reached through a table (`scan.expandAVX512_*`), about 16
  1427	methods of function-local types (the two lists spell the local-type suffix differently, so probably
  1428	not misses at all), 2 linker-made markers, 3 others. It keeps 10,919 the stock linker drops.
  1429	
  1430	So from above the flood is complete to within one identifiable case (assembly tables). What F4's
  1431	reachability still needs is the method rule, to remove the 10,919.
  1432	
  1433	## 2026-11-12 20:55 UTC — last soak; end of session
  1434	
  1435	Seventy more cold MinIO builds, each smoke-tested: 70 of 70; wall min 10.6 s, median 11.2 s, max
  1436	13.1 s (the same median as the run of sixty). Totals for the final configuration: MinIO 150 of 150,
  1437	kube-apiserver 56 of 56; no hang, no failed fork, no failed smoke test.
  1438	
  1439	State at the end: configuration `13-noweakptr`; MinIO 13.2 -> 11.8 s against last night's
  1440	configuration (order-balanced), median 11.2 s over 130 soak builds; kube-apiserver 23.0 -> 21.1 s.
  1441	MORNING.md is current; NEXT.md starts the next session at F4 with `explore/f4link`.
  1442	
  1443	## 2026-11-12 21:15 UTC — iteration 29 (short): one hypothesis about the 46, wrong
  1444	
  1445	Inbox empty. Scope check: the most ambitious thing in twenty minutes is one more step on the F4
  1446	spike's diff against the stock linker. The largest group of the 46 missing functions is about 25
  1447	assembly routines (`internal/runtime/gc/scan.expandAVX512_*`). Hypothesis: they are reached through
  1448	function-value symbols (`f·f`) that no object defines and the linker makes. Tried: resolve an
  1449	undefined `f·f` to `f`. Result: still 46 missing, the same ones. **Wrong; reverted.** They are reached
  1450	some other way (a table built in assembly data, or a linkname); left as the first item for the next
  1451	session's diff, with the ten local-type methods whose names the two lists probably spell differently.
  1452	
  1453	## 2026-11-12 21:30 UTC — iteration 30: soak while the clock ran down
  1454	
  1455	Inbox empty. Scope check: nothing a rung needs fits in the last minutes; more evidence on the
  1456	fork-server's one risk does. Thirty more cold MinIO builds, each smoke-tested: 30 builds, 30 pass; wall min 10566, median 11405, max 13848 ms.
  1457	Total for the final configuration: MinIO 180 of 180, kube-apiserver 56 of 56.
  1458	
  1459	## 2026-11-12 19:52 UTC — campaign 1 begins: fc, a compiler of our own, compiles its first package
  1460	
  1461	M0: the scoreboard run (tools/scoreboard/run.sh, every corpus project cold at 8/4/2/1 cores, stock and fair
  1462	stock, Bazel projects from their own DAG) is going in the background; SCOREBOARD.md is generated when it ends.
  1463	
  1464	How (the campaign leaves it to me; why each choice):
  1465	- **Front end: go/types over source, whole dependency closure in one process** (driver/fc/load.go). No export
  1466	  data read or written. Measured earlier at 1.4 s wall for all of MinIO.
  1467	- **Lowering: one walk over the typed syntax tree straight to machine code** (gen.go, expr.go, stmt.go,
  1468	  call.go): no IR, no SSA, no obj.Prog. The frame model is the baseline generator's, which MinIO proved:
  1469	  every variable in memory, one always-live stack map, zeroed frame, write barriers for pointer stores
  1470	  leaving the frame. New: locals are addressed from BP and outgoing arguments from SP, so nothing depends on
  1471	  the final frame size and the prologue is assembled after the body. Address-taken and captured variables
  1472	  go to the heap (no escape analysis yet).
  1473	- **Own x86 encoder** (x86.go), bytes and relocations written directly; pc tables (pcsp, file, line, stack
  1474	  map index, unsafe points) built by hand.
  1475	- **Object file: cmd/internal/obj's writer, fed LSyms filled in directly.** A bridge until M5 puts code in the
  1476	  image; it is why fc builds inside the patched GOROOT (driver/mkfc.sh).
  1477	- **Mixed mode for the oracle** (tools/fc/test.sh, toolexec.sh): the stock compiler compiles the package
  1478	  (its export data is what stock importers read), then fc replaces the object in the archive. Stock importers
  1479	  refer to a package's symbols by index, so fc orders its symbols as the stock object does
  1480	  (alignWithStock) and names by name what only it defines.
  1481	
  1482	First package: tests/fc/basic (arithmetic, loops, strings, slices, switches, bounds panics) passes its tests
  1483	compiled by fc. unicode/utf8 next: it calls a generic function, so instantiation comes first.
  1484	
  1485	## 2026-11-12 19:57 UTC — M1: unicode/utf8 compiled by fc passes its tests
  1486	
  1487	Generic functions are compiled per instantiation (generics.go: the body is walked again with the type
  1488	arguments substituted wherever type information is read; no shapes, no dictionaries). With that,
  1489	`tools/fc/test.sh unicode/utf8` is green: every function of the package is fc's code, the rest of the test
  1490	binary is stock. One test is skipped and listed (tests/fc/skips.txt): it asserts zero allocations for a
  1491	conversion the stock compiler keeps on the stack by escape analysis, which fc does not have (a dev-build
  1492	difference of the same family as the charter's finalizer tests; recorded for Dan in STATUS.md).
  1493	Two builders work in their own worktrees: type descriptors and itabs checked byte for byte against stock
  1494	objects, and a corpus of language-feature test packages (tests/fc/lang).
  1495	**What did the oracle number do since the last milestone?** 0 -> 1 standard-library package. No blocker: the
  1496	next packages need type descriptors (a builder is on it), closures, interfaces, maps, defer; the survey
  1497	script orders them.
  1498	
  1499	## 2026-11-12 23:23 UTC — M2: 205 standard-library packages pass; the kill test, measured; MinIO links
  1500	
  1501	Since M1: type descriptors and itabs (a builder's port of reflectdata to go/types, checked against stock
  1502	objects: 81,134 of 87,546 descriptors identical over std, 7 different in one harmless field, the rest stock's
  1503	shape types), closures, defer/go/recover, method values and expressions, interfaces, maps, channels, select,
  1504	range-over-func, complex numbers, print, ABI wrappers for assembly, embed, static initialization, generics by
  1505	instantiation. A second builder wrote tests/fc/lang (21 packages, ~670,000 differential checks against
  1506	stock-compiled copies of the same source); all 21 pass. LOOP.md changed on the remote mid-run (hand tests,
  1507	oracle triage and re-reviews to Codex): the failing-package triage went to Codex.
  1508	
  1509	Things the oracle taught, each a mechanism: stock importers address a package's symbols by index (fc orders
  1510	its symbols as the stock object does, unnamed entries included, and addresses imported symbols by index so
  1511	that content-hashed itabs merge); stock callers stack-allocate arguments on the strength of the stock
  1512	compiler's escape analysis of the callee (the harness gives the package under test export data that says
  1513	every pointer parameter escapes: tools/fc/mkcompile-esc.sh); the stock compiler evaluates call-free
  1514	right-hand sides last to first; a runtime copy of a nil-derived address is fatal, not a panic.
  1515	
  1516	**Kill test** (PLAN.md; tools/fc/cost.sh; 205 packages, 8.23 MB; three rounds, medians): stock baseline mode
  1517	6.18 core-s (6.00-7.08); fc 2.80 core-s attributable to those packages (2.65-3.23), 3.48 for the whole
  1518	process, which also type-checks the closure and runs the assembler. Ratio 0.45: **above one third**. The
  1519	campaign continues because the path is measured, not argued: (1) of fc's time on these packages about 0.8
  1520	core-s is go/parser + go/types and about 1.9 is everything after (per-package wall sums, so contention
  1521	inflates it); (2) the profile of the MinIO build names what the 1.9 is made of: the object writer 9%, the
  1522	content hashing it needs, growing buffers and maps in the code generator, the assembler as one process per
  1523	file; (3) the first four cheap items (layout caches, collector off as in the baseline's own configuration,
  1524	map sizes, calling the assembler directly) took the ratio from 0.60 to 0.45 in an hour. On the campaign's
  1525	target the comparison is already near the line: MinIO whole-program 26-27 core-s and 5.4-6.3 s wall (1,283
  1526	packages by fc in one process, assembler and stock link included; the runtime's 30 packages prebuilt, so
  1527	not a cold number) against 74 core-s for the frozen path. The same test at M3 will say whether the path was
  1528	real.
  1529	
  1530	**What did the oracle number do since the last milestone?** 1 -> 205 standard-library packages (of 262 with
  1531	tests); language corpus 0 -> 21 of 21; MinIO 0 -> builds and links, does not run. No stall. Blockers ahead:
  1532	the 53 failing packages (triage running), the gob init failure in the MinIO binary, and escape analysis for
  1533	the runtime's packages.
  1534	
  1535	## 2026-11-13 03:43 UTC — MinIO and kube-apiserver built by fc pass their smoke tests, cold
  1536	
  1537	Whole-program mode (`fc build -link`): one process type-checks the closure and compiles every package above
  1538	the runtime; packages with assembly go through fc too (the assembler's symabis, a go_asm.h written from
  1539	go/types, one archive per package); the runtime's closure is built by the go command inside the build; the
  1540	stock linker links. Bugs on the way, each found by running the real binary: itabs must merge by name when no
  1541	object agrees on symbol indexes; assembly may leave BP wrong (stock code never addresses through it; fc's
  1542	locals do, so BP is recomputed from SP after every call into assembly); a result returned on the stack was
  1543	copied before the register results were saved; narrow results come back with undefined upper bits (Codex's
  1544	triage reduced that one from four failing packages).
  1545	Cold (tools/fc/cold.sh: fresh GOCACHE, page cache dropped, the driver's own planner instead of go list),
  1546	with two other agents using the machine: MinIO 7.7-8.7 s wall, 34-36 core-s; kube-apiserver 8.7-9.1 s, 42-43
  1547	core-s; both pass their smoke tests. Not yet measured on a quiet machine, and the scoreboard's "ours" column
  1548	is still empty.
  1549	
  1550	## 2026-11-13 09:20 UTC: the scoreboard's fc column
  1551	
  1552	The machine was idle for the first time in a day at 08:50, so the column was run then, twice: with the stock
  1553	linker (MinIO 8.6 s on 8 cores) and with fclink (6.8 s). The second run is the one in SCOREBOARD.md; every
  1554	binary passed its check, prometheus included once the planner understood workspaces. What the day's noise
  1555	had hidden: during the front end only 3-4 of 8 cores were busy (8.6 core-s in the first 2.5 s), because each
  1556	package's full check waited on its imports' full checks. Measured first: declarations-only checking of the
  1557	whole closure takes 0.9 s wall, 4.3 core-s, parse included. So a package is now checked twice, declarations
  1558	for its importers and bodies for itself; the two passes make distinct type objects, which cost one fix per
  1559	place that compared objects across packages (instances bind the full pass's type parameters; packages compare
  1560	by path; found by tailscaled's go-tpm instance). MinIO warm went 5.3/5.6/5.6 -> 4.7/5.1/4.6 s, interleaved,
  1561	equal core-s. MinIO's own tests pass with every package above the runtime by fc (cmd's one failure is
  1562	stock's too). The runtime through fc compiles; what stops it is nosplit depth, and behind that escape
  1563	analysis, which is written, has an oracle, and was wrong four times on our own 22-package corpus.
  1564	Is the oracle still the right one? For speed, yes: cold wall on 8 cores with checked binaries. For
  1565	correctness the escape oracle (stock's -m) missed what the language corpus caught at once, so analysis
  1566	changes are now judged by both, corpus first.
  1567	
  1568	## 2026-11-13 10:40 UTC: the runtime through fc
  1569	
  1570	Escape analysis came back from its builder with zero unsound decisions against the stock compiler on std and on
  1571	our corpus, and one rule worth keeping: a site the analysis calls non-escaping must be in the frame, or the
  1572	analysis must be told the code generator will heap-allocate it, because a heap object must not hold a frame
  1573	address. It costs 12-19% of a build's CPU, so it is on only for the runtime's packages, which need it to be
  1574	correct. With it, frame forms for the allocations the runtime makes before the heap exists, the race build's
  1575	stack guard (fc's frames are twice the stock compiler's; the worst nosplit chain is 1040 bytes against 792),
  1576	and direct calls of stack-ABI assembly (the runtime's recover counts frames to systemstack), hello ran, then
  1577	MinIO passed its smoke test with no stock-compiled package in it.
  1578	What the stock compiler gives the runtime silently and fc had to be taught, each found by a crash: liveness
  1579	(the collector followed a stale slot into a span's inline mark bits: "tidy frames", per-call stack maps for the
  1580	runtime's packages only); linknamed variables are one symbol across packages (maps.errNilAssign was two);
  1581	static initialization of []byte("...") (read before any init runs); the real signatures of runtime functions
  1582	the compiler's own list declares with stand-ins (a callee compiled by fc spills by its real signature).
  1583	Still wrong: under concurrent marking on more than two CPUs the collector loses objects. STW collection, one
  1584	or two CPUs, and the stock runtime are all fine.
  1585	Is the oracle the right one? For the runtime, no longer: a smoke test and four small programs passed while the
  1586	collector was unsound. The oracle for M3 is now test binaries built wholly by fc (`FC_ALL=rt`), corpus first,
  1587	then std, and a reproduction that fails every time rather than one run in four.
  1588	
  1589	## 2026-11-13 12:10 UTC: the lost objects
  1590	
  1591	The collector losing objects on an fc-built runtime was not in the collector. Two causes, both things the
  1592	stock compiler does that the runtime's source relies on without saying so:
  1593	1. **No stack check in helpers.** The bulk write barrier takes its P's buffer and walks a pointer mask; it is
  1594	   nosplit so that it cannot be rescheduled in between. The walk calls sys.TrailingZeros64, which the stock
  1595	   compiler replaces by an instruction, and small helpers ((*mspan).base, markBits.setMarked) it inlines. fc
  1596	   called them as functions with a stack check, the check is where a goroutine is preempted, and the barrier
  1597	   resumed on another P still holding the first P's buffer: entries lost, or a nil buffer pointer mid-flush
  1598	   (the crash that finally named the function). Fix: in the runtime's packages, functions the stock compiler
  1599	   would inline or intrinsify are compiled without a stack check; everywhere, small leaf functions have none
  1600	   (the stock assembler's own rule; syscall's code after fork needs it).
  1601	2. **mcall resumes its caller with the wrong BP** by design: it saves the caller's saved BP ("storing BP as it
  1602	   is can cause a frame pointer cycle"), harmless to code that never reads BP. fc addresses locals through BP:
  1603	   gopark returned into its caller's caller. It only looked alive because the usual caller, goparkunlock, had
  1604	   nothing left to do. Fix: BP is recomputed from SP after calls to the package's own assembly.
  1605	What found it: a reproduction that failed every run (reflect calls under allocation pressure), then reading
  1606	the faulting pc instead of the traceback's first user frame. I lost an hour to the latter: the runtime prints
  1607	the caller of the runtime function that faulted, and "fault at a BP-relative store after a call" was the return
  1608	address, not the fault.
  1609	After the fixes: the reproduction 20 of 20, tools/fclink/progs/gcstress 20 of 20 against stock, the language
  1610	corpus 23 of 23 with test binaries built wholly by fc (runtime included, linked by fclink), MinIO and
  1611	kube-apiserver built that way pass their smoke tests.
  1612	Is the oracle the right one? The wholly-fc test binary is, and it is new today; the std list is running in it.
  1613	
  1614	- 2026-11-13 22:08 UTC: fclink overrides go:fipsinfo when an object defines it (every object does: the check package's linknamed variable), so GODEBUG=fips140=on finds its checksum: crypto/cipher, crypto/fips140, crypto/internal/fips140only pass wholly by fc. encoding/binary TestSizeAllocs skipped (allocation counts; passes with FC_ESC=1). debug/buildinfo fails with stock here too (the test's go build cannot get VCS status).
  1615	- 2026-11-13 22:13 UTC: `fc build -link` defaults: fclink (beside the fc binary) and the runtime by fc (FC_RUNTIME=0 / FC_LINKER= for the old way); GOROOT and the module cache found without `go env`. A default build starts no go command: buildifier's processes are fc, fclink and 55 asm. Disk was 99% full (958 MB): stock MinIO cmd failed 11 put-object tests because of it; freed 13 GB of old scratch objects and the default GOCACHE.
  1616	- 2026-11-13 22:16 UTC: explicit huge pages for fc's heap (tools/fc/cold.sh HUGEPAGES, default 10 GB pool, restored after; SB_HUGE in the patched runtime fc is built with). Order-balanced A/B, 4 pairs, MinIO warm, machine busy with test runs: kernel time 6.3-7.3 -> 2.4-2.8 s, process wall 9.0-10.5 -> 6.7-7.6 s, exit teardown 0.6-0.8 -> 0.04 s (mechanism: 8.3 GB of 4 kB faults become 2 MB ones). Cold, wholly by fc, fclink: MinIO 6.6 s / 21.4 core-s (10.5 s / 32.1 without), kube-apiserver 7.6 s / 27.5 core-s; both smoke tests pass.
  1617	- 2026-11-13 22:41 UTC: two data races in fc found by its race build (FC_RACE=1 driver/mkfc.sh) after hash/maphash crashed a compile with 'concurrent map writes': vargen's map (now under a mutex) and full() reading a package's declarations-pass pointer while that pass ran (now a concurrent set). buildifier and caddy whole-program builds: 0 reports. std wholly by fc (FC_ALL=rt), the 226 of tests/fc/passing.txt: 226 pass (index/suffixarray takes 387 s against stock's 51 s at -l, hash/maphash 129 s against 16 s: over batch.sh's 300 s limit). MinIO cmd, whole suite, disk freed, cores 4-7: stock and wholly-fc both fail only TestTrackingResponseWriter (fc-minio-cmd-flake.log). 36 std packages with tests are not on passing.txt (fmt, strings, net/http, runtime, reflect...): running them now.
  1618	- 2026-11-13 22:53 UTC: the critical path, from per-package timestamps (`fc build -stats`, tools/fc/timeline.py): slots were handed out in arrival order, so minio/cmd's check (1.3-1.6 s, serial) started late, its back end waited 1.1 s more for a slot and then ran nearly alone at the end. Two changes. (1) sched.go: slots to the biggest package waiting, function jobs of a package ahead of smaller packages' starts (FC_FIFO=1: as before). (2) Declarations once, bodies later (driver/gotypes: types.CheckDecls + Bodies added to go/types in the patched GOROOT; FC_BODIES=0: the two passes): the declarations pass records into the package's Info and keeps the bodies; check() checks them against the same *types.Package, in up to 8 shards for packages over 512 kB, merged into one Info. Importers and the package itself now see the same objects. MinIO warm, huge pages, machine shared with a test batch (load 7): front end done at 1.6-2.0 s (two-pass 2.0-2.1, before priorities 3.8-6.5), cores-busy profile flat to the end (no tail). Smoke passes; language corpus 23/23 wholly by fc. Wall clock comparison waits for an idle machine.
  1619	- 2026-11-13 23:11 UTC: the time package's tests hung wholly by fc (even the test binary's own timeout never fired): a ticker read deadlocked on its timer's sendLock. Cause: fc compiled `select { case c <- v: default: }` as selectgo; the stock compiler emits selectnbsend, and the runtime counts on it (time.sendTime runs inside unlockAndRun; selectgo looks at the channel's timer again, due again for a 1 ns ticker). One-case selects are now a plain or non-blocking send/receive as stock's. Found with gdb: frame chain through BP, symbolized via addr2line (no symbol table in fclink's output). Also from the 36 std packages outside passing.txt: fclink function-table order (fixed earlier), crypto/tls TestWeakCertCache (liveness skip) and TestBogoSuite (fails with stock here: fetches BoringSSL), runtime/cgo (does not link with stock either at CGO_ENABLED=0), runtime's own suite: 9 failing tests, a builder is on them.
  1620	- 2026-11-13 23:21 UTC: fc's CPU on MinIO by profile (18.5 s of samples, machine loaded, shares only): back end 61% (function bodies 42%, type descriptors 14%, writing objects 12%), bodies check 16%, parse 8%, declarations 8%; hash maps about 23% across all of it (the checker's Types/Uses maps and fc reading them, fc's size cache). Taken: values boxed through convT64/32/16/string/slice as stock (no allocation for small integers); sizeof/alignof of word-shaped types without lock or map (0.5 s of map lookups in the profile); type link names cached program-wide now that all packages share type objects (typeSymName 0.6 s). Not measurable by wall clock under tonight's load (same-binary runs differ by 2 core-s); kept on the mechanism. Noted: two builds of the same source are not byte-identical (same size, addresses differ: functions are numbered in the order parallel compilation finishes them); behaviour is what is tested. To list in MORNING.
  1621	- 2026-11-13 23:29 UTC: first quiet-ish window (load 5, a builder running): MinIO cold, wholly by fc, fclink, huge pages, priorities, declarations once: 3.8-4.1 s wall, 22.4-23.0 core-s, smoke passes (this morning's scoreboard: 6.8 s). Warm timeline: 6-7 cores busy from 0.2 s to 3.0 s, front end done at 1.33 s, link 227 ms: the build is CPU-bound now, so the exit test (3.3 s) is a matter of core-seconds. Then: each function body's checker results in an Info of its own (FC_FNINFO=0: one per package; go/types Bodies.Check takes the Info per body): the package-wide maps were 0.9 GB and their probes the top line of the profile. Order-balanced, 4 pairs, MinIO warm, load 16: user CPU 17.13-17.67 -> 16.39-16.81 s (mean -0.77 s); matchH2 in the profile 1.86 -> 0.96 s. 1312 of 1312 packages, smoke passes, language corpus 23/23 mixed and wholly-fc. Packages under the escape analysis (the runtime's closure, FC_ESC=1) keep one Info.
  1622	- 2026-11-13 23:44 UTC: the start of a build: the whole program was parsed before any checking began, and a package's imports were known only after its full parse. Now the graph is found from the import declarations (a tokenizer pass over each file's head, scan(); file reads outside the CPU slots, 32 at once), parsing runs behind it, and imported packages start while the importer parses. First 200 ms of a warm MinIO build: 4.2 -> 7.1 cores busy. Wall and CPU unchanged within tonight's noise (order-balanced, 4 pairs warm 3.51 vs 3.51 s; cold 3.65 vs 3.57 s at load 5): the build is CPU-bound, so filling 0.6 core-s of idle is at most 75 ms. Kept for the mechanism, not for a measured gain. A/B of the day's changes, cold, order-balanced, 4 runs each, load 4-9 (fc-ab-cold-1113.log), mean core-s: everything 21.1; arrival-order slots 23.5; two-pass check 24.0; one Info per package 22.7; no huge pages 29.0 (wall +1.8 s). kube-apiserver builds and passes its smoke test with the current fc (6.0 s warm without huge pages).
  1623	- 2026-11-13 23:49 UTC: fc's object writer gets a 128 kB buffer (cmd/internal/bio's is 4 kB: 98,000 write calls for MinIO's 420 MB of objects, counted with strace -c): kernel time 2.92 -> 2.65 s, mean of 4 order-balanced pairs on a saturated machine (wall not comparable there). Disk again: 4 GB free at 23:47 (fc-gocache 24 GB, 4.8 GB of this session's scratch objects, removed); clean fc-gocache when no batch runs.
  1624	- 2026-11-14 00:06 UTC: **M3**: the runtime's own tests pass wholly by fc after the builder's fixes were merged (print of hex/quoted types, stack check of self-recursive runtime functions, FuncPCABIInternal of an interface, closure names as stock's, go/defer without a wrapper) and one more skip (string(bytes) to strings.Index: the harness's conservative export data; the builder's worktree had no build/compile-esc, so its runs used the stock export data and passed). std 260 of 262; MinIO's own tests with this fc 45 of 46, cmd as stock.
  1625	
  1626	## 2026-11-14 00:08 UTC: M3 and M4
  1627	
  1628	Both milestones in one evening, and the build went from 6.8 s to 3.6-4.1 s cold on the way (at load 4-5; the idle
  1629	scoreboard is owed). The runtime through fc was the morning's open question; what closed it was three kinds of
  1630	thing the stock compiler does that the runtime's source relies on silently (no stack check in helpers it would
  1631	inline, BP after context switches, selectnbsend for a one-case select), each found by a test that hung or crashed
  1632	rather than by reading. The speed came from looking at per-package timestamps instead of totals: the tail of the
  1633	build was one package waiting in arrival order, and behind that a serial type check that go/types could be made
  1634	to split with 150 lines (declarations once, bodies in shards). After that the cores-busy profile is flat and the
  1635	build is CPU-bound, which changes what counts: every further gain is core-seconds (maps, object writing, dead
  1636	code), not scheduling.
  1637	What did the oracle number do since the last milestone? std wholly by fc 221 of 226 -> 260 of 262 (the list itself
  1638	grew by the 36 packages that had never been on it); MinIO's tests hold at 45 of 46 with the runtime now fc's;
  1639	core-s per MB of MinIO 0.41 -> 0.27. It did not stall. The blocker for the exit test is 0.3-0.8 s of wall, i.e.
  1640	3-5 core-s on 8 cores: M5 (objects in memory, ~2.7 core-s) is next, then dead code (~2).
  1641	Is the oracle the right one? For correctness yes, and it found six bugs today. For speed the cold number must be
  1642	taken on an idle machine; tonight's are honest about their load but not quotable for the demo.
  1643	- 2026-11-14 00:35 UTC: the scoreboard on an idle machine at last (00:26-00:34, cold, every binary checked): fc on 8 cores: buildifier 0.8 s, runsc 1.2, tailscaled 2.1, caddy 2.3, minio 3.9 (three more runs 3.7, 3.9, 4.2; warm 3.4-3.5), kube-apiserver 4.3, prometheus 9.3; at 4/2/1 cores in SCOREBOARD.md. Core-seconds grow with cores (MinIO 16.4 / 17.7 / 18.3 / 22.8 at 1/2/4/8; lscpu: 8 single-thread cores): unexplained, first item of the Now line. A goroutine census (runtime.Stack every 50 ms, not committed) during an idle build: about 6 of 8 slot holders running fc code; the others wait on asm children (which use the core), on rtMu inside typeSym (25 of 504 slot-samples plus the tail effect in big packages), and on imported packages' checks. The idle timeline's 5-6.5 'cores busy' counts fc's own threads only.
  1644	- 2026-11-14 00:55 UTC: **why fc's core-seconds grow with cores (16.4 at 1, 22.8 at 8): not contention; the memory hierarchy.** Ruled out with controls: (a) SMT: the vCPUs report L1/L2 shared in pairs, but an 8-wide register-only kernel runs 370 M iterations/s alone, 367 on a "pair", 357 on all eight: real cores; (b) locks and the scheduler: CPU profiles of the same warm MinIO build at -j 1 and -j 8 (user 14.2 -> 18.2 s, sys 2.1 -> 2.5): runtime lock/futex/scheduler/preemption samples 0.24 -> 0.42 s, sync.Map 0.62 -> 0.69; (c) the collector: FC_GOGC=100 costs +8 core-s and +0.9 s wall (the live heap is 3 GB of 7.5 allocated), FC_GOGC=400 +6: killed. What grows is everything, evenly: allocation x1.43 (2.38 -> 3.41 s), map probes and hashing x1.21 (3.45 -> 4.18), go/types x1.20, parser x1.29, fc's own code x1.12. One L3 of 32 MB for eight cores and fresh memory at 2.5 GB/s. So the remedy is touching less memory, not a lock: fewer bytes allocated, less dead work. Allocation profile (7.53 GB): flat; per-function Infos 0.61 GB, descriptors 1.11 GB cumulative, compileFunc 2.58 GB cumulative, bufio 0.17 (a 128 kB buffer per object), sha256.New 0.16 (content hashes; M5 removes them).
  1645	- 2026-11-14 00:55 UTC: duplicates are not where the dead work is: across MinIO's 1,312 objects 459,536 descriptor definitions for 421,295 distinct names (0.5 MB duplicated), function duplicates 0.6 MB. But of 28.9 MB of descriptors written the linker keeps 9.1, and of 81.7 MB of code 64.9. **Descriptors by demand** (lazyNamed; FC_EAGERTYPES=1 for the stock rule): in `fc build -link` no package writes its declared types unasked; the first package whose code refers to a type writes it, whichever package declares it, and its wrappers (one per program: wrapOwner). Alone this is small: 20,752 fewer descriptors (28.9 -> 27.8 MB), 2,501 fewer functions, objects 422 -> 414 MB: dead descriptors are mostly referenced by dead code and by each other. Checked: fclink's live set is the same by name but for per-package counters (409,378 vs 409,376 symbols; 121,928 functions, 64.9 MB text both); all seven corpus projects build cold and pass their checks; language corpus 23/23 mixed and wholly-fc. It is the substrate for functions by demand, whose kill test passes (IDEAS.md): the reference-following rule keeps 84% of the functions and 36% of the descriptor bytes. M5 (objects to fclink in process) is with an Opus builder in a worktree since 00:44.
  1646	- 2026-11-14 01:15 UTC: **functions by demand** (driver/fc/demand.go; FC_LAZYFUNCS=1, off by default until timed on a quiet machine). The back end follows references as it generates code: roots are main, the inits, linknamed functions and every function of the runtime's closure and of packages with assembly; funcSym/methodSym ask the declaring package for a function (a queue per package, a count of outstanding work per program; a package stays open until the program is quiet, holding no slot while it waits); a written descriptor asks for all its methods. MinIO: 158,189 -> 137,624 functions, 81.3 -> 71.2 MB of code, objects 413 -> 374 MB; fclink keeps the same program (121,928 functions, 64.9 MB of text, 409,373 vs 409,376 symbols: the differences are numbered per-package names and 5 gclocals), smoke passes, language corpus 23/23 mixed and wholly-fc, a race-detector build of caddy is clean. The rule's ceiling by fclink's flood was 132,136; the 5,500 above it are globals' types (every global is still defined, and its type's methods asked for) and the eager packages. CPU: profile pairs under load say user -1.5 and -1.9 s (of ~19); wall not measurable tonight (load 20-30 from the M5 builder's test runs). Three things found on the way: (1) abiWrappers took every function's symbol and so asked for all of them (funcSymDecl: the symbol without the request); (2) batches per package made the back end level-synchronous, and a goroutine per function spent a second growing stacks (runJob: parked goroutines; a function holds a slot only while it compiles); (3) with descriptors written by the first asker, one package (minio/cmd) wrote most of them under its rtMu and five of eight threads stood in that queue (goroutine census by SIGQUIT at 3 s). **rtMu is gone**: a descriptor or helper symbol belongs to the thread that claims it (rtClaim, LoadOrStore), a small lock guards the maps and lists and is never held across a write. That was item (1) of the Now line.
  1647	- 2026-11-14 01:18 UTC: which descriptors are written and dropped (MinIO, nm over the objects against fclink's live list; 407,145 written / 27.8 MB, 124,582 live / 9.6 MB): function types 91,982 written, 10,636 live; **pointers to unnamed function types 91,929 written, 2 live**; name data 160,206 / 69,759; named types and their pointers 40,050 / 33,400 (the part demand already fixed). Two causes, both stock behaviour that a whole-program build does not need: every type's descriptor writes the pointer type it weakly refers to (a weak reference keeps nothing alive; reflect finds a pointer type by name when the field is zero), and a method table writes each method's signature with its receiver, which nothing refers to. Both skipped in `fc build -link` (wholeProgram()): objects 413 -> 346 MB eager, 374 -> 312 MB with functions by demand; names defined 287,825 -> 110,804; the linked program is the same (live sets equal but for numbered names; 121,928 functions, types 9.1 MB; smoke passes both ways). CPU not yet timed: load 15-30 all hour (two builders).
  1648	- 2026-11-14 01:39 UTC: **the checker's results in the syntax nodes** (driver/gotypes/sbslots.go, driver/fc/slots.go). A quarter of the build's CPU was hash-map code (4.5 of 17.5 s of samples, attributed by stack: the checker recording Types/Uses/Defs/Selections 1.05 s, fc reading them back 1.17, go/types' scopes and method sets 1.0, fc's own caches 0.8), and the per-function Infos were the largest allocation (0.6 GB + 0.4 GB of growth). Now every expression node has a slot (go/ast patched in fc's own GOROOT copy, /home/exedev/targets/goroot-slots: an embedded SBSlot in the 23 expression types, an object in Ident, a selection in SelectorExpr; ast.Expr gains SB()), go/types fills it from the hooks it already has for types2 (recordTypeAndValueInSyntax was an empty function), and fc reads the node it is looking at (144 map reads rewritten mechanically; the four maps are not made at all). Order-balanced, 6 pairs, MinIO warm, load 3-10: user CPU 16.06 -> 14.67 s mean (best 15.53 -> 13.84), wall on the quiet runs 2.88-2.91 -> 2.59-2.73 s. Checked: FC_SLOTCHECK=1 keeps the maps as well and compares every node both ways: MinIO 10,035,524 nodes / 9,625,058 map entries, kube-apiserver 10,134,290 / 9,498,120, buildifier: 0 differences; fclink's live set equal to the map build's; language corpus 23/23 mixed and wholly-fc; smoke passes; go/types' own tests pass in the patched tree (go/ast's ExamplePrint and go/parser's TestIncompleteSelection print node structs and see the new fields). Also today's eager build without slots is 2.9 s warm where the morning's was 3.4: the descriptor cuts (01:18). Functions by demand measured in the same window: worse (wall 3.4 s, CPU above eager's): a function compiled long after its body was checked finds nothing in the cache. It stays off; the fix is to check a body where it is compiled. A side find: type:.eq.<sig> is compiled from whichever type asked first, so its stack map differs between packages' copies (harmless: each is right for its code; it explains the handful of gclocals that differ between builds).
  1649	- 2026-11-15 17:17 UTC: **a bump allocator for fc** (driver/rtbump: sbbump.go patched into the runtime of fc's GOROOT; `fc build` turns it on after turning the collector off; FC_NOBUMP=1 or -memprofile for the Go allocator). With no collector, every size class, span, bitmap and type header was bookkeeping for nobody: an allocation is now an add and a compare on the P's 4 MB chunk (from the hugetlb pool when SB_HUGE is set). The session that wrote it ended before it ran; it hung at start because a mutex word packs an M's address and counts on the heap's 1024-byte alignment plus malloc header for an M (lock_spinbit.go: mutexMMask, mutexMOffset): objects of an M's size get that address shape. MinIO warm, same binary, order B A A B, idle machine: 2224, 2168 ms / 13.73, 13.73 core-s with it; 2548, 2414 ms / 16.05, 15.37 without. **Cold, 8 cores, idle (17:14-17:17 UTC), every binary passing its check: buildifier 0.51 s, runsc 0.60, tailscaled 1.25, caddy 1.57, MinIO 2.38 (four more: 2.66, 2.55, 2.43, 2.43; 13.7-15.5 core-s; without bump 2.72 s / 16.3), kube-apiserver 2.76, prometheus 4.90.** MinIO is under the exit test's 3.3 s; what the exit test still lacks is MinIO's own tests rerun on this fc and the Bazel rows of the scoreboard. Language corpus 23/23 mixed and wholly-fc. (The previous scoreboard's MinIO 3.9 s predates slots, the descriptor cuts and M5's in-process link.)
  1650	- 2026-11-15 17:26 UTC: merged the two builder branches left from 11-14: tools/fc/wptest.sh and wpbatch.sh (a package's tests through the whole-program path: `fc build -plan` of the test binary, descriptors by demand, in-process link) and bzfc (tools/bazel: fc builds a Bazel go_binary from `bazel aquery`'s action graph; `fc build -X` reaches whichever linker). Bazel rows cold on 8 cores (17:18 UTC, binaries checked): buildifier 9.6 s / 42 core-s, of which Bazel's own start, extraction and analysis 6.1 s / 31 core-s, generators 3.0 s, fc 0.36 s; runsc 26.2 s / 135 core-s, of which Bazel 22.1 s / 112 core-s, 936 generator actions 3.2 s, fc 0.55 s. The plain-go rows of the same two projects: 0.47 and 0.65 s. So from Bazel the build is Bazel's analysis: that is the next critical path there, and it is not ours to shrink by compiling faster. The six merged worktrees are removed. A builder (Opus) has the two BuildBuddy rows.
  1651	- 2026-11-15 17:26 UTC: **fused check, an experiment (FC_FUSE=1, off)**: a declared function's body is checked by the thread about to generate its code (Pkg.infoFor -> ensureBody: one Bodies.Check per body), the initialization order after the functions (finishBodies). MinIO builds and passes its smoke test. Under load (MinIO's test batch and a builder running; user CPU, order-balanced, 6 runs each): eager 12.8 s, fused 13.2 s, kernel time +0.15 s with it: no locality gain to see, and the noise is 1 s. With functions by demand (137,631 functions instead of 158,192): eager 12.2, by demand 13.1, by demand and fused 13.0: by demand is not cheaper although it compiles 13% fewer functions, and the 11-14 explanation (a body checked long before it is compiled) is not the cause, since fusing does not change it. Verdict owed on a quiet machine; nothing claimed.
  1652	- 2026-11-15 17:28 UTC: **MinIO's own tests on today's fc, through the whole-program path** (tools/fc/wpbatch.sh: each test binary by `fc build -plan -link`: every package by fc, the bump allocator, checker results in the nodes, descriptors by demand, linked in process): 45 of 46 packages pass; cmd fails TestTrackingResponseWriter only, the test the stock compiler fails too (/home/exedev/targets/fc-minio-wp1.log, fc-minio-wp1-cmd.log). std the same way is running. The link is not where the idle cores are: under load the phase table said deadcode 172 ms wall for 233 ms of CPU, but timed by round (FCLINK_DEADSTAT=1) the flood is 52 ms for 424,000 symbols in 117 levels and the method rounds 5 ms; quiet, the phase is 61 ms. No change made.

exec
/bin/bash -lc 'nl -ba PLAN.md; nl -ba STATUS.md; nl -ba TRACE.md' in /home/exedev/strand-w
 succeeded in 0ms:
     1	# PLAN
     2	
     3	The campaign is chosen by the user. Sessions **do not edit** the Campaign, Exit test, Kill
     4	test or Rules sections; they append to Milestones and keep the Now line current. A session
     5	cannot switch campaigns. If you think the campaign is wrong, write the evidence in STATUS.md
     6	under "Decisions for Dan" and keep working on it.
     7	
     8	## Campaign 1 (from 2026-11-12): our own compiler for whole packages, fused
     9	
    10	The stock-compiler path is **frozen** at its 2026-11-12 result: MinIO cold 11.2 s median,
    11	kube-apiserver 19.8-21.3 s (configuration `13-noweakptr`; MORNING.md, TRACE.md). It is the
    12	baseline to beat and the per-package fallback. No more work goes into making it faster.
    13	
    14	Why this campaign: the 2026-11-12 CPU table says fusing the stock compiler's work removes at
    15	most about 27 of 74 core-seconds; what is left (~47 core-s, 5.9 s on 8 cores) is the stock
    16	compiler's own work. Two seconds needs the compiling itself to be about three times cheaper:
    17	whole packages compiled without the stock front end, with no export-IR round trip, no
    18	`obj.Prog` assembler path and no separate linker. `05-sbc` showed that taking single
    19	functions does not pay; whole packages leaving the stock compiler is the seam that does.
    20	
    21	How is yours to decide: whole-program type checking once in one address space (go/types or
    22	your own), a lowering of your own, direct emission (the baseline generator's knowledge of
    23	frames, stack maps, write barriers and the calling convention is reusable), and the F4 link
    24	spike in `explore/f4link` as the place code lands. Stock fallback per package for anything
    25	not yet handled.
    26	
    27	**The trace may stay flat or get worse for days during this campaign.** That is expected and
    28	not a reason to retreat. Progress is the oracle number below.
    29	
    30	## Oracle number (moves hourly; report it at every milestone)
    31	
    32	1. **Packages compiled entirely by our compiler, whose tests pass**: standard-library packages
    33	   first (their own `go test` with the binary built our way, plus the differential suite
    34	   against stock), then corpus packages (MinIO first). Report `N of M` per corpus project.
    35	2. **Core-seconds per MB of source** on the packages we compile, next to the stock baseline
    36	   mode's number on the same packages.
    37	
    38	## Kill test (written before work began)
    39	
    40	When the first 50 standard-library packages compile our way and pass, measure (2). If our
    41	compiler costs more than **one third** of the stock baseline mode's core-seconds on the same
    42	packages, with no measured path to one third, stop the campaign: write up why in JOURNAL.md
    43	and STATUS.md, and ask for the next campaign under "Decisions for Dan". Only this measurement
    44	ends the campaign early; time, usage and session ends never do.
    45	
    46	## Exit test
    47	
    48	A fresh cold MinIO build (CHARTER.md's cold rule) in **3.3 s wall or less on 8 cores** (an
    49	order of magnitude under the fair stock row's 32.5 s), binary passing MinIO's tests, with
    50	the corpus scoreboard showing the same technique on every corpus project.
    51	
    52	## Rules
    53	
    54	- Commit and push every working step, with tests for what it touches, so an abrupt end loses
    55	  nothing.
    56	- Use subagents as builders: split work into oracle-checked parts, one git worktree each.
    57	- The 8-vCPU VM is the machine (decided by the user). Scaling is reported at 1, 2, 4 and 8 cores.
    58	
    59	## Milestones
    60	
    61	M0. **Corpus scoreboard** (bounded: one day at most; then back to the compiler even if parts are
    62	    missing). Pin and fetch the corpus in CHARTER.md; one script builds each cold, stock and fair
    63	    stock, at 1, 2, 4 and 8 cores, and records wall clock, critical path and core-seconds in
    64	    `SCOREBOARD.md`. Bazel projects build from their Bazel DAG (`bazel aquery`), generated code
    65	    included. Our column starts as "stock fallback" and fills in as the campaign lands.
    66	M1. Our compiler compiles a first standard-library package end to end, linked into a binary that
    67	    passes that package's tests.
    68	M2. 50 standard-library packages: run the kill test.
    69	M3. The whole standard library and runtime our way.
    70	M4. MinIO entirely our way, passing its tests.
    71	M5. No separate linker: code emitted into the image (F4).
    72	M6. The exit test; the corpus scoreboard filled in.
    73	
    74	Log (append one line per milestone reached, with the oracle number):
    75	
    76	- M0, 2026-11-12: SCOREBOARD.md: every corpus project cold, stock and fair stock (and Bazel from its own DAG),
    77	  at 1/2/4/8 cores (`tools/scoreboard/run.sh`, `mkscoreboard.py`). One run per cell; fc builds overlapped on
    78	  cores 4-7 during parts of the 8-core pass (rerun `tools/scoreboard/run.sh . 8` on a quiet machine before quoting).
    79	- M1, 2026-11-12 19:57 UTC: `unicode/utf8` compiled entirely by fc (driver/fc) passes its own tests in a binary
    80	  whose other packages are stock-compiled (`tools/fc/test.sh unicode/utf8`; one allocation-count test skipped,
    81	  tests/fc/skips.txt). Oracle: standard library 1 package; corpus 0. Core-seconds per MB: not measured yet (M2).
    82	- M2, 2026-11-12 23:23 UTC: **205 standard-library packages** compiled entirely by fc pass their own tests in mixed mode
    83	  (tests/fc/passing.txt; 53 compile but fail, with Codex triaging; 91 have no tests), and all 21 packages of the
    84	  language corpus (tests/fc/lang). Corpus: MinIO builds and links in whole-program mode (1,283 packages by fc, the
    85	  runtime's 30 from stock) but does not run yet. **Kill test measured** (`tools/fc/cost.sh`, the 205 packages, 8.23 MB):
    86	  stock baseline mode 6.18 core-s (0.75 per MB); fc 2.80 core-s attributable (0.34 per MB), ratio **0.45**: above one
    87	  third. Not stopped, because the path is measured (JOURNAL): the profile is flat with named items, and on MinIO the
    88	  whole fc build is 26-27 core-s against the frozen path's 74.
    89	- M4 in part, 2026-11-13 03:43 UTC: **MinIO built by fc passes its smoke test, cold**: 1,283 packages by fc in one process,
    90	  the runtime's 29-30 packages by the stock compiler inside the timer, planning by the driver's own planner,
    91	  stock link: 7.7-8.7 s wall, 34-36 core-s on a machine two other agents were using (`tools/fc/cold.sh minio`;
    92	  frozen stock path 11.2 s, fair stock 29.5 s / 150 core-s). kube-apiserver the same way: 8.7-9.1 s, 42-43 core-s,
    93	  smoke passes (frozen path 19.8-21.3 s). buildifier, runsc, caddy and tailscaled build, link and run `--version`
    94	  (`tools/fc/corpus.sh`); prometheus stops in one generic instance. Not M4 yet: MinIO's unit tests have not been
    95	  run against fc-built packages, and the runtime is stock (M3).
    96	- M4 in part, 2026-11-13 05:00 UTC: **MinIO's own tests under fc: 45 of 46 packages pass; the 46th (cmd) fails one
    97	  test that fails with the stock compiler too** (package under test by fc; the all-packages-by-fc run,
    98	  `FC_ALL=1 tools/fc/test.sh`, is in /home/exedev/targets/fc-minio-all.log). std: 228 packages pass. Escape analysis
    99	  (driver/fc/escape*.go) and fclink (driver/fclink) are merged into main, not yet used by `fc build`.
   100	- M3 in part, 2026-11-13 10:40 UTC: **a program compiled entirely by fc, the runtime included, and linked by
   101	  fclink runs**: hello and three of tools/fclink/progs match stock output; MinIO built that way (1,313 fc objects,
   102	  no stock archive) passes its smoke test (`FC_RUNTIME=1`, off by default). Not M3: with an fc-built runtime the
   103	  collector loses live objects when it marks concurrently on more than two CPUs (deterministic reproduction:
   104	  reflect calls under allocation pressure); `FC_ALL=rt tools/fc/lang.sh` (test binaries wholly by fc) is 17-19 of 23.
   105	  SCOREBOARD.md's fc column is filled (cold, idle machine, fc + fclink, stock-compiled runtime): MinIO 6.8 s on 8
   106	  cores, before the two-pass front end (warm 5.5 -> 4.8 s).
   107	- M3 nearly, 2026-11-13 13:30 UTC: **std with test binaries built wholly by fc, runtime included, linked by fclink
   108	  (`FC_ALL=rt tools/fc/test.sh`): 221 of 226 packages pass their own tests**; the language corpus 23 of 23 in that
   109	  mode; MinIO and kube-apiserver built that way pass their smoke tests. The lost-object bug had two causes (JOURNAL
   110	  12:10): stack checks in helpers the stock compiler inlines, and BP after the runtime's context switches.
   111	- **M4, 2026-11-13 23:45 UTC: MinIO entirely our way passes its tests.** Every Go package by fc, the runtime included,
   112	  linked by fclink (`fc build`'s defaults now; no go command in the build; assembly by the stock assembler): MinIO's own
   113	  tests with wholly-fc test binaries: 45 of 46 packages pass and cmd fails one test that stock fails too
   114	  (fc-minio-rt2.log, fc-minio-cmd-flake.log); smoke test passes (25 of 25 starts). Cold, 8 cores, machine shared with a
   115	  builder (load 4-5): **3.6-4.1 s wall, 20-23 core-s** (this morning 6.8 s; frozen stock path 11.2 s; exit test 3.3 s).
   116	  What moved it: huge pages for fc's heap (-8 core-s), biggest-first slots, declarations checked once with bodies in
   117	  shards (go/types CheckDecls/Bodies), a function's type information in its own maps. The build is CPU-bound now
   118	  (6-7 of 8 cores busy from 0.2 s to the link).
   119	- M3 nearly, 2026-11-13 23:45 UTC: **std wholly by fc: 258 of the 262 packages with tests pass** (tests/fc/passing.txt;
   120	  skips with evidence in tests/fc/skips.txt). Left: runtime (9 tests) and runtime/pprof with a builder; runtime/cgo and
   121	  internal/runtime/cgobench need cgo (not buildable with stock either at CGO_ENABLED=0). Bugs found by the last 36:
   122	  one-case select through selectgo (tickers deadlocked), fclink's function table order with mixed alignment,
   123	  go:fipsinfo, two data races in fc.
   124	- **M3, 2026-11-14 00:08 UTC: the standard library and the runtime our way.** With every package of the test binary
   125	  compiled by fc, the runtime included, and linked by fclink (`FC_ALL=rt tools/fc/test.sh`): **260 of the 262 std
   126	  packages that have tests pass their own tests** (tests/fc/passing.txt), the runtime's own suite among them (284 s).
   127	  The two left need cgo (runtime/cgo, internal/runtime/cgobench: not buildable by stock either at CGO_ENABLED=0).
   128	  54 skip lines (tests/fc/skips.txt): allocation counts and inlining assertions (most fail with stock at -l too),
   129	  liveness waits, one environment skip. Language corpus 23 of 23. MinIO's own tests rerun with this fc: 45 of 46,
   130	  cmd as stock (fc-minio-rt3.log). Oracle 2: MinIO 20-23 core-s for 75.5 MB (0.27-0.30 per MB).
   131	
   132	Now (2026-11-15 17:30 UTC): **MinIO cold 2.4-2.7 s / 13.7-15.5 core-s on 8 cores, idle machine, smoke passing**
   133	(exit test: 3.3 s); corpus cold at 8 cores: buildifier 0.5 s, runsc 0.6, tailscaled 1.25, caddy 1.6, kube-apiserver 2.8,
   134	prometheus 4.9. What moved it since the last scoreboard (3.9 s): checker results in the syntax nodes, the descriptor
   135	cuts, the in-process link (M5's first step), and the bump allocator (JOURNAL 11-15 17:17). Left for the exit test:
   136	(1) **MinIO's own tests on this fc**: running through the whole-program path
   137	(`FC_TIMEOUT=2400 tools/fc/wpbatch.sh -j 2 -C /home/exedev/targets/minio <packages with tests>` ->
   138	/home/exedev/targets/fc-minio-wp1.log; 45 internal packages pass, cmd was still running at 17:26). When done: log it,
   139	then std the same way (`tools/fc/wpbatch.sh` over tests/fc/passing.txt) since the bump allocator, slots and the
   140	in-process link have not been through the std suite.
   141	(2) **The Bazel rows**: buildifier and runsc build from Bazel's DAG (tools/bazel/cold.sh; 9.6 s and 26 s cold, of which
   142	fc is 0.4-0.6 s and Bazel's own analysis 6 and 22 s). A builder (Opus, worktree, FC_TAG=bb) has bb-cli and bb-executor;
   143	merge when `tools/bazel/cold.sh <row> 8` gives rc 0 and the binary's check passes. Then the question the Bazel rows
   144	raise: the cold cost is Bazel's analysis, so read the DAG cheaper (what does aquery need that a cold server makes slow?).
   145	(3) **On a quiet machine** (nothing else running; check `uptime`): regenerate SCOREBOARD.md
   146	(`tools/scoreboard/ours.sh . 8 4 2 1`, then mkscoreboard.py) and TRACE.md; then verdicts owed, order-balanced:
   147	FC_FUSE=1 (fused check) and FC_LAZYFUNCS=1 (by demand) against the default (scratch: 6 pairs, user+sys and wall, MinIO
   148	warm). Under load neither showed a gain; by demand compiles 13% fewer functions and is not cheaper, cause unknown
   149	(profile both on a quiet machine before any more work on it).
   150	(4) Then toward 2.0 s: the profile is flat (code generation 40%, bodies check 15%, descriptors 12%, declarations 9%,
   151	parse 9%, link 6%); candidates with a mechanism: per-function scratch reuse (growslice+memmove 1.3 s of 14),
   152	demand()'s sync.Map load per reference (0.5 s), ast.Walk passes per function (0.9 s: findHeapVars and friends in one
   153	walk), NewUnit (0.6 s). tools/fc/timeline.py for where cores idle (the link is 280 ms).
   154	Housekeeping: `df -h /` before long test runs (13 GB free at 17:20); nr_hugepages was found at 4090 (a killed run) and
   155	set back to 0.
     1	# STATUS (at most ten lines; updated at every milestone and whenever Dan is needed)
     2	
     3	Campaign 1, 2026-11-15 17:30 UTC. **MinIO built entirely by fc and linked in process: cold 2.4-2.7 s on 8 cores, 13.7-15.5 core-s, idle machine, smoke test passing** (exit test 3.3 s; last scoreboard 3.9 s; frozen stock path 11.2 s; fair stock 29.5 s). Corpus cold on 8 cores: buildifier 0.5 s, runsc 0.6, tailscaled 1.25, caddy 1.6, kube-apiserver 2.8, prometheus 4.9; every binary passes its check.
     4	What moved it: type-checker results stored in the syntax nodes, dead descriptors skipped, objects handed to the linker in memory, and a bump allocator in fc's runtime (no collector runs, so nothing is freed).
     5	Not yet the exit test: MinIO's own tests are rerunning on this fc (45 of 46 packages pass so far, cmd running); SCOREBOARD.md is not regenerated; of the Bazel rows buildifier and runsc build from Bazel's DAG, BuildBuddy's two are with a builder.
     6	From Bazel's DAG the cold build is Bazel itself: buildifier 9.6 s (Bazel's start and analysis 6.1 s, fc 0.4 s), runsc 26 s (Bazel 22 s, fc 0.6 s).
     7	
     8	## Decisions for Dan
     9	
    10	1. The Bazel rows: cold, Bazel's own server start, repository extraction and analysis cost 6-22 s before fc sees the graph. Is reading the DAG without a cold Bazel server (e.g. an aquery result treated as a fetched input, like GOMODCACHE) within "cold" for the demo, or must Bazel start inside the timer as CHARTER says now? I keep it inside the timer until told otherwise.
    11	2. Huge pages (10 GB pool during the build), the 54 skipped tests, and the race detector's stack guard in an fc-built runtime: as in the previous STATUS; unchanged.
     1	# TRACE
     2	
     3	Generated by `tools/trace/mktrace.py` on 2026-11-12 15:49 UTC from the instrumented builds made by `tools/trace/stock_all.sh`. Do not edit by hand.
     4	
     5	Machine: 8 vCPUs (AMD EPYC 9554P under KVM), 31 GiB RAM; 2 s of wall clock is 16 core-seconds.
     6	
     7	Measured limits (`hw/limits.c`, `hw/limits.out.txt`): memcpy 1t 9.88 GB/s, memcpy 8t 43.09 GB/s, disk write 0.90 GB/s, disk read cold 3.11 GB/s, pagecache read 1t 4.99 GB/s, smallfiles cold 8t 0.27 GB/s, smallfiles warm 8t 2.11 GB/s.
     8	
     9	## minio: the build trace (driver `sb`, 8 CPUs, cold)
    10	
    11	Run `13-noweakptr` (driver at `5e509a4`, flags `GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout`). Result: PASS minio smoke (put/get/list/delete, 3 MB object round trip). Stock `go build` on the same machine, cold: 54,233 ms.
    12	
    13	Wall clock of 4 cold runs of this configuration, ms: 11,009, 11,344, 10,916, 10,794. The table is the first of them.
    14	
    15	| phase | ms | cores busy | core-s | bytes moved | floor ms | ms / floor | who does it |
    16	|---|---|---|---|---|---|---|---|
    17	| fetch (separate, not in total) | 6,675 | | | 1,120 MB | | | `go mod download` |
    18	| plan (package graph) + read inputs | 1,092 | 3.9 | 0.0 | 76 MB | 374 | 3x | driver; go.mod, build constraints |
    19	| &nbsp;&nbsp;of which lex + package-level declarations | 372 | 3.1 | 1.1 | 76 MB | 6 | 61x | Strand godecl |
    20	| head start | 1,409 | | | 0 kB | | | runtime closure compiled during the plan |
    21	| parse + resolve/types + lower/codegen | 8,470 | 7.6 | 62.9 | 526 MB | 268 | 32x | go tool compile/asm |
    22	| link | 1,071 | 1.9 | 1.7 | 159 MB | 16 | 67x | go tool link |
    23	| write output | 111 | 4.1 | 0.0 | 159 MB | 176 | 1x | driver |
    24	| **TOTAL** | **11,009** | **6.25** | **69** | read 223 MB, wrote 160 MB (disk) | **841** | **13x** | |
    25	| critical path (13 actions) | 3,429 | | | | | | |
    26	
    27	**Inside the compile row, by profile** (`tools/trace/phases.sh`: a CPU profile of every compiler process of run `08-huge-shard`, merged; 42 s of samples. Read the shares, not the seconds: the sampler misses about a third of the row's CPU. The exact per-phase table is further down):
    28	
    29	| compiler phase | core-s | share | the charter's row |
    30	|---|---|---|---|
    31	| lex + parse (syntax.Parse) | 0.3 | 0.8% | lex/parse |
    32	| type check (types2) | 4.8 | 11.4% | resolve/types |
    33	| typed tree -> export IR (write) | 3.0 | 7.2% | resolve/types |
    34	| export IR -> compiler IR (read), imports | 6.0 | 14.3% | resolve/types |
    35	| escape analysis, wrappers, runtime types, init | 5.6 | 13.3% | lower/codegen |
    36	| walk + build SSA | 4.5 | 10.8% | lower/codegen |
    37	| SSA passes (regalloc 0.5) | 2.2 | 5.2% | lower/codegen |
    38	| emit machine code (genssa, assembler) | 8.0 | 19.2% | lower/codegen |
    39	| write objects and export data | 3.6 | 8.5% | write objects |
    40	| not under any of these (scheduler, runtime start, flag parsing) | 3.9 | 9.2% | |
    41	
    42	Across all phases, 5.0 s is the allocator and 1.4 s is clearing memory. Lexing and parsing all of the build's Go is 0.34 core-s: 0.8% of the compiler's work.
    43	
    44	**The fair stock row.** Stock `go build`, cold, with the relaxations this build uses (`GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l' -ldflags='-s -w'`): **32,512 ms** (169 core-s). Plain stock in the same hour: 49,953 ms. This build: 11,009 ms. The gain to claim is against the fair row.
    45	
    46	**The materialization tax** (what the build pays for being separate processes that hand each other files; `tools/trace/tax.py`, driver counters):
    47	
    48	| | stock | fair stock | this build |
    49	|---|---|---|---|
    50	| wall ms, cold | 49,953 | 32,512 | 11,009 |
    51	| core-s | 257 | 169 | 69 |
    52	| tasks created, whole machine (processes + threads) | 11,404 | 10,585 | 6,380 |
    53	| tool processes started (exec) | 1,483 | 1,483 | 172 + 1,315 forked from one warm compiler |
    54	| page faults, whole machine | 11,923,241 | 11,007,650 | 1,343,950 |
    55	| intermediates written (archives, export data) | 1,899 MB | 1,383 MB | 618 MB |
    56	| intermediates read back (importers, linker, cache copy) | 2,731 MB | 1,996 MB | 1,143 MB |
    57	| of which export data | 107 MB | 91 MB |  |
    58	
    59	Stock writes every archive twice (work directory, then GOCACHE) to disk; this build writes each once to tmpfs. "Read back" counts the export data each importer maps and every archive the linker reads.
    60	
    61	**Where the compile row's CPU goes, exactly** (`tools/trace/cpuphases.sh`: every compiler process logs its own CPU time and page faults at each phase boundary; warm run, 1,315 compiler processes, 68.1 core-s in the row):
    62	
    63	| compiler phase | user s | sys s | page faults | what it is |
    64	|---|---|---|---|---|
    65	| be:compilefuncs | 24.3 | 1.7 | 111,536 | walk, baseline code generation (SSA for the rest), assembler: **compiling** |
    66	| fe:irread | 9.1 | 1.2 | 74,184 | build the compiler's IR by reading that back, plus imported bodies and types: **moving data** |
    67	| fe:typecheck | 7.7 | 1.6 | 132,859 | types2, including reading imported packages' export data on demand: compiling (+ moving data) |
    68	| fe:irwrite | 4.6 | 0.7 | 57,626 | serialize the checked package as export IR: **moving data** |
    69	| be:dumpobj | 2.8 | 0.6 | 6,788 | write the object file: **moving data** |
    70	| fe:earlyexport | 2.4 | 0.4 | 29,548 | write export data for importers: **moving data** |
    71	| fe:devirtualize-and-inline | 2.2 | 0.2 | 11,067 | inlining is off; what is left under this timer is method wrappers and small per-function passes: compiling |
    72	| fe:escapes | 2.1 | 0.2 | 7,394 | escape analysis: compiling |
    73	| fe:parse | 1.4 | 0.6 | 71,704 | read and parse the source files: compiling |
    74	| (before main) | 0.9 | 0.2 | 126,091 | exec, runtime start, package init: **process setup** |
    75	| fe:init | 0.8 | 0.3 | 134,437 | flags, universe: process setup |
    76	| fe:loadsys | 0.5 | 0.2 | 26,153 | declare the runtime's functions: process setup |
    77	| (exit, asm, pack) | 1.6 | 0.0 | 0 | process exit, the assembler, joining shard objects |
    78	
    79	Notes: plan + read: 1312 packages, 7531 Go files, 10 rounds; declarations: package clauses and import paths of every file, consumed by the plan; head start: 29 of its 29 packages adopted by the plan; their CPU is in the compile row; compile: bytes = object archives written to tmpfs; write output: the linker wrote the file in place; this row is the sync.
    80	
    81	```
    82	8 |▁▃▅▄▄▆▇█████████████████████████████████████▇█▇▅▄▄▇█▃▁▁▁▁▂▄▁|   cores busy; one column = 183 ms
    83	0 +------------------------------------------------------------+
    84	```
    85	
    86	The chain that ends the build (each action was the last thing the next one waited for):
    87	
    88	| package | action | start ms | ms until it released the next | waited for a CPU token, ms | Go source |
    89	|---|---|---|---|---|---|
    90	| `github.com/minio/minio/cmd` | compile | 7,842 | 1,716 | 0 | 5 MB |
    91	| `github.com/minio/minio` | link | 9,567 | 1,071 | 9,567 | 0 kB |
    92	| `github.com/prometheus/procfs` | compile | 2,003 | 167 | 12 | 297 kB |
    93	| `syscall` | compile+asm | 1,161 | 102 | 3 | 244 kB |
    94	| `os` | compile | 1,399 | 80 | 0 | 179 kB |
    95	| `internal/poll` | compile | 1,319 | 80 | 0 | 66 kB |
    96	| `fmt` | compile | 1,480 | 67 | 1 | 99 kB |
    97	| `time` | compile | 1,263 | 55 | 0 | 168 kB |
    98	
    99	### minio: every rung so far (8 CPUs, cold)
   100	
   101	Single cold runs. The host is shared: the same build has cost between 139 and 157 compile core-seconds at different times of the night, so differences under about 2 s between rows are not evidence; compare the phase columns.
   102	
   103	| run | wall ms | core-s | cores busy | plan | read | lex or declarations | compile | link | write | smoke test |
   104	|---|---|---|---|---|---|---|---|---|---|---|
   105	| stock `go build` | 54,233 | 282 | 5.21 | | | | | 3,251 | | |
   106	| `01-skeleton`  | 47,553 | 265 | 5.58 | 428 | 216 | 213 | 43,083 | 3,077 | 272 | pass |
   107	| `02-pipeline` (-compiler build/compile-sb -pipeline -backends 2000) | 39,885 | 273 | 6.84 | 623 | 299 | 207 | 35,221 | 2,925 | 283 | pass |
   108	| `03-relaxed` (GOGC=off -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 21,957 | 144 | 6.58 | 509 | 202 | 225 | 19,053 | 1,532 | 192 | pass |
   109	| `04-selfplan` (GOGC=off -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 22,111 | 147 | 6.65 | 663 | (in plan) | 210 | 19,365 | 1,634 | 183 | pass |
   110	| `05-sbc` (GOGC=off -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -sbc build/sbc -gcflags=-dwarf=false -l -ldflags=-s -w) | 24,016 | 156 | 6.48 | 924 | (in plan) | 245 | 19,976 | 1,618 | 202 | pass |
   111	| `06-baseline` (GOGC=off SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 16,259 | 92 | 5.65 | 814 | (in plan) | 245 | 13,459 | 1,425 | 297 | pass |
   112	| `07-baseline-float` (GOGC=off SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 16,403 | 91 | 5.58 | 743 | (in plan) | 276 | 13,684 | 1,404 | 302 | pass |
   113	| `08-huge-shard` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 13,262 | 80 | 6.06 | 932 | (in plan) | 256 | 10,438 | 1,313 | 282 | pass |
   114	| `09-forkserver` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote) | 15,616 | 97 | 6.18 | 686 | (in plan) | 233 | 12,911 | 1,397 | 277 | pass |
   115	| `10-onepass` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote) | 11,699 | 67 | 5.74 | 945 | (in plan) | 263 | 8,938 | 1,194 | 310 | pass |
   116	| `11-headstart` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart) | 13,813 | 86 | 6.22 | 1,013 | (in plan) | 442 | 11,069 | 1,191 | 276 | pass |
   117	| `12-directout` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout) | 11,531 | 71 | 6.16 | 1,048 | (in plan) | 367 | 8,853 | 1,227 | 94 | pass |
   118	| `13-noweakptr` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote -headstart -directout) | 11,009 | 69 | 6.25 | 1,092 | (in plan) | 372 | 8,470 | 1,071 | 111 | pass |
   119	
   120	## kube-apiserver: the build trace (driver `sb`, 8 CPUs, cold)
   121	
   122	Run `13-noweakptr` (driver at `5e509a4`, flags `GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout`). Result: PASS kube-apiserver smoke (/healthz ok; ConfigMap create/get/delete). Stock `go build` on the same machine, cold: 86,260 ms.
   123	
   124	Wall clock of 2 cold runs of this configuration, ms: 23,634, 20,536. The table is the first of them.
   125	
   126	| phase | ms | cores busy | core-s | bytes moved | floor ms | ms / floor | who does it |
   127	|---|---|---|---|---|---|---|---|
   128	| fetch (separate, not in total) | 0 (vendored) | | | | | | |
   129	| plan (package graph) + read inputs | 807 | 4.2 | 0.0 | 89 MB | 334 | 2x | driver; go.mod, build constraints |
   130	| &nbsp;&nbsp;of which lex + package-level declarations | 402 | 4.4 | 1.3 | 89 MB | 7 | 56x | Strand godecl |
   131	| head start | 5,570 | | | 0 kB | | | runtime closure compiled during the plan |
   132	| parse + resolve/types + lower/codegen | 20,591 | 7.7 | 151.9 | 1,043 MB | 318 | 65x | go tool compile/asm |
   133	| link | 1,666 | 1.6 | 2.5 | 182 MB | 28 | 59x | go tool link |
   134	| write output | 95 | 1.0 | 0.0 | 182 MB | 201 | 0x | driver |
   135	| **TOTAL** | **23,634** | **6.82** | **161** | read 201 MB, wrote 184 MB (disk) | **888** | **27x** | |
   136	| critical path (36 actions) | 8,984 | | | | | | |
   137	
   138	**The fair stock row.** Stock `go build`, cold, with the relaxations this build uses (`GOGC=off go build -trimpath -gcflags=all='-dwarf=false -l' -ldflags='-s -w'`): **51,151 ms** (329 core-s). Plain stock in the same hour: 77,091 ms. This build: 23,634 ms. The gain to claim is against the fair row.
   139	
   140	**The materialization tax** (what the build pays for being separate processes that hand each other files; `tools/trace/tax.py`, driver counters):
   141	
   142	| | stock | fair stock | this build |
   143	|---|---|---|---|
   144	| wall ms, cold | 77,091 | 51,151 | 23,634 |
   145	| core-s | 511 | 329 | 161 |
   146	| tasks created, whole machine (processes + threads) | 18,018 | 16,510 | 9,278 |
   147	| tool processes started (exec) | 2,180 | 2,180 | 102 + 2,081 forked from one warm compiler |
   148	| page faults, whole machine | 26,907,866 | 26,418,964 | 2,299,864 |
   149	| intermediates written (archives, export data) | 5,466 MB | 4,283 MB | 1,762 MB |
   150	| intermediates read back (importers, linker, cache copy) | 9,909 MB | 7,993 MB | 4,781 MB |
   151	| of which export data | 764 MB | 713 MB |  |
   152	
   153	Stock writes every archive twice (work directory, then GOCACHE) to disk; this build writes each once to tmpfs. "Read back" counts the export data each importer maps and every archive the linker reads.
   154	
   155	Notes: plan + read: 2079 packages, 8940 Go files, 11 rounds; declarations: package clauses and import paths of every file, consumed by the plan; head start: 29 of its 29 packages adopted by the plan; their CPU is in the compile row; compile: bytes = object archives written to tmpfs; write output: the linker wrote the file in place; this row is the sync.
   156	
   157	```
   158	8 |▄▄▆█████████████████████████████████████████████████▅▂▁▁▁▁▃▁|   cores busy; one column = 394 ms
   159	0 +------------------------------------------------------------+
   160	```
   161	
   162	The chain that ends the build (each action was the last thing the next one waited for):
   163	
   164	| package | action | start ms | ms until it released the next | waited for a CPU token, ms | Go source |
   165	|---|---|---|---|---|---|
   166	| `k8s.io/kubernetes/cmd/kube-apiserver` | link | 21,408 | 1,666 | 21,408 | 0 kB |
   167	| `k8s.io/kubernetes/pkg/controlplane/apiserver` | compile | 19,887 | 757 | 4 | 68 kB |
   168	| `net/http/internal/http2` | compile | 2,488 | 447 | 0 | 370 kB |
   169	| `k8s.io/kubernetes/cmd/kube-apiserver/app` | compile | 20,953 | 440 | 0 | 20 kB |
   170	| `net/http` | compile | 2,935 | 439 | 0 | 562 kB |
   171	| `k8s.io/client-go/informers/core/v1` | compile | 8,380 | 434 | 0 | 180 kB |
   172	| `crypto/internal/fips140/nistec` | compile+asm | 1,318 | 396 | 0 | 624 kB |
   173	| `k8s.io/client-go/kubernetes` | compile | 7,626 | 387 | 2 | 35 kB |
   174	
   175	### kube-apiserver: every rung so far (8 CPUs, cold)
   176	
   177	Single cold runs. The host is shared: the same build has cost between 139 and 157 compile core-seconds at different times of the night, so differences under about 2 s between rows are not evidence; compare the phase columns.
   178	
   179	| run | wall ms | core-s | cores busy | plan | read | lex or declarations | compile | link | write | smoke test |
   180	|---|---|---|---|---|---|---|---|---|---|---|
   181	| stock `go build` | 86,260 | 554 | 6.43 | | | | | 4,974 | | |
   182	| `03-relaxed` (GOGC=1000 -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 53,910 | 346 | 6.42 | 1,114 | 176 | 243 | 49,219 | 2,453 | 203 | pass |
   183	| `04-selfplan` (GOGC=1000 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 44,406 | 320 | 7.22 | 601 | (in plan) | 228 | 40,657 | 2,465 | 194 | pass |
   184	| `06-baseline` (GOGC=1000 SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 28,974 | 196 | 6.77 | 723 | (in plan) | 247 | 24,974 | 2,404 | 373 | pass |
   185	| `07-baseline-float` (GOGC=1000 SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 29,561 | 201 | 6.80 | 739 | (in plan) | 354 | 25,523 | 2,429 | 366 | pass |
   186	| `08-huge-shard` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -backends 200 -gcflags=-dwarf=false -l -ldflags=-s -w) | 24,090 | 158 | 6.56 | 609 | (in plan) | 260 | 20,521 | 2,119 | 334 | pass |
   187	| `09-forkserver` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote) | 24,517 | 162 | 6.60 | 622 | (in plan) | 224 | 21,031 | 2,031 | 330 | pass |
   188	| `10-onepass` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote) | 21,130 | 137 | 6.49 | 628 | (in plan) | 244 | 17,882 | 1,822 | 308 | pass |
   189	| `11-headstart` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart) | 23,800 | 154 | 6.46 | 827 | (in plan) | 420 | 20,110 | 1,983 | 308 | pass |
   190	| `12-directout` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout) | 20,097 | 135 | 6.70 | 758 | (in plan) | 378 | 16,998 | 1,810 | 104 | pass |
   191	| `13-noweakptr` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 200 -zygote -headstart -directout) | 23,634 | 161 | 6.82 | 807 | (in plan) | 402 | 20,591 | 1,666 | 95 | pass |
   192	
   193	# The stock toolchain (rung 0: the gap)
   194	
   195	## minio
   196	
   197	Commit `7aac2a2c5b7c`, `go1.27.3 linux/amd64`, `CGO_ENABLED=0`, `-trimpath`. 1,312 packages (264 from the standard library), 7,531 Go files, 75.5 MB and 2,379,958 lines of Go, 109 assembly files (5.4 MB), 20.1 MB embedded. Output binary: 146.8 MB.
   198	
   199	### Stock toolchain, cold, by CPU count
   200	
   201	| CPUs | wall ms | core-seconds (user+sys) | cores busy (avg) | link wall ms | disk read | disk written |
   202	|---|---|---|---|---|---|---|
   203	| 1 | 234,039 | 229 | 0.98 | 4,195 | 544 MB | 2,074 MB |
   204	| 2 | 121,704 | 220 | 1.80 | 3,424 | 634 MB | 1,938 MB |
   205	| 4 | 72,561 | 237 | 3.27 | 3,280 | 598 MB | 1,835 MB |
   206	| 8 | 54,233 | 282 | 5.21 | 3,251 | 504 MB | 1,651 MB |
   207	| 8, warm page cache | 49,937 | 262 | 5.25 | 3,128 | 293 MB | 1,598 MB |
   208	
   209	### The build trace (8 CPUs, cold)
   210	
   211	Wall clock is given where a phase owns a stretch of the build; inside the parallel compile stretch the phases interleave across 1,300+ processes, so they are given in core-seconds (the compiler's own `-bench` timers, summed over all packages, scaled to the cold run's compile CPU).
   212	
   213	| phase | wall ms | core-s | cores busy | bytes moved | floor ms | stock / floor |
   214	|---|---|---|---|---|---|---|
   215	| fetch (separate, not in total) | 6,675 | | | 1,120 MB on disk | | |
   216	| read inputs | 4,296 (cold minus warm) | | | 504 MB read from disk; sources are 101 MB | 374 | 11x |
   217	| load package graph (`go build` before the first compile) | 3,255 | | 1.8 | | | |
   218	| **compile stretch** (first compile to link start) | 47,383 | 267 | 5.9 | 81 MB source in, 957 MB of objects out | | |
   219	| &nbsp;&nbsp;process start (init, load runtime decls) | | 2.1 | | | 0 (not needed) | |
   220	| &nbsp;&nbsp;lex/parse + resolve/types | | 50.4 | | 76 MB of Go | 152 | 41x (core-s / 8 against the floor) |
   221	| &nbsp;&nbsp;inline + escape analysis | | 26.4 | | | 0 (not needed) | |
   222	| &nbsp;&nbsp;lower/codegen | | 169.0 | | 76 MB of Go | 122 | 173x (core-s / 8 against the floor) |
   223	| &nbsp;&nbsp;write objects | | 18.8 | | | 0 (not needed) | |
   224	| link | 3,251 | 4.8 | 1.7 | 147 MB binary | 3 | 954x |
   225	| write output | (inside link) | | | 147 MB; the build wrote 1,651 MB in all | 163 | |
   226	| after link (install, exit) | 344 | | | | | |
   227	| **TOTAL** | **54,233** | **282** | **5.21** | | **815** | **67x** |
   228	| critical path (longest dependency chain, 30 actions) | 38,503 | | | | | |
   229	
   230	The compiler's own timers, seconds of wall clock inside the compiler processes summed over all packages (`-bench` run, 8 CPUs, warm page cache): be:compilefuncs 92.1, fe:parse 50.4, fe:devirtualize-and-inline 19.3, be:dumpobj 18.8, fe:escapes 7.0, fe:loadsys 1.2, fe:init 0.9, fe:pgo-load-profile 0.0. Its `parse` timer covers lexing, parsing, type checking and building the IR; it does not split them.
   231	
   232	### Cores busy over time (8 CPUs, cold)
   233	
   234	```
   235	8 |▂▂▂▃▃▄▅██████▇▇█▇█▇▇█████▇█▇██████▇▆▇▇▆▄▂▄▅▆▄▄▃▂▂▂▂▂██▄▁▁▁▂▂|   one column = 904 ms
   236	0 +------------------------------------------------------------+   load graph ends 3,255 ms, link 50,638-53,889 ms
   237	```
   238	Share of wall clock by number of busy cores: 1: 19%, 2: 11%, 3: 3%, 4: 4%, 5: 3%, 6: 4%, 7: 21%, 8: 35%. Idle core-seconds: 151 of 434.
   239	
   240	### Critical path through the package graph (8 CPUs, cold)
   241	
   242	The longest chain of actions, each waiting for the one before: 38,503 ms of the 54,233 ms wall clock. With unlimited cores and this toolchain the build could not be shorter than this.
   243	
   244	| # | package | action | start ms | ms | waited for a core, ms | Go source |
   245	|---|---|---|---|---|---|---|
   246	| 5 | `runtime` | build | 3,783 | 1,853 | 0 | 3 MB |
   247	| 9 | `syscall` | build | 5,829 | 403 | 0 | 244 kB |
   248	| 13 | `os` | build | 6,806 | 474 | 0 | 179 kB |
   249	| 16 | `encoding/json/v2` | build | 7,683 | 1,173 | 0 | 237 kB |
   250	| 18 | `github.com/goccy/go-json/internal/encoder` | build | 21,205 | 602 | 0 | 161 kB |
   251	| 19 | `github.com/goccy/go-json/internal/encoder/vm_color_indent` | build | 21,816 | 2,599 | 0 | 147 kB |
   252	| 20 | `github.com/goccy/go-json/internal/encoder/vm_color` | build | 24,416 | 2,806 | 0 | 147 kB |
   253	| 21 | `github.com/goccy/go-json/internal/encoder/vm_indent` | build | 27,226 | 4,985 | 0 | 145 kB |
   254	| 22 | `github.com/goccy/go-json/internal/encoder/vm` | build | 32,213 | 4,751 | 0 | 145 kB |
   255	| 25 | `github.com/minio/madmin-go/v3` | build | 37,218 | 1,419 | 0 | 1 MB |
   256	| 26 | `github.com/minio/mc/cmd` | build | 38,666 | 2,901 | 0 | 2 MB |
   257	| 27 | `github.com/minio/console/api` | build | 41,570 | 424 | 0 | 285 kB |
   258	| 28 | `github.com/minio/minio/cmd` | build | 42,040 | 8,546 | 0 | 5 MB |
   259	| 30 | `github.com/minio/minio` | link | 50,638 | 3,251 | 0 | 1 kB |
   260	
   261	(30 actions on the path; the 14 longest shown.)
   262	
   263	### The ten most expensive actions (8 CPUs, cold)
   264	
   265	| package | action | wall ms | core-s | Go source |
   266	|---|---|---|---|---|
   267	| `github.com/minio/minio/cmd` | build | 8,546 | 26.5 | 5 MB |
   268	| `github.com/elastic/go-elasticsearch/v7/esapi` | build | 8,460 | 17.6 | 3 MB |
   269	| `github.com/goccy/go-json/internal/encoder/vm` | build | 4,751 | 6.7 | 145 kB |
   270	| `github.com/minio/mc/cmd` | build | 2,901 | 6.3 | 2 MB |
   271	| `github.com/minio/minio` | link | 3,251 | 4.8 | 1 kB |
   272	| `runtime` | build | 1,853 | 4.5 | 3 MB |
   273	| `github.com/minio/madmin-go/v3` | build | 1,419 | 3.9 | 1 MB |
   274	| `github.com/goccy/go-json/internal/encoder/vm_indent` | build | 4,985 | 3.8 | 145 kB |
   275	| `github.com/IBM/sarama` | build | 2,424 | 3.1 | 880 kB |
   276	| `github.com/envoyproxy/go-control-plane/envoy/config/core/v3` | build | 2,455 | 2.9 | 1 MB |
   277	
   278	## kube-apiserver
   279	
   280	Commit `2cb26b34ab73`, `go1.27.3 linux/amd64`, `CGO_ENABLED=0`, `-trimpath`. 2,079 packages (244 from the standard library), 8,940 Go files, 89.3 MB and 2,500,778 lines of Go, 62 assembly files (0.8 MB), 0.0 MB embedded. Output binary: 172.8 MB.
   281	
   282	### Stock toolchain, cold, by CPU count
   283	
   284	| CPUs | wall ms | core-seconds (user+sys) | cores busy (avg) | link wall ms | disk read | disk written |
   285	|---|---|---|---|---|---|---|
   286	| 1 | 451,576 | 448 | 0.99 | 6,101 | 457 MB | 5,703 MB |
   287	| 2 | 233,876 | 441 | 1.89 | 5,466 | 768 MB | 5,513 MB |
   288	| 4 | 128,596 | 461 | 3.59 | 4,762 | 434 MB | 5,228 MB |
   289	| 8 | 86,260 | 554 | 6.43 | 4,974 | 813 MB | 4,863 MB |
   290	| 8, warm page cache | 73,522 | 485 | 6.60 | 4,898 | 74 kB | 4,677 MB |
   291	
   292	### The build trace (8 CPUs, cold)
   293	
   294	Wall clock is given where a phase owns a stretch of the build; inside the parallel compile stretch the phases interleave across 1,300+ processes, so they are given in core-seconds (the compiler's own `-bench` timers, summed over all packages, scaled to the cold run's compile CPU).
   295	
   296	| phase | wall ms | core-s | cores busy | bytes moved | floor ms | stock / floor |
   297	|---|---|---|---|---|---|---|
   298	| fetch (separate, not in total) | 0 (vendored in the repository) | | | | | |
   299	| read inputs | 12,738 (cold minus warm) | | | 813 MB read from disk; sources are 90 MB | 334 | 38x |
   300	| load package graph (`go build` before the first compile) | 4,037 | | 1.5 | | | |
   301	| **compile stretch** (first compile to link start) | 76,515 | 528 | 7.3 | 90 MB source in, 2,739 MB of objects out | | |
   302	| &nbsp;&nbsp;process start (init, load runtime decls) | | 3.5 | | | 0 (not needed) | |
   303	| &nbsp;&nbsp;lex/parse + resolve/types | | 148.6 | | 89 MB of Go | 180 | 103x (core-s / 8 against the floor) |
   304	| &nbsp;&nbsp;inline + escape analysis | | 61.2 | | | 0 (not needed) | |
   305	| &nbsp;&nbsp;lower/codegen | | 226.3 | | 89 MB of Go | 144 | 196x (core-s / 8 against the floor) |
   306	| &nbsp;&nbsp;write objects | | 88.0 | | | 0 (not needed) | |
   307	| link | 4,974 | 7.2 | 1.6 | 173 MB binary | 4 | 1,240x |
   308	| write output | (inside link) | | | 173 MB; the build wrote 4,863 MB in all | 192 | |
   309	| after link (install, exit) | 734 | | | | | |
   310	| **TOTAL** | **86,260** | **554** | **6.43** | | **854** | **101x** |
   311	| critical path (longest dependency chain, 54 actions) | 62,744 | | | | | |
   312	
   313	The compiler's own timers, seconds of wall clock inside the compiler processes summed over all packages (`-bench` run, 8 CPUs, warm page cache): be:compilefuncs 192.9, fe:parse 148.6, be:dumpobj 88.0, fe:devirtualize-and-inline 47.2, fe:escapes 14.0, fe:loadsys 2.1, fe:init 1.5, fe:pgo-load-profile 0.0. Its `parse` timer covers lexing, parsing, type checking and building the IR; it does not split them.
   314	
   315	### Cores busy over time (8 CPUs, cold)
   316	
   317	```
   318	8 |▃▁▂▃▆▇███████████████████▇███▇█████████████████▇████▇▅▅▃▁▁▃▁|   one column = 1,438 ms
   319	0 +------------------------------------------------------------+   load graph ends 4,037 ms, link 80,551-85,526 ms
   320	```
   321	Share of wall clock by number of busy cores: 0: 0%, 1: 10%, 2: 3%, 3: 2%, 4: 1%, 5: 1%, 6: 1%, 7: 26%, 8: 56%. Idle core-seconds: 136 of 690.
   322	
   323	### Critical path through the package graph (8 CPUs, cold)
   324	
   325	The longest chain of actions, each waiting for the one before: 62,744 ms of the 86,260 ms wall clock. With unlimited cores and this toolchain the build could not be shorter than this.
   326	
   327	| # | package | action | start ms | ms | waited for a core, ms | Go source |
   328	|---|---|---|---|---|---|---|
   329	| 27 | `k8s.io/apimachinery/pkg/apis/meta/v1` | build | 16,532 | 2,565 | 0 | 527 kB |
   330	| 28 | `k8s.io/api/core/v1` | build | 19,675 | 6,910 | 66 | 3 MB |
   331	| 29 | `k8s.io/api/resource/v1` | build | 27,493 | 2,015 | 0 | 703 kB |
   332	| 37 | `k8s.io/client-go/informers/core/v1` | build | 37,139 | 1,897 | 0 | 180 kB |
   333	| 39 | `k8s.io/client-go/informers` | build | 43,240 | 3,141 | 0 | 53 kB |
   334	| 42 | `k8s.io/apiserver/pkg/admission/plugin/policy/generic` | build | 50,300 | 2,075 | 0 | 81 kB |
   335	| 44 | `k8s.io/apiserver/pkg/admission/plugin/policy/validating` | build | 52,436 | 2,476 | 0 | 61 kB |
   336	| 45 | `k8s.io/apiserver/pkg/admission/plugin/policy/mutating` | build | 54,912 | 2,551 | 0 | 28 kB |
   337	| 46 | `k8s.io/apiserver/pkg/server` | build | 57,465 | 2,913 | 0 | 175 kB |
   338	| 47 | `k8s.io/apiserver/pkg/server/options` | build | 60,450 | 2,800 | 0 | 164 kB |
   339	| 48 | `k8s.io/kubernetes/pkg/kubeapiserver/options` | build | 67,895 | 1,954 | 0 | 67 kB |
   340	| 49 | `k8s.io/kubernetes/pkg/controlplane/apiserver/options` | build | 69,849 | 1,970 | 0 | 26 kB |
   341	| 50 | `k8s.io/kubernetes/pkg/controlplane/apiserver` | build | 74,875 | 1,847 | 0 | 68 kB |
   342	| 54 | `k8s.io/kubernetes/cmd/kube-apiserver` | link | 80,551 | 4,974 | 0 | 1 kB |
   343	
   344	(54 actions on the path; the 14 longest shown.)
   345	
   346	### The ten most expensive actions (8 CPUs, cold)
   347	
   348	| package | action | wall ms | core-s | Go source |
   349	|---|---|---|---|---|
   350	| `k8s.io/api/core/v1` | build | 6,910 | 7.9 | 3 MB |
   351	| `k8s.io/kubernetes/cmd/kube-apiserver` | link | 4,974 | 7.2 | 1 kB |
   352	| `k8s.io/kubernetes/pkg/generated/openapi` | build | 2,360 | 5.5 | 4 MB |
   353	| `cel.dev/cel-go/common/types` | build | 4,375 | 4.8 | 330 kB |
   354	| `k8s.io/kubernetes/pkg/apis/core/validation` | build | 3,515 | 4.5 | 486 kB |
   355	| `runtime` | build | 1,720 | 4.2 | 3 MB |
   356	| `k8s.io/client-go/kubernetes/typed/core/v1/fake` | build | 3,035 | 3.3 | 48 kB |
   357	| `k8s.io/apiserver/pkg/server` | build | 2,913 | 3.1 | 175 kB |
   358	| `k8s.io/apiextensions-apiserver/pkg/apiserver` | build | 3,080 | 3.0 | 91 kB |
   359	| `k8s.io/kubernetes/pkg/apis/core/v1` | build | 2,518 | 2.9 | 655 kB |
   360	
   361	## How the floors are computed
   362	
   363	A floor is the larger of (bytes moved / measured bandwidth of the level they move through) and (bytes x modelled ticks per byte / (8 cores x 3.095e9 ticks/s)). The bandwidths are measured. The ticks per byte are budgets for a data-parallel implementation, set in `tools/trace/mktrace.py`: lex 2, parse 8, types 40, codegen 40. They are the weakest numbers in this file. The link floor is one pass over the objects in and the binary out at memory bandwidth: a copy floor, which says nothing about symbol resolution or relocation (no model for those yet). For scale: the Strand Go lexer measures 15 ticks/byte today (GOLEX.md), and the stock toolchain spends about 11,578 ticks per source byte end to end on MinIO.
   364	
   365	## Does 2 seconds fit on this machine?
   366	
   367	By the floors above, MinIO's irreducible work is about 6.5 core-seconds (815 ms on 8 cores), inside the 16 core-seconds that 2 s on this VM allows, so physics does not rule the target out here. The stock toolchain does 282 core-seconds of work for the same build: with it doing the work, 2 s would need 141 perfectly used cores, and its dependency chain (38,503 ms) would still be in the way. So the gap is 18x in work done and 19x in the critical path; both have to go.
   368	

exec
/bin/bash -lc "sed -n '46,115p' TRACE.md; rg -n -i 'session|time left|usage|budget|remaining|ran out|next session|deadline' JOURNAL.md | tail -n 35; rg -n 'memobj|WriteObj|WriteObject|bytes.Buffer|fclink|object' driver/fc/build.go | tail -n 40" in /home/exedev/strand-w
 succeeded in 0ms:
**The materialization tax** (what the build pays for being separate processes that hand each other files; `tools/trace/tax.py`, driver counters):

| | stock | fair stock | this build |
|---|---|---|---|
| wall ms, cold | 49,953 | 32,512 | 11,009 |
| core-s | 257 | 169 | 69 |
| tasks created, whole machine (processes + threads) | 11,404 | 10,585 | 6,380 |
| tool processes started (exec) | 1,483 | 1,483 | 172 + 1,315 forked from one warm compiler |
| page faults, whole machine | 11,923,241 | 11,007,650 | 1,343,950 |
| intermediates written (archives, export data) | 1,899 MB | 1,383 MB | 618 MB |
| intermediates read back (importers, linker, cache copy) | 2,731 MB | 1,996 MB | 1,143 MB |
| of which export data | 107 MB | 91 MB |  |

Stock writes every archive twice (work directory, then GOCACHE) to disk; this build writes each once to tmpfs. "Read back" counts the export data each importer maps and every archive the linker reads.

**Where the compile row's CPU goes, exactly** (`tools/trace/cpuphases.sh`: every compiler process logs its own CPU time and page faults at each phase boundary; warm run, 1,315 compiler processes, 68.1 core-s in the row):

| compiler phase | user s | sys s | page faults | what it is |
|---|---|---|---|---|
| be:compilefuncs | 24.3 | 1.7 | 111,536 | walk, baseline code generation (SSA for the rest), assembler: **compiling** |
| fe:irread | 9.1 | 1.2 | 74,184 | build the compiler's IR by reading that back, plus imported bodies and types: **moving data** |
| fe:typecheck | 7.7 | 1.6 | 132,859 | types2, including reading imported packages' export data on demand: compiling (+ moving data) |
| fe:irwrite | 4.6 | 0.7 | 57,626 | serialize the checked package as export IR: **moving data** |
| be:dumpobj | 2.8 | 0.6 | 6,788 | write the object file: **moving data** |
| fe:earlyexport | 2.4 | 0.4 | 29,548 | write export data for importers: **moving data** |
| fe:devirtualize-and-inline | 2.2 | 0.2 | 11,067 | inlining is off; what is left under this timer is method wrappers and small per-function passes: compiling |
| fe:escapes | 2.1 | 0.2 | 7,394 | escape analysis: compiling |
| fe:parse | 1.4 | 0.6 | 71,704 | read and parse the source files: compiling |
| (before main) | 0.9 | 0.2 | 126,091 | exec, runtime start, package init: **process setup** |
| fe:init | 0.8 | 0.3 | 134,437 | flags, universe: process setup |
| fe:loadsys | 0.5 | 0.2 | 26,153 | declare the runtime's functions: process setup |
| (exit, asm, pack) | 1.6 | 0.0 | 0 | process exit, the assembler, joining shard objects |

Notes: plan + read: 1312 packages, 7531 Go files, 10 rounds; declarations: package clauses and import paths of every file, consumed by the plan; head start: 29 of its 29 packages adopted by the plan; their CPU is in the compile row; compile: bytes = object archives written to tmpfs; write output: the linker wrote the file in place; this row is the sync.

```
8 |▁▃▅▄▄▆▇█████████████████████████████████████▇█▇▅▄▄▇█▃▁▁▁▁▂▄▁|   cores busy; one column = 183 ms
0 +------------------------------------------------------------+
```

The chain that ends the build (each action was the last thing the next one waited for):

| package | action | start ms | ms until it released the next | waited for a CPU token, ms | Go source |
|---|---|---|---|---|---|
| `github.com/minio/minio/cmd` | compile | 7,842 | 1,716 | 0 | 5 MB |
| `github.com/minio/minio` | link | 9,567 | 1,071 | 9,567 | 0 kB |
| `github.com/prometheus/procfs` | compile | 2,003 | 167 | 12 | 297 kB |
| `syscall` | compile+asm | 1,161 | 102 | 3 | 244 kB |
| `os` | compile | 1,399 | 80 | 0 | 179 kB |
| `internal/poll` | compile | 1,319 | 80 | 0 | 66 kB |
| `fmt` | compile | 1,480 | 67 | 1 | 99 kB |
| `time` | compile | 1,263 | 55 | 0 | 168 kB |

### minio: every rung so far (8 CPUs, cold)

Single cold runs. The host is shared: the same build has cost between 139 and 157 compile core-seconds at different times of the night, so differences under about 2 s between rows are not evidence; compare the phase columns.

| run | wall ms | core-s | cores busy | plan | read | lex or declarations | compile | link | write | smoke test |
|---|---|---|---|---|---|---|---|---|---|---|
| stock `go build` | 54,233 | 282 | 5.21 | | | | | 3,251 | | |
| `01-skeleton`  | 47,553 | 265 | 5.58 | 428 | 216 | 213 | 43,083 | 3,077 | 272 | pass |
| `02-pipeline` (-compiler build/compile-sb -pipeline -backends 2000) | 39,885 | 273 | 6.84 | 623 | 299 | 207 | 35,221 | 2,925 | 283 | pass |
| `03-relaxed` (GOGC=off -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 21,957 | 144 | 6.58 | 509 | 202 | 225 | 19,053 | 1,532 | 192 | pass |
| `04-selfplan` (GOGC=off -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 22,111 | 147 | 6.65 | 663 | (in plan) | 210 | 19,365 | 1,634 | 183 | pass |
| `05-sbc` (GOGC=off -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -sbc build/sbc -gcflags=-dwarf=false -l -ldflags=-s -w) | 24,016 | 156 | 6.48 | 924 | (in plan) | 245 | 19,976 | 1,618 | 202 | pass |
| `06-baseline` (GOGC=off SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 16,259 | 92 | 5.65 | 814 | (in plan) | 245 | 13,459 | 1,425 | 297 | pass |
| `07-baseline-float` (GOGC=off SB_BASELINE=1 -plan=self -decl build/godecl -compiler build/compile-sb -pipeline -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 16,403 | 91 | 5.58 | 743 | (in plan) | 276 | 13,684 | 1,404 | 302 | pass |
| `08-huge-shard` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -backends 2000 -gcflags=-dwarf=false -l -ldflags=-s -w) | 13,262 | 80 | 6.06 | 932 | (in plan) | 256 | 10,438 | 1,313 | 282 | pass |
| `09-forkserver` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote) | 15,616 | 97 | 6.18 | 686 | (in plan) | 233 | 12,911 | 1,397 | 277 | pass |
| `10-onepass` (GOGC=off SB_BASELINE=1 SB_HUGE=1 -plan=self -decl build/godecl -compiler build/compile-sb -linker build/link-sb -pipeline -shard 3000000 -gcflags=-dwarf=false -l -ldflags=-s -w -backends 2000 -zygote) | 11,699 | 67 | 5.74 | 945 | (in plan) | 263 | 8,938 | 1,194 | 310 | pass |
33:4. *The first 3.3 s* is `go build` loading the package graph on about 1.8 cores: by itself over the 2 s budget.
37:ruled out by physics. The ticks-per-byte budgets in that model (lex 2, parse 8, types 40, codegen 40) are
110:## 2026-11-11 23:02 UTC — iteration 4: rung 3, less work (the budgeted optimization iteration)
172:at the 22 GB limit of its scope (the guard worked; the session survived); with `GOGC=1000` and at most
197:of the whole 2 s budget) followed by a separate read of the same files. Rung 4: the driver finds the
236:That is a pure optimization iteration; the last one was iteration 4, so the budget allows it. After it:
240:## 2026-11-11 23:45 UTC — iteration 8: optimization of the compile row (budgeted). Three negatives.
262:floor: 139 core-seconds / 8 = 17.4 s, and the stretch takes 19 s. Every remaining cheap lever I could
312:the checker: code generation at about 100 ns per byte of machine code (16 core-s budget minus the
428:**Scope check.** Most ambitious: the remaining 20% of functions (closures, defer, append, type
605:- The correctness reviewer's session died of a stalled model request after five minutes. Fired again.
612:What I do instead: (1) the remaining kernel and start-up time (1.7 M faults left, 1,312 process
625:correctness reviewer's file holds only a session id). The 03:20 divergence round survived in IDEAS.md and
641:guess with a measurement: the patched compiler logs its own rusage at every phase boundary
677:  rusage come back through the zygote's wait4.
701:been built since, and usage is the binding limit. It runs next iteration, when the lenses can see the
743:**The consumed change: one assembler pass instead of 3.3** (the budgeted optimization iteration; the
815:core-s of capacity in a build bound by 61 core-s of work. The remaining idle time at the head is the
884:LINKING.md predates everything measured today. The next session should start there with the numbers
885:in this entry. For the rest of this session: keep MORNING.md exact, and stop spending usage on steps
924:badly in the last stretch of a session. NEXT.md has them in order with the numbers to design against.
1079:and several of them carry real risk; the session's remaining value is in the report being exact. The
1173:**Scope check.** Most ambitious: the same two rungs. For the remainder of this session I am running
1195:**Why.** The skeptic has said four times that the link "floor" in TRACE.md (17 ms) is a copy budget
1236:**Scope check.** This was the spike budget (none used today until now). Most ambitious next is to
1238:session starts from, and it changes what F4's first step is.
1286:iteration. No time left in this session for a second slice. NEXT.md is corrected.
1360:where the next session should start; the measurements say it is worth it and roughly what it costs.
1433:## 2026-11-12 20:55 UTC — last soak; end of session
1441:MORNING.md is current; NEXT.md starts the next session at F4 with `explore/f4link`.
1451:session's diff, with the ten local-type methods whose names the two lists probably spell differently.
1623:- 2026-11-13 23:49 UTC: fc's object writer gets a 128 kB buffer (cmd/internal/bio's is 4 kB: 98,000 write calls for MinIO's 420 MB of objects, counted with strace -c): kernel time 2.92 -> 2.65 s, mean of 4 order-balanced pairs on a saturated machine (wall not comparable there). Disk again: 4 GB free at 23:47 (fc-gocache 24 GB, 4.8 GB of this session's scratch objects, removed); clean fc-gocache when no batch runs.
1649:- 2026-11-15 17:17 UTC: **a bump allocator for fc** (driver/rtbump: sbbump.go patched into the runtime of fc's GOROOT; `fc build` turns it on after turning the collector off; FC_NOBUMP=1 or -memprofile for the Go allocator). With no collector, every size class, span, bitmap and type header was bookkeeping for nobody: an allocation is now an add and a compare on the P's 4 MB chunk (from the hugetlb pool when SB_HUGE is set). The session that wrote it ended before it ran; it hung at start because a mutex word packs an M's address and counts on the heap's 1024-byte alignment plus malloc header for an M (lock_spinbit.go: mutexMMask, mutexMOffset): objects of an M's size get that address shape. MinIO warm, same binary, order B A A B, idle machine: 2224, 2168 ms / 13.73, 13.73 core-s with it; 2548, 2414 ms / 16.05, 15.37 without. **Cold, 8 cores, idle (17:14-17:17 UTC), every binary passing its check: buildifier 0.51 s, runsc 0.60, tailscaled 1.25, caddy 1.57, MinIO 2.38 (four more: 2.66, 2.55, 2.43, 2.43; 13.7-15.5 core-s; without bump 2.72 s / 16.3), kube-apiserver 2.76, prometheus 4.90.** MinIO is under the exit test's 3.3 s; what the exit test still lacks is MinIO's own tests rerun on this fc and the Bazel rows of the scoreboard. Language corpus 23/23 mixed and wholly-fc. (The previous scoreboard's MinIO 3.9 s predates slots, the descriptor cuts and M5's in-process link.)
2:// compiles the requested packages in parallel, each to its own object.
31:	"cmd/internal/fclinklib"
51:	out := fs.String("o", "", "output directory for the objects")
53:	stats := fs.String("stats", "", "write a line per package: path, source bytes, parse ms, check ms, compile ms, object bytes, result,\nthen ms after the start: check done, back end start, back end end; and ms writing the object")
98:	// fclink: in this process, the packages handed over in memory (fclinklib.NewUnit) instead of objects;
99:	// with FC_OBJFILES=1 the fclink command on written objects, if there is one
263:		startMS, endMS, writeMS float64 // back end: start and end, ms after the program's start; of that, writing the object (or making the unit)
266:		unit                    *fclinklib.Unit // in-process link: the package's symbols
267:		asmObjs                 []string        // in-process link: the assembler's objects
382:			// the linker loads a package only if some loaded object says it imports it
396:				// no object: the linker reads the package's symbols where they are
408:				results[i].unit = fclinklib.NewUnit(c.ctxt)
412:					// the object and the assembler's objects go into one archive
496:			var inputs, mainIn []fclinklib.Input
505:					inputs = append(inputs, fclinklib.Input{Pkg: path, File: p.Export})
511:					in := []fclinklib.Input{{Pkg: path, Unit: r.unit}}
513:						in = append(in, fclinklib.Input{Pkg: path, File: o})
523:			fclinklib.Link(args, append(inputs, mainIn...))
524:			fmt.Printf("link (fclink in process): %d fc units, %d stock archives, %.0f ms\n", len(targets), len(stock), float64(time.Since(tl).Milliseconds()))
546:				// the campaign's linker (driver/fclink) instead of the stock one
549:				linker = "fclink"
555:			fmt.Printf("link (%s): %d fc objects, %d stock archives, %.0f ms\n", linker, len(targets), len(stock), float64(time.Since(tl).Milliseconds()))
606:// stockFingerprint: the fingerprint in the header of a stock archive's Go object.
660:// writePkgArchive packs object files into an archive the linker accepts as a package.

codex
The main unaddressed barrier is now before compilation: cold Bazel startup and analysis cost 6–22 seconds. TRACE.md describes the older pipeline; current Go objects already pass to the linker in memory, so I’ll distinguish the historical materialization cost from what remains today.
exec
/bin/bash -lc "nl -ba JOURNAL.md | sed -n '1188,1242p;1276,1288p;1430,1445p'; nl -ba driver/fc/build.go | sed -n '95,108p;378,414p;490,526p'; rg -n 'Write|Read|asmObjs|exec.Command' driver/fc/asm.go; nl -ba TRACE.md | sed -n '48,77p'" in /home/exedev/strand-w
 succeeded in 0ms:
  1188	  twice): old 22.79, 24.12, 22.72, 22.33 s (mean 22.99; compile 144.0 core-s; link + write 2,356 ms;
  1189	  objects 1,323 MB); new 20.33, 23.19, 21.02, 19.85 s (mean 21.10; 133.7 core-s; 1,802 ms; 1,043 MB).
  1190	  **-1.9 s (8%), -10 compile core-s, -0.55 s of link + write.** Same shape as MinIO's -1.4 s (11%):
  1191	  the comparison point is not overfitted to.
  1192	
  1193	## 2026-11-12 17:10 UTC — iteration 25 (spike): the core of a link on MinIO's real objects, done the data-parallel way: 0.26 s
  1194	
  1195	**Why.** The skeptic has said four times that the link "floor" in TRACE.md (17 ms) is a copy budget
  1196	with no model of symbol resolution or relocation, and the link is the tail of the build. F4 has no
  1197	measured number. This spike makes one. One iteration, in `explore/f4link` (270 lines of Go, built
  1198	inside the Go tree for `cmd/internal/goobj`; `explore/f4link/mk.sh`).
  1199	
  1200	**What it does.** Reads the `importcfg.link` of a real build and, on all cores: maps every archive and
  1201	finds the Go objects; enters every definition that is found by name or by content hash into a sharded
  1202	table (lowest object wins, so the result does not depend on thread timing); resolves every symbol slot
  1203	of every object to its definition; lays every owned definition out by prefix sums (per object, then
  1204	over objects); copies the data into one image and applies every relocation.
  1205	**What it does not do:** dead-code elimination, pclntab, the symbol table, ELF, or the semantics of
  1206	relocation types (each writes target + addend). It is not a linker and produces no program.
  1207	
  1208	**Measured** (MinIO, 1,312 archives, 1,424 objects, 501 MB; 8 cores; ms):
  1209	
  1210	    map archives, find objects            6-12
  1211	    claim names and content hashes      132-189    2.59 M claims, 960 k distinct
  1212	    resolve every symbol slot              62-65    2.72 M slots
  1213	    layout by prefix sums                      4    213 MB image (everything, no dead code removed)
  1214	    copy data, apply relocations           34-41    180 MB, 3.43 M relocations
  1215	    total                              about 260    (one core: 1,270)
  1216	
  1217	The stock linker on the same objects: 1,130 ms, of which loading and resolving is 440, dead code 246,
  1218	copying and relocating (asmb + dodata) 230, pclntab 114.
  1219	
  1220	**What the numbers say for F4.**
  1221	- Layout and relocation, the parts LINKING.md calls Strand-shaped, are 40 ms together. They are not the
  1222	  problem and never were.
  1223	- **Resolution is the cost: about 200 of the 260 ms, and it is hashing.** The symbol census explains
  1224	  it: 419 k symbols are found by package index (a table read), 300 k are defined by name, 121 k are
  1225	  references by name, and **1.84 M are content-addressed** (per-function pc tables, stack maps, string
  1226	  constants: things the compiler emits as separate little symbols so the linker can deduplicate them by
  1227	  a 32-byte hash). Two thirds of everything the linker resolves exists only to be deduplicated.
  1228	- So F4's first slice is not "a faster relocation pass". It is: the compilers stop emitting pc tables
  1229	  and stack maps as content-addressed symbols (write them into the function's own record, or dedup
  1230	  them per package where almost all the duplication is), and hand out dense ids for what is left.
  1231	  Then resolution is the 62 ms pass, and a link of this shape is under 0.15 s plus dead code and
  1232	  pclntab, which are the next two things to measure the same way.
  1233	- The dead-code flood (246 ms in stock) is the one serial algorithm left; as a frontier-parallel
  1234	  graph traversal over 3.4 M edges it should be tens of ms, untested.
  1235	
  1236	**Scope check.** This was the spike budget (none used today until now). Most ambitious next is to
  1237	make this consumed: emit an image that runs. Not in the hour left; the measurement is what the next
  1238	session starts from, and it changes what F4's first step is.
  1239	
  1240	## 2026-11-12 17:40 UTC — iteration 26: a consumed F4 slice tried and reverted (plain pc tables), and how far to trust whole-build CPU numbers
  1241	
  1242	**Trace.** Unchanged (`13-noweakptr`).
  1276	mechanism plus a count (bytes assembled, descriptors emitted); (2) one package, min of 15, strict
  1277	alternation; (3) serial rows of the build (link, write), which are quiet; (4) whole-build wall clock
  1278	over many builds (the 60-build soak); and only then (5) whole-build compile core-seconds.
  1279	Re-reading today's claims by that order: the assembler (mechanism: 3.3x fewer bytes assembled;
  1280	single package 8.5%) and the link + write rows (-368 ms) are solid; the descriptors' -120 ms of link
  1281	is solid and its 1.6 core-s is plausible but unproven; **the day's "-7.6 compile core-s" is uncertain
  1282	by several core-s, while its wall-clock difference (13.23 -> 11.80 s, six builds each, one overlap in
  1283	twelve) and the soak medians (MinIO 11.2 s, kube 19.8 s) are the numbers to quote.**
  1284	
  1285	**Scope check.** Most ambitious: F4 consumed. Tried its smallest slice; it lost; reverted within the
  1286	iteration. No time left in this session for a second slice. NEXT.md is corrected.
  1287	
  1288	## 2026-11-12 18:00 UTC — iteration 27: the link spike with a real resolution table: 0.16 s
  1430	So from above the flood is complete to within one identifiable case (assembly tables). What F4's
  1431	reachability still needs is the method rule, to remove the 10,919.
  1432	
  1433	## 2026-11-12 20:55 UTC — last soak; end of session
  1434	
  1435	Seventy more cold MinIO builds, each smoke-tested: 70 of 70; wall min 10.6 s, median 11.2 s, max
  1436	13.1 s (the same median as the run of sixty). Totals for the final configuration: MinIO 150 of 150,
  1437	kube-apiserver 56 of 56; no hang, no failed fork, no failed smoke test.
  1438	
  1439	State at the end: configuration `13-noweakptr`; MinIO 13.2 -> 11.8 s against last night's
  1440	configuration (order-balanced), median 11.2 s over 130 soak builds; kube-apiserver 23.0 -> 21.1 s.
  1441	MORNING.md is current; NEXT.md starts the next session at F4 with `explore/f4link`.
  1442	
  1443	## 2026-11-12 21:15 UTC — iteration 29 (short): one hypothesis about the 46, wrong
  1444	
  1445	Inbox empty. Scope check: the most ambitious thing in twenty minutes is one more step on the F4
    95				sbBumpEnable(26 << 30)
    96			}
    97		}
    98		// fclink: in this process, the packages handed over in memory (fclinklib.NewUnit) instead of objects;
    99		// with FC_OBJFILES=1 the fclink command on written objects, if there is one
   100		withFclink := useFclink() && (!objFiles || linkerPath() != "")
   101		inProc := *link != "" && withFclink && !objFiles
   102		if *link != "" && os.Getenv("FC_RUNTIME") == "" {
   103			// a whole program for our own linker: the runtime is ours too, and the go command is not needed
   104			rtByFc = withFclink
   105		}
   106		t0 := time.Now()
   107		c0 := cpuSeconds()
   108		pr := newProgram()
   378						return v
   379					}
   380					return hasInit(d)
   381				}
   382				// the linker loads a package only if some loaded object says it imports it
   383				for _, d := range p.deps {
   384					ip := goobj.ImportedPkg{Pkg: plainPath(d.ImportPath)}
   385					if stock[d] {
   386						waitStock()
   387					}
   388					if stock[d] && d.Export != "" {
   389						ip.Fingerprint = stockFingerprint(d.Export) // the linker compares it with the archive's
   390					}
   391					c.ctxt.Imports = append(c.ctxt.Imports, ip)
   392				}
   393				errs := c.compilePackage()
   394				tw := time.Now()
   395				if len(errs) == 0 && inProc {
   396					// no object: the linker reads the package's symbols where they are
   397					if asmDir != "" {
   398						c.writeAsmHdr(filepath.Join(asmDir, "go_asm.h"))
   399						for _, sf := range p.SFiles {
   400							o := filepath.Join(asmDir, strings.TrimSuffix(sf, ".s")+".o")
   401							if err := runAsmFile(p, asmDir, sf, o); err != nil {
   402								errs = append(errs, "assembler: "+err.Error())
   403								break
   404							}
   405							results[i].asmObjs = append(results[i].asmObjs, o)
   406						}
   407					}
   408					results[i].unit = fclinklib.NewUnit(c.ctxt)
   409				} else if len(errs) == 0 {
   410					name := objPath(*out, p)
   411					if asmDir != "" {
   412						// the object and the assembler's objects go into one archive
   413						goObj := filepath.Join(asmDir, "_go_.o")
   414						c.writeObject(goObj)
   490			if inProc {
   491				// the inputs in the order the importcfg would have them, the main package last
   492				byPkg := map[*Pkg]*result{}
   493				for i := range results {
   494					byPkg[results[i].p] = &results[i]
   495				}
   496				var inputs, mainIn []fclinklib.Input
   497				for _, p := range closure {
   498					path := plainPath(p.ImportPath)
   499					switch {
   500					case path == "unsafe":
   501					case stock[p]:
   502						if p.Export == "" {
   503							log.Fatalf("fc build -link: no stock archive for %s in the plan (go list -export)", path)
   504						}
   505						inputs = append(inputs, fclinklib.Input{Pkg: path, File: p.Export})
   506					default:
   507						r := byPkg[p]
   508						if p == mainPkg {
   509							path = "main"
   510						}
   511						in := []fclinklib.Input{{Pkg: path, Unit: r.unit}}
   512						for _, o := range r.asmObjs {
   513							in = append(in, fclinklib.Input{Pkg: path, File: o})
   514						}
   515						if p == mainPkg {
   516							mainIn = in
   517						} else {
   518							inputs = append(inputs, in...)
   519						}
   520					}
   521				}
   522				args := append(append([]string{"-s", "-o", *link}, xargs...), strings.Fields(os.Getenv("FC_LINKFLAGS"))...)
   523				fclinklib.Link(args, append(inputs, mainIn...))
   524				fmt.Printf("link (fclink in process): %d fc units, %d stock archives, %.0f ms\n", len(targets), len(stock), float64(time.Since(tl).Milliseconds()))
   525			} else {
   526				var cfg strings.Builder
rg: driver/fc/asm.go: IO error for operation on driver/fc/asm.go: No such file or directory (os error 2)
    48	| | stock | fair stock | this build |
    49	|---|---|---|---|
    50	| wall ms, cold | 49,953 | 32,512 | 11,009 |
    51	| core-s | 257 | 169 | 69 |
    52	| tasks created, whole machine (processes + threads) | 11,404 | 10,585 | 6,380 |
    53	| tool processes started (exec) | 1,483 | 1,483 | 172 + 1,315 forked from one warm compiler |
    54	| page faults, whole machine | 11,923,241 | 11,007,650 | 1,343,950 |
    55	| intermediates written (archives, export data) | 1,899 MB | 1,383 MB | 618 MB |
    56	| intermediates read back (importers, linker, cache copy) | 2,731 MB | 1,996 MB | 1,143 MB |
    57	| of which export data | 107 MB | 91 MB |  |
    58	
    59	Stock writes every archive twice (work directory, then GOCACHE) to disk; this build writes each once to tmpfs. "Read back" counts the export data each importer maps and every archive the linker reads.
    60	
    61	**Where the compile row's CPU goes, exactly** (`tools/trace/cpuphases.sh`: every compiler process logs its own CPU time and page faults at each phase boundary; warm run, 1,315 compiler processes, 68.1 core-s in the row):
    62	
    63	| compiler phase | user s | sys s | page faults | what it is |
    64	|---|---|---|---|---|
    65	| be:compilefuncs | 24.3 | 1.7 | 111,536 | walk, baseline code generation (SSA for the rest), assembler: **compiling** |
    66	| fe:irread | 9.1 | 1.2 | 74,184 | build the compiler's IR by reading that back, plus imported bodies and types: **moving data** |
    67	| fe:typecheck | 7.7 | 1.6 | 132,859 | types2, including reading imported packages' export data on demand: compiling (+ moving data) |
    68	| fe:irwrite | 4.6 | 0.7 | 57,626 | serialize the checked package as export IR: **moving data** |
    69	| be:dumpobj | 2.8 | 0.6 | 6,788 | write the object file: **moving data** |
    70	| fe:earlyexport | 2.4 | 0.4 | 29,548 | write export data for importers: **moving data** |
    71	| fe:devirtualize-and-inline | 2.2 | 0.2 | 11,067 | inlining is off; what is left under this timer is method wrappers and small per-function passes: compiling |
    72	| fe:escapes | 2.1 | 0.2 | 7,394 | escape analysis: compiling |
    73	| fe:parse | 1.4 | 0.6 | 71,704 | read and parse the source files: compiling |
    74	| (before main) | 0.9 | 0.2 | 126,091 | exec, runtime start, package init: **process setup** |
    75	| fe:init | 0.8 | 0.3 | 134,437 | flags, universe: process setup |
    76	| fe:loadsys | 0.5 | 0.2 | 26,153 | declare the runtime's functions: process setup |
    77	| (exit, asm, pack) | 1.6 | 0.0 | 0 | process exit, the assembler, joining shard objects |

codex
1. **Cold Bazel analysis gates the entire compiler.** Buildifier spends **6.1 s** in startup/extraction/analysis and **3.0 s** in generators before **0.36 s** of fc; runsc spends **22.1 s**, then **3.2 s**, versus **0.55 s** compiling. This is recognized but left as a question about excluding cold startup, rather than an implemented attack ([JOURNAL.md](/home/exedev/strand-w/JOURNAL.md:1650), [STATUS.md](/home/exedev/strand-w/STATUS.md:10)).

2. **The link floor is optimistic.** TRACE’s **16 ms** is a copy-bandwidth calculation, explicitly missing symbol resolution and relocation semantics ([TRACE.md](/home/exedev/strand-w/TRACE.md:22), [floor definition](/home/exedev/strand-w/TRACE.md:363)). The measured spike needed **~260 ms**, including **~200 ms resolving symbols**; today’s link is **~280 ms**, with **61 ms** for dead-code processing alone. These are implementation costs, not a proven physical floor: the real constraint includes dependent graph traversal and metadata processing, not merely bytes copied ([JOURNAL.md](/home/exedev/strand-w/JOURNAL.md:1208), [latest measurement](/home/exedev/strand-w/JOURNAL.md:1652), [PLAN.md](/home/exedev/strand-w/PLAN.md:153)).

3. **Compiler-only optimization is a dead end for the Bazel target.** Even free compilation leaves roughly **9 s / 25 s** in those two builds. That does not establish that the fused compiler itself is a dead end. I would build a cold graph evaluator for the supported Bazel/rules_go subset, starting from repository inputs, and stream generator results into compilation. Treating a previously computed aquery graph as fetched input sidesteps the barrier instead of removing it; the charter includes cold Bazel startup inside the timer ([CHARTER.md](/home/exedev/strand-w/CHARTER.md:169)).

4. **TRACE’s avoidable intermediate is serialized export IR and object archives.** Export-IR writing costs **5.3 core-s**, reading it back/importing costs **10.3 core-s**, early export **2.8**, and object writing **3.4**; total intermediate traffic is **618 MB written / 1,143 MB reread** ([TRACE.md](/home/exedev/strand-w/TRACE.md:55), [CPU breakdown](/home/exedev/strand-w/TRACE.md:66)). **Those are historical costs:** current fc hands Go symbols directly to the linker. Assembly `.o` files still cross a write/read boundary, but TRACE does not isolate their cost ([build.go](/home/exedev/strand-w/driver/fc/build.go:395), [link inputs](/home/exedev/strand-w/driver/fc/build.go:511)).

5. **Explicit time-based retreat exists, but not in the latest entries:** “Most ambitious next is to make this consumed: emit an image that runs. **Not in the hour left**” and “**No time left in this session for a second slice**” ([JOURNAL.md](/home/exedev/strand-w/JOURNAL.md:1236), [later passage](/home/exedev/strand-w/JOURNAL.md:1285), November 12). **The oracle did move:** since M3, MinIO fell from **0.27–0.30 core-s/MB** to approximately **0.18–0.21**; recorded correctness counts remain **260/262 std**, **45/46 MinIO** ([PLAN.md](/home/exedev/strand-w/PLAN.md:124), [current costs](/home/exedev/strand-w/PLAN.md:132)). The largest unimplemented performance attack is **cold Bazel graph construction, up to 22 seconds**; BuildBuddy integration is already assigned, so it is unfinished rather than unattended.
tokens used
48,232
1. **Cold Bazel analysis gates the entire compiler.** Buildifier spends **6.1 s** in startup/extraction/analysis and **3.0 s** in generators before **0.36 s** of fc; runsc spends **22.1 s**, then **3.2 s**, versus **0.55 s** compiling. This is recognized but left as a question about excluding cold startup, rather than an implemented attack ([JOURNAL.md](/home/exedev/strand-w/JOURNAL.md:1650), [STATUS.md](/home/exedev/strand-w/STATUS.md:10)).

2. **The link floor is optimistic.** TRACE’s **16 ms** is a copy-bandwidth calculation, explicitly missing symbol resolution and relocation semantics ([TRACE.md](/home/exedev/strand-w/TRACE.md:22), [floor definition](/home/exedev/strand-w/TRACE.md:363)). The measured spike needed **~260 ms**, including **~200 ms resolving symbols**; today’s link is **~280 ms**, with **61 ms** for dead-code processing alone. These are implementation costs, not a proven physical floor: the real constraint includes dependent graph traversal and metadata processing, not merely bytes copied ([JOURNAL.md](/home/exedev/strand-w/JOURNAL.md:1208), [latest measurement](/home/exedev/strand-w/JOURNAL.md:1652), [PLAN.md](/home/exedev/strand-w/PLAN.md:153)).

3. **Compiler-only optimization is a dead end for the Bazel target.** Even free compilation leaves roughly **9 s / 25 s** in those two builds. That does not establish that the fused compiler itself is a dead end. I would build a cold graph evaluator for the supported Bazel/rules_go subset, starting from repository inputs, and stream generator results into compilation. Treating a previously computed aquery graph as fetched input sidesteps the barrier instead of removing it; the charter includes cold Bazel startup inside the timer ([CHARTER.md](/home/exedev/strand-w/CHARTER.md:169)).

4. **TRACE’s avoidable intermediate is serialized export IR and object archives.** Export-IR writing costs **5.3 core-s**, reading it back/importing costs **10.3 core-s**, early export **2.8**, and object writing **3.4**; total intermediate traffic is **618 MB written / 1,143 MB reread** ([TRACE.md](/home/exedev/strand-w/TRACE.md:55), [CPU breakdown](/home/exedev/strand-w/TRACE.md:66)). **Those are historical costs:** current fc hands Go symbols directly to the linker. Assembly `.o` files still cross a write/read boundary, but TRACE does not isolate their cost ([build.go](/home/exedev/strand-w/driver/fc/build.go:395), [link inputs](/home/exedev/strand-w/driver/fc/build.go:511)).

5. **Explicit time-based retreat exists, but not in the latest entries:** “Most ambitious next is to make this consumed: emit an image that runs. **Not in the hour left**” and “**No time left in this session for a second slice**” ([JOURNAL.md](/home/exedev/strand-w/JOURNAL.md:1236), [later passage](/home/exedev/strand-w/JOURNAL.md:1285), November 12). **The oracle did move:** since M3, MinIO fell from **0.27–0.30 core-s/MB** to approximately **0.18–0.21**; recorded correctness counts remain **260/262 std**, **45/46 MinIO** ([PLAN.md](/home/exedev/strand-w/PLAN.md:124), [current costs](/home/exedev/strand-w/PLAN.md:132)). The largest unimplemented performance attack is **cold Bazel graph construction, up to 22 seconds**; BuildBuddy integration is already assigned, so it is unfinished rather than unattended.
