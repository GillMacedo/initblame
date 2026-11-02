# Changelog

All notable changes are documented here, in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-11-02
### Added
- Reads `GODEBUG=inittrace=1` output, with several runs in one file, and reports the median init clock, allocations and run count for each package
- Totals init work by module, using the binary's build info, saved `go version -m` output, or `go list`
- Shows the import chain that brings each slow package in, from `go list` run offline with `--dir`
- `run` mode starts a Go binary several times with inittrace on, and stops programs that keep running after a timeout
- `--base` compares two traces and lists packages that are new or clearly slower, with `--fail` and `--budget` exit codes for CI
- `--json`, `--min`, `--top` and `--save` options
- Claude Code plugin with the `finding-slow-go-startup` skill
