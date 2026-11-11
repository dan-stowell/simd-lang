In this repository (Strand), the most recent commits replaced or changed a phase of the build
pipeline described in CHARTER.md and TRACE.md. Check only correctness of that phase against the stock
toolchain: find inputs where its output differs (tokens, AST, types, or binary behaviour), especially at
chunk boundaries and in rarely used syntax. Run the existing differential tests and write new
counterexamples if you find any. Write findings as you go into the output; do not change implementation
or tests. Name each counterexample as a minimal input file.
