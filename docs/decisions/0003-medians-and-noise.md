# 0003: Medians over runs, and a noise rule for comparisons

**Decision:** `run` starts the program 5 times by default and reports the median per package. `Compare` lists a package only if it's new, or slower by `--min` ms and by at least 1.5 times its base time.

**Why:** in the saved traces in `examples/`, 5 runs of the same binary on the same machine gave very different clocks for the same package. The gap between the fastest and slowest run was 11 to 39 percent for some packages and several times for others, mostly from one outlier run: `example.com/geodata` took 13, 14, 17, 17 and 65 ms. A single run, or any change over 1 ms, would report noise as a regression. The median of those five is 17 ms.

**Alternatives:** statistical tests like benchstat's. They need more runs than people will wait for at startup, and the large regressions this tool is for show up clearly with medians.
