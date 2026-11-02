# initblame plugin

This plugin adds the `finding-slow-go-startup` skill to Claude Code. When a Go program is slow to start, Claude runs the bundled `scripts/initblame.go` with `go run` on a `GODEBUG=inittrace=1` trace. It lists the packages and modules whose init work takes the most time and memory, the import chain that brings each one in, and what changed since an earlier trace. Claude then reads the init code of each slow package and suggests a fix. It needs Go 1.22 or newer. See the repository README for examples and limits.

Install:

    claude plugin marketplace add GillMacedo/initblame
    claude plugin install initblame@initblame-marketplace
