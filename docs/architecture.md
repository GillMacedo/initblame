# Architecture

    trace file(s) or `run` (binary started N times with GODEBUG=inittrace=1, stderr kept)
        --> ParseTrace: `init PKG @START ms, CLOCK ms clock, BYTES bytes, ALLOCS allocs`
            a package seen again starts a new run; %2e in symbol names is undone
        --> Summarize: median clock, bytes and allocs per package over runs; median end of the last init
    module list, first match wins:
        --bin BINARY        debug/buildinfo: main module and deps
        --modules FILE      saved `go version -m` text
        --dir DIR           `go list -deps -json` with GOPROXY=off
        none                a guess from the package path's host layout
        --> each package's module by longest prefix; standard library as "std"
    --dir DIR --> import graph from `go list -deps` --> shortest chain from the main package (BFS)
    --base FILE --> Compare: new packages, or slower by --min ms and by at least 1.5x
    Print / JSON --> report, exit 1 for --budget or --fail, 2 for bad input

- **Source of truth:** the runtime measures each package's init on the one goroutine that runs them in order, so the numbers are what the program really spent. initblame only aggregates them.
- **Noise:** init times of the same binary vary between runs, sometimes by several times in one outlier run (see `decisions/0003-medians-and-noise.md`). Medians over runs and the 1.5x rule in `Compare` keep noise out of the findings. `run` defaults to 5 runs.
- **Names:** inittrace prints the linker's symbol prefix, which escapes a dot in the last path element as `%2e` and prints the main package as `main`. Vendored packages in `go list` are mapped back to their import paths.
- **Long-running programs:** init finishes before `main`, so `run` stops a program after `--timeout` and keeps its trace.

Key choices: one standard-library Go file run with `go run`, a skill instead of an MCP server, and medians instead of single runs. See `decisions/`.
