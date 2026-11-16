# fc flow study

Open `index.html` directly in a WebGL-capable browser; no server, dependencies or network needed.
This is an architectural illustration, **not a measured timeline or throughput simulation**.

- Stock: package-level parallel compilation, optimizing generation, export/archive handoffs, separate link.
- fc: early declaration availability, body workers, direct generation, in-memory symbol units and link.
- Eight illustrative lanes are not a claim of full CPU utilization. Stock caching is outside this cold-build view.
- Experimental demand-driven compilation is not assumed. Runtime performance is not equated with build speed.

Local references: `driver/fc/build.go`, `PLAN.md`, `JOURNAL.md`, `STATUS.md`.
Timings quoted in the footer are historical November 15 morning measurements, with the later-correctness-rerun caveat.

Validation: loaded via file:// in Chromium; WebGL context created, no GL error, canvas sized to display;
Pause toggles to Play, explanation selection updates aria-pressed and text, boundary control toggles.
Also supports reduced-motion preference (paused initially), horizontal scrolling on small screens,
and a textual explanation when WebGL is unavailable.
