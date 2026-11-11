Divergence step. Spawn 3-4 subagents in parallel. Give each ONLY: CHARTER.md, TRACE.md, HARDWARE.md,
and one lens below. Do not give them DESIGN.md or the current approach; they should be unanchored.
Ask each for 3 ideas, each with: the idea, why it could remove a chunk of the critical path or of
bytes moved, and a kill test runnable in under an hour.

Lenses:
- Flip a constraint: what if there were no linking, no object files, no per-package boundaries,
  no ASTs, no symbol strings, no disk?
- Borrow from another field: GPU rasterization and binning, columnar databases and query engines,
  regex/automata engines, genomics aligners, network packet processing, video codecs.
- Ridiculous on purpose: propose something an expert would call absurd, and the experiment that
  would show it isn't.
- Design the problem away: where the known algorithm is sequential (type inference, name
  resolution, generic instantiation, linking), change what is computed instead of parallelizing it.

Append all ideas to IDEAS.md under a dated heading with status "untested". Pick the one with the
best (payoff × cheapness of kill test) for the next spike.
