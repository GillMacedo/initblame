# Security

Report vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/GillMacedo/initblame/security/advisories/new), not in a public issue.

`initblame` reads trace files, reads the module list from a Go binary, and runs `go list` offline when given `--dir`. Its `run` mode starts the binary you name, with your arguments, and nothing else. Please report privately if a crafted trace, binary or module could make it write files other than `--save`, use the network, or run a program you didn't name. A wrong time or module is a normal bug.
