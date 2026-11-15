#!/usr/bin/env python3
"""tools/fc/safecheck.py <fc call graph (FC_CALLGRAPH)> <go tool objdump of the stock build of the same program>

A //go:nosplit function of the runtime's packages has, in the stock build, no safepoint except where its machine
code calls a function that has one: the helpers the stock compiler inlines into it have vanished. fc does not
inline, so each such helper must have no stack check (no synchronous preemption) and no asynchronous safe point,
or the collector can see the nosplit function's frame at a place the runtime's authors never had to consider
(runtime.reflectcallmove with a pointer past the end of an object in its arguments, 2026-11-15; the helpers the
stock compiler inlines, 2026-11-13).

For every //go:nosplit function F that both builds have, compare what can stop inside F's dynamic extent before
the first function with a stack check:
  fc:    the functions reached from F through calls to functions without a stack check; each one that has a
         stack check, or that a signal may preempt (async-safe body, name outside runtime., internal/runtime/,
         reflect.), is a candidate;
  stock: the same walk over the stock machine code.
A candidate the stock walk also reaches is the stock build's behaviour too, and so is anything below a function
that has a stack check in the stock build (fc may have left the check out of it). Any other is reported.
Panic paths are left out (a function that never returns is not a place the frame is resumed from).

Also, as fc's linker has no nosplit check: the deepest chain of frames without a stack check below every
//go:nosplit function must fit the stack every function is guaranteed (the stock linker's limit)."""
import re, sys

fcg, dis = sys.argv[1], sys.argv[2]
verbose = len(sys.argv) > 3

def norm(n):
    return n[:-5] if n.endswith(".abi0") else n

# fc: name -> [nosplit, pragma, noasync, callees]; a wrapper shares its function's name: merged
fc = {}
for line in open(fcg):
    f = line.rstrip("\n").split("\t")
    name, ns, pragma, noasync, frame, callees = norm(f[0]), f[1] == "true", f[2] == "true", f[3] == "true", int(f[4]), [norm(c) for c in f[5:]]
    if name in fc:
        o = fc[name]
        o[0] = o[0] and ns; o[1] = o[1] or pragma; o[2] = o[2] and noasync   # (the wrapper calls only its function)
        o[3] = list(dict.fromkeys(o[3] + [c for c in callees if c != name]))
        o[4] += frame + 16
    else:
        fc[name] = [ns, pragma, noasync, [c for c in callees if c != name], frame]

# stock: name -> (has a stack check, callees)
stock = {}
cur = None
for line in open(dis):
    if line.startswith("TEXT "):
        cur = norm(line.split("(SB)")[0][5:])
        stock.setdefault(cur, [False, []])
        continue
    if cur is None:
        continue
    m = re.search(r"\b(?:CALL|JMP) (\S+)\(SB\)", line)
    if m:
        c = norm(m.group(1))
        if c.startswith("runtime.morestack"):
            stock[cur][0] = True
        elif c != cur:
            stock[cur][1].append(c)

# diagnostics: print and println before a throw (and under runtime.KeepAlive's never-true condition)
diag = re.compile(r"^runtime\.(print\w+|gwrite|recordForPanic)$")
# an interface made from a constant or a small value: static data or nothing in the stock build, an allocation in
# fc's (the argument of a panic in LockOSThread, of a println); reported apart
conv = re.compile(r"^runtime\.(convT\w*|newobject)$")
noreturn = re.compile(r"^runtime\.(goPanic\w*|panic\w*|throw|fatal\w*|gopanic|badsystemstack|badmcall\w*|abort|exit\w*|sigpanic\w*|dieFromSignal|crash|morestack\w*|newstack|badunlockosthread|badmorestack\w*|badreflectcall|deferreturn)$")
runtimeName = re.compile(r"^(runtime\.|internal/runtime/|reflect\.)")

def stockWalk(f):
    seen = set()
    todo = [f]
    while todo:
        x = todo.pop()
        if x in seen or x not in stock:
            continue
        seen.add(x)
        if stock[x][0] and x != f:
            continue
        todo += stock[x][1]
    return seen

def fcWalk(f, sw):
    """-> {candidate: (kind, path)}"""
    out, seen, todo = {}, set(), [(f, (f,))]
    while todo:
        x, path = todo.pop()
        if x in seen:
            continue
        seen.add(x)
        if x not in fc or noreturn.match(x) or diag.match(x):
            continue   # assembly (the same code in both builds), a panic, a diagnostic
        if x != f and x in sw and stock[x][0]:
            continue   # the stock build can stop here too (its stack check): what fc does below is inside that extent
        ns, _, noasync, callees, _ = fc[x]
        if x != f:
            if not ns:
                out[x] = ("stack check", path)
                continue
            if not noasync and not runtimeName.match(x):
                out[x] = ("async", path)
        for c in callees:
            todo.append((c, path + (c,)))
    return out

def depth(f, memo, onpath):
    """the deepest stack below f's entry before a stack check: frames of the functions without one"""
    if f in memo:
        return memo[f]
    if f not in fc or noreturn.match(f) or diag.match(f):
        return (0, ())
    if f in onpath:
        return (0, ())   # (recursion without a stack check would be its own report; the runtime has none)
    ns, _, _, callees, frame = fc[f]
    own = frame + 16   # return address and saved BP
    if not ns:
        return (0, ())
    best = (0, ())
    onpath.add(f)
    for c in callees:
        d = depth(c, memo, onpath)
        if d[0] > best[0]:
            best = d
    onpath.discard(f)
    memo[f] = (own + best[0], (f,) + best[1])
    return memo[f]

bad, accepted, deep = {}, {}, []
nF = 0
memo = {}
# the stock linker's rule: abi.StackNosplitBase times the guard multiplier, less a call. fc's runtime is
# compiled with the race build's guard (multiplier 2: driver/fc/load.go takes consts_race.go), because its
# frames are larger than the stock compiler's.
LIMIT = 800 * 2 - 8
for f, (ns, pragma, noasync, callees, frame) in sorted(fc.items()):
    if not pragma:
        continue
    d = depth(f, memo, set())
    if d[0] > LIMIT:
        deep.append((d[0], d[1]))
    if f not in stock or re.search(r"_test\.", f):
        continue
    nF += 1
    sw = stockWalk(f)
    for g, (kind, path) in fcWalk(f, sw).items():
        if g in sw:
            continue
        if any(conv.match(p) for p in path[1:]):
            accepted.setdefault((g, kind), []).append(path)
            continue
        bad.setdefault((g, kind), []).append(path)
missing = sum(1 for f, v in fc.items() if v[1] and f not in stock)
for (g, kind), paths in sorted(accepted.items()):
    p = min(paths, key=len)
    print("safecheck: (accepted: an interface conversion that allocates only in fc's build) %s from %d function(s), e.g. %s" % (g, len(paths), " -> ".join(p)))
for n, path in sorted(deep, reverse=True)[:20]:
    print("safecheck: %d bytes of stack without a check (limit %d): %s" % (n, LIMIT, " -> ".join(path)))
for (g, kind), paths in sorted(bad.items()):
    p = min(paths, key=len)
    print("safecheck: %s (%s) is reached from %d nosplit function(s) that have no such stop in the stock build, e.g. %s" % (g, kind, len(paths), " -> ".join(p)))
    if verbose:
        for q in sorted(paths, key=len)[1:6]:
            print("    " + " -> ".join(q))
print("safecheck: %d //go:nosplit functions compared (%d more are not in the stock binary or are test code), %d functions to fix, %d nosplit chains over the stack limit" % (nF, missing, len(bad), len(deep)))
sys.exit(1 if bad or deep else 0)
