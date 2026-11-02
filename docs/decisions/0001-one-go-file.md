# 0001: One standard-library Go file in a skill

**Decision:** the tool is a single Go file with no dependencies, run with `go run`, bundled in a skill.

**Why:** anyone whose Go program starts slowly already has Go, so `go run` is the whole install. An agent can already run a script and read the init code it points at, so an MCP server would add a binary and an npm package without giving the agent anything new.

**Alternatives:** an MCP server with the official Go SDK, like `stuckgo`. It would make sense if agents needed this without a shell.
