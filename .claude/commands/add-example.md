Add an example program for this startup case: $ARGUMENTS

1. Create a small made-up program in `examples/` with its own go.mod and only standard-library or local `replace` dependencies, so it builds offline.
2. Build it, run `go run ./plugin/scripts run --dir examples/NAME -- BINARY --version`, and check the slow packages it lists against the code's init work.
3. Add an integration test in `tests/integration_test.go` that builds and runs it, and a line in `examples/README.md`.
4. Run `make test`.
