---
name: finding-slow-go-startup
description: Finds which packages and modules make a Go program slow to start, from the runtime's GODEBUG=inittrace=1 output, and which import chain brings each one in. Use when a Go CLI, serverless function (Lambda, Cloud Run) or service is slow to start or has slow cold starts, when even --version or --help is slow, when startup got slower after adding or upgrading a dependency, or when the user asks about init() cost, package-level variables, or startup allocations in Go.
---

# Finding slow Go startup

Before `main` runs, Go runs every imported package's `init` work: `init()` functions and package-level variables such as `regexp.MustCompile`, big maps, embedded data parsing, and scheme or codec registration. It runs on one goroutine, in order, for every command, even `--version`. The Go runtime can report each package's share with `GODEBUG=inittrace=1`. The bundled tool reads that report, takes medians over several runs, totals it by module, and shows the import chain that brings each slow package in.

It needs Go installed. It reads trace files, runs `go list` offline when given `--dir`, and runs the user's binary only in `run` mode.

## Get a trace

Prefer a trace the user already has, or ask them to make one. If they agree to let you run their program, use a command that exits quickly and does nothing else, such as `--version` or `--help`:

    go build -o /tmp/prog ./cmd/prog
    go run "${CLAUDE_PLUGIN_ROOT}/scripts/initblame.go" run --dir . -- /tmp/prog --version

- `run` starts the binary 5 times with `GODEBUG=inittrace=1` (`-n` to change it), discards its stdout, and stops each run after 10 seconds (`--timeout`). Init finishes before `main`, so a server that keeps running still gives a full trace. Don't use `run` on a command that writes, deletes, sends or deploys anything: init only needs the program to start.
- `--save trace.txt` keeps the raw trace so you can compare later.
- To read a trace the user saved: `go run "${CLAUDE_PLUGIN_ROOT}/scripts/initblame.go" --bin /tmp/prog --dir . trace.txt`. Several runs can be in one file.
- For a deployed program, the user can set `GODEBUG=inittrace=1` in its environment and copy the `init ...` lines from its logs. Those lines go to stderr.

Flags: `--dir` is the module root with the source, for import chains, and `--pkg` picks the main package when it isn't the binary's or `.`. `--bin` or `--modules` (saved `go version -m` output) give exact modules. `--min` sets the threshold in ms (default 1), `--top` the list length, `--json` gives everything.

## Compare before and after

When startup got slower after a change, get a trace from each build and compare:

    go run "${CLAUDE_PLUGIN_ROOT}/scripts/initblame.go" --dir . --base before.txt after.txt

It lists packages that are new, or slower by `--min` ms and by at least half again. Add `--fail` to exit 1 when it finds any, or `--budget MS` to exit 1 when total init clock is over a limit, for CI.

## Explain each slow package

For each package the tool lists:

1. Open its source, in the module cache (`go env GOMODCACHE`) or the repo, and find what runs at init: `func init()` and package-level `var x = ...` with a function call. Name the line, such as a `regexp.MustCompile` table, a large map literal, JSON or YAML parsing, or registering many types.
2. Read the import chain. The step from the user's module to the first outside package is the import they control.
3. Suggest the smallest fix that fits:
   - In their own code: build the value on first use with `sync.OnceValue` or `sync.Once`, or move it into the function that needs it.
   - For a dependency: import it only from the subcommand or package that needs it, choose a lighter sub-package (for example one lexer instead of all of them), or check whether a newer version made it lazy. Don't suggest forking a dependency unless asked.
4. If a module has many small packages and no single slow one, say that its cost is spread out and fixing one package won't help much.

## Report

1. Total init clock, when the last init finished, and how many runs the numbers come from.
2. A table: package, ms, allocated, what it does at init, the import from the user's code, and the fix.
3. Say that times vary between runs and machines, so a package under about 1 ms or a change of less than half again is likely noise.

## Don't

- Don't claim init is the whole startup time. The runtime's own setup, loading config, connecting to services and the work `main` does are not in the trace.
- Don't edit code or go.mod unless the user asks.
- Don't run `go mod download` without asking, since it uses the network. If `--dir` fails because modules are missing, say so, and run without `--dir`.
