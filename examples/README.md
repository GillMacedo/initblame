# Examples

The programs here are made up. They build offline: `slowstart` uses a local `replace` for its one dependency, and `quickstart` uses only the standard library.

- `slowstart/`: a CLI whose dependency `example.com/geodata` builds a 100,000-entry map in a package variable, and whose `internal/rules` compiles 300 regexps at init. Even `--version` pays for both. Built with `-tags lazy`, geodata builds the map on first use with `sync.OnceValue` instead, which is the usual fix.
  - `trace.txt`: 5 runs of `slowstart --version`.
  - `trace-lazy.txt`: 5 runs of the lazy build, the base for the comparison in the README.
- `quickstart/`: a small HTTP health checker with no init work of its own. It's the healthy case: nothing reaches 1 ms.
  - `trace.txt`: 5 runs of `quickstart --version`.

The traces were recorded with `initblame run --save` on Go 1.27.3, Linux arm64, on 2026-11-02. `make traces` records new ones on your machine; the numbers will differ.

## real/

Traces of three open-source programs, built from shallow clones taken on 2026-11-02 with `go build` on Go 1.27.3, Linux arm64, and run 5 times each with `initblame run --save`. The `-modules.txt` files are `go version -m` output for each binary. Only the traces and module lists are stored here, no code from the projects.

| Files | Program and command | Commit | License |
|---|---|---|---|
| `gh-trace.txt`, `gh-modules.txt` | github.com/cli/cli, `gh --version` | `9490d20` | MIT |
| `helm-trace.txt`, `helm-modules.txt` | helm.sh/helm/v4, `helm version` | `bc232c5` | Apache-2.0 |
| `rclone-trace.txt`, `rclone-modules.txt` | github.com/rclone/rclone, `rclone version` | `f0cb5dd` | MIT |

To read one: `go run ./plugin/scripts --modules examples/real/helm-modules.txt examples/real/helm-trace.txt`. Import chains need the project's source: clone it at the commit, run `go mod download`, and add `--dir PATH --pkg ./cmd/helm`.
