# initblame

Claude Code skill and Go CLI that reads Go's `GODEBUG=inittrace=1` output and reports which packages and modules make a program slow to start, and the import chain that brings each one in.

## Architecture
`plugin/scripts/initblame.go` holds all the logic in one standard-library file, so `go run` needs no setup. `ParseTrace` reads inittrace lines and splits runs, `Summarize` takes per-package medians and totals by module, `fromBuildInfo`, `fromVersionM` and `loadGraph` find modules, `chains` finds import chains from `go list`, `Compare` diffs two traces, `collect` runs a binary in `run` mode, and `Print` writes the report. `plugin/skills/finding-slow-go-startup/SKILL.md` tells Claude how to use it. See `docs/architecture.md`.

## Commands
- `make test`: gofmt, vet, unit tests, and integration tests in `tests/` that build and run the example programs and check every README command
- `make lint`: plugin and marketplace validation
- `make demo`: run on the saved example traces
- `make traces`: record new example traces on this machine

## Conventions
- Standard library only. The tool runs `go list` (offline, with GOPROXY=off) and, in `run` mode, the binary the user named. Never use the network, never build the user's code, never write files other than `--save`.
- The runtime's trace is the source of truth. Don't estimate init cost from source code.
- README examples are real output from the saved traces in `examples/`. `TestReadmeCommands` fails if they drift. Regenerate them after changing the output format.

## Do and don't
- Do say "init clock" for what the trace measures. Don't call it startup time: runtime setup and `main` aren't in it.
- Don't present small differences as findings. Times vary between runs, which is why `run` takes medians and `Compare` needs a change of half again.

## Definition of done
`make test` and `make lint` pass, and `ROADMAP.md` and docs are updated when relevant.
