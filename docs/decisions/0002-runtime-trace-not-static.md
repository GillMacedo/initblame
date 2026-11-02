# 0002: Measure with the runtime, don't guess from source

**Decision:** initblame only reads what `GODEBUG=inittrace=1` reports. It doesn't look at `init` functions or package variables to estimate their cost.

**Why:** the cost of init work depends on data sizes, the GC and the machine. A static scan would flag every `regexp.MustCompile` and miss a cheap-looking call that loads a large table. The runtime trace is built into Go since 1.16 and needs no code change.

**Alternatives:** a static analyzer that lists init work. The skill gets the useful part of that by having Claude read the init code of the packages the trace flags.
