# initblame: find what makes a Go program slow to start

[![CI](https://github.com/GillMacedo/initblame/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/initblame/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**A Claude Code skill and Go CLI for Go developers whose CLI, serverless function or service is slow to start or has slow cold starts. It reads the runtime's `GODEBUG=inittrace=1` output, takes medians over several runs, totals init time and allocations by module, shows the import chain that brings each slow package in, and compares two builds to find what got slower.**

Before `main` runs, Go runs the init work of every package the program imports: `init()` functions and package-level variables such as `regexp.MustCompile` tables, big maps, embedded data parsing and type registration. It runs one package at a time, for every command, even `--version`. Go can print each package's cost with `GODEBUG=inittrace=1`, but a large program prints hundreds of lines per run, by package rather than by module, the numbers jump between runs, and nothing says why a package is in the binary at all. `initblame` answers those questions. The skill then has Claude read the init code of each slow package and suggest a fix.

## What you can ask Claude

- "Our Go CLI is slow even for `--version`. What is it doing at startup?"
- "Lambda cold starts got slower after we upgraded dependencies. Here are inittrace logs from before and after."
- "Which dependency makes our binary allocate 10 MB before main runs?"
- "Why is `k8s.io/kubectl` in our binary, and what does it cost at startup?"

## Install

As a Claude Code plugin:

    claude plugin marketplace add GillMacedo/initblame
    claude plugin install initblame@initblame-marketplace

Or as a CLI with Go 1.22 or newer. It has no dependencies:

    go run github.com/GillMacedo/initblame/plugin/scripts@latest run -- ./yourprogram --version

## Example

`examples/slowstart` is a made-up CLI. Its dependency `example.com/geodata` builds a 100,000-entry map in a package variable, and its own `internal/rules` compiles 300 regexps. `trace.txt` holds 5 runs of `slowstart --version`. From a clone of this repo:

    $ go run ./plugin/scripts --dir examples/slowstart --pkg ./cmd/slowstart examples/slowstart/trace.txt
    17 packages ran init work, 23.8 ms of init clock in total, 12.9 MB allocated (median of 5 runs).
    The last init finished 55.0 ms after the process started.
    Modules from go list in examples/slowstart.

    Modules by init clock:
       17.0 ms   71%    7.4 MB  example.com/geodata (1 package)
        6.7 ms   28%    5.5 MB  example.com/slowstart (1 package)

    2 packages took 1 ms or more:
       17.0 ms    7.4 MB  example.com/geodata
          imported by main -> example.com/slowstart/internal/report -> example.com/geodata
        6.7 ms    5.5 MB  example.com/slowstart/internal/rules
          imported by main -> example.com/slowstart/internal/report -> example.com/slowstart/internal/rules

The usual fix is to build the value on first use. The same program built with `-tags lazy` makes geodata use `sync.OnceValue`. Comparing the eager build against the lazy one as the base shows what the eager map costs. `internal/rules` also moved, from 5.6 to 6.7 ms, but that's within run-to-run noise, so it isn't listed:

    $ go run ./plugin/scripts --dir examples/slowstart --pkg ./cmd/slowstart --base examples/slowstart/trace-lazy.txt examples/slowstart/trace.txt
    17 packages ran init work, 23.8 ms of init clock in total, 12.9 MB allocated (median of 5 runs).
    The last init finished 55.0 ms after the process started.
    Modules from go list in examples/slowstart.

    Base: 17 packages, 5.7 ms of init clock, last init finished at 6.0 ms.
    Change: +18.1 ms of init clock, 0 packages added, 0 removed.

    1 package new or slower by 1 ms or more:
       +17.0 ms  example.com/geodata (0.0 ms -> 17.0 ms)
          imported by main -> example.com/slowstart/internal/report -> example.com/geodata

A small program with no heavy init work stays quiet. `examples/quickstart` imports `net/http` and `flag`:

    $ go run ./plugin/scripts examples/quickstart/trace.txt
    75 packages ran init work, 0.2 ms of init clock in total, 47.0 kB allocated (median of 5 runs).
    The last init finished 1.4 ms after the process started.
    Modules from package paths (a guess; pass --bin, --modules or --dir for exact modules).

    No package's init took 1 ms or more.

On a real program: the GitHub CLI, `gh` at commit `9490d20`, built with Go 1.27.3 on Linux arm64. On `gh --version`, two packages of the `chroma` syntax highlighter take 8.0 of its 12.9 ms of init clock:

    $ go run ./plugin/scripts --modules examples/real/gh-modules.txt examples/real/gh-trace.txt
    382 packages ran init work, 12.9 ms of init clock in total, 10.2 MB allocated (median of 5 runs).
    The last init finished 16.1 ms after the process started.
    Modules from go version -m output.

    Modules by init clock:
        8.0 ms   62%    4.5 MB  github.com/alecthomas/chroma/v2 (5 packages)

    2 packages took 1 ms or more:
        4.1 ms    1.8 MB  github.com/alecthomas/chroma/v2/styles
        3.8 ms    2.6 MB  github.com/alecthomas/chroma/v2/lexers

With the gh source and `--dir`, the chain for both is `main -> ... -> github.com/cli/cli/v2/pkg/markdown -> charm.land/glamour/v2 -> charm.land/glamour/v2/ansi -> ...chroma`. On `helm version` at `bc232c5`, `k8s.io/kubectl/pkg/cmd/get` takes 6.8 of 19.0 ms, imported directly by `helm.sh/helm/v4/pkg/cmd`. On `rclone version` at `f0cb5dd`, rclone's own 193 packages take 11.6 of 16.9 ms with no package above 2.6 ms, so the cost is spread out. The traces are in [`examples/real/`](examples/real/). These numbers come from one machine. They show where init time goes in these programs, not whether it matters to their users.

## Usage

    initblame [flags] TRACE...                 read GODEBUG=inittrace=1 output, - for stdin
    initblame run [flags] -- BINARY [ARGS...]  run a Go binary with inittrace on

Make a trace by hand with `GODEBUG=inittrace=1 ./yourprogram --version 2> trace.txt`. Several runs can go in one file: a package seen again starts a new run. Lines with other text, such as log prefixes, are skipped, as long as each `init ...` line is whole.

- `run` starts the binary 5 times (`-n`), discards its stdout, and stops each run after 10 seconds (`--timeout`). Init finishes before `main`, so a server that keeps running still gives a full trace. `--save FILE` keeps the raw trace. Only run commands that are safe to repeat, such as `--version` or `--help`.
- `--dir DIR` is the module root with the program's source. It runs `go list -deps` there with `GOPROXY=off` to find the import chain of each package. `--pkg` picks the main package when it isn't the binary's or `.`.
- Modules come from `--bin BINARY` (its build info), `--modules FILE` (saved `go version -m` output), or `--dir`. Without any of them, they're guessed from package paths, and the output says so.
- `--base FILE` compares with an earlier trace. A package is listed when it's new, or slower by `--min` ms and by at least half again its base time.
- `--min MS` (default 1) is the threshold, and `--top N` (default 10) the list length. `--json` prints everything.
- `--budget MS` exits 1 when total init clock is over the limit. `--fail` exits 1 when `--base` finds a package. Exit code 2 means bad usage or input.

To check a branch against main, build both and compare. This sequence was run in a local git repo, not yet in a CI service:

    (cd path/to/initblame && go build -o /tmp/initblame ./plugin/scripts)
    git worktree add /tmp/base main && (cd /tmp/base && go build -o /tmp/old ./cmd/app)
    go build -o /tmp/new ./cmd/app
    /tmp/initblame run --save old.txt -- /tmp/old --version
    /tmp/initblame run --base old.txt --fail -- /tmp/new --version

## Limits

- **Init only.** The trace covers package init work. The runtime's own setup before the first package, and everything `main` does, such as reading config or connecting to services, aren't in it. "The last init finished" is the time from process start, so it also includes gaps between packages, such as GC work.
- **Noisy numbers.** In the saved traces, one package's clock varied between 13 and 65 ms across 5 runs. Medians help. Compare traces from the same machine, and treat changes under about 1 ms, or under half again, as noise.
- **Plugins aren't traced.** The runtime doesn't trace packages loaded with the `plugin` package.
- **Chains are the shortest path.** A package imported from several places shows one chain. Removing that import may not remove the package.
- **`--dir` needs the module cache or a vendor directory.** It never downloads. If modules are missing it stops and says to run `go mod download`. Build tags and GOOS in `go list` are the host's, so they should match how the binary was built.
- **Go 1.16 or newer** for the program being traced, since that's when inittrace was added. The tool itself needs Go 1.22.

## FAQ

**How do I find out why my Go program starts slowly?** Run it with `GODEBUG=inittrace=1` to see each package's init work, or use `initblame run -- ./prog --version`, which does that several times and sums it up. If init is small, the time goes to `main` or to the runtime, and a CPU profile of `main` is the next step.

**What is GODEBUG=inittrace=1?** A runtime setting, since Go 1.16, that prints one line to stderr for each package with init work: when it started, how long it took, and how much it allocated. See the [runtime docs](https://pkg.go.dev/runtime#hdr-Environment_Variables).

**How do I make a slow package-level variable lazy?** Wrap its construction in `sync.OnceValue` (Go 1.21) or `sync.Once`, and call it where the value is used. `examples/slowstart/geodata/lazy.go` shows the change.

**Does this help with AWS Lambda or Cloud Run cold starts?** It covers the init part of a cold start. Set `GODEBUG=inittrace=1` in the function's environment for one deploy, copy the `init` lines from its logs into a file, and pass the file to `initblame`.

**Can a dependency I don't call still slow down startup?** Yes. Importing a package runs its init work even if you never call it. The import chain shows why the package is in your binary.

**Is it an MCP server?** No. It's a skill with one Go source file, so `go run` is all it needs.

## Tested on

- **Example programs in `examples/`**, built and run by the integration tests in `tests/` with Go 1.27.3 on Linux arm64. They check that `example.com/geodata` is the slowest package with the right import chain and over 1 MB allocated, that the lazy build removes its init cost and a comparison with `--fail` exits 1, that the package count matches the lines the runtime printed, and that a program that keeps running is stopped and still traced.
- **Healthy case:** `examples/quickstart` stays quiet, stays within a 50 ms budget, and comparing it with itself finds nothing.
- **Real programs:** `gh`, `helm` and `rclone` at the commits above, built from source, with `run` and with `--dir` on their source for import chains. Running `gh` with `--dir` first showed 6 packages with no chain, because inittrace escapes dots in names like `gopkg.in/yaml%2ev3`. That is fixed and covered by a unit test. With an empty module cache, `--dir` on gh stopped with the `go mod download` message, and `go list` left all three source trees unchanged.
- **README:** a test runs each `$ ` command above and compares the output.

It hasn't been tested on Windows or macOS, on Linux amd64, on Go versions other than 1.27.3 (the CI config also runs the tests on Go 1.22), with vendored modules, on real serverless platforms, in a fresh Claude session, or through `go run ...@latest`, which needs the published repo.

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues are [listed here](https://github.com/GillMacedo/initblame/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes. PRs that change CI, agent settings, npm or Python dependencies, vendored code or build files can't be merged this way, and a comment says so. The same goes for anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about two hours, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code and reviewed by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `initblame` saved you some prompts, a star helps others find it.

---

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
