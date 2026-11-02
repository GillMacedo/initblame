// Command initblame reads Go's GODEBUG=inittrace=1 output and reports which
// packages and modules make a program slow to start, and which import chain
// brings each one in.
//
// It uses only the standard library so that `go run` needs no setup.
package main

import (
	"bufio"
	"bytes"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const version = "0.1.0"

// Init is one line of inittrace output.
type Init struct {
	Package string
	StartMS float64
	ClockMS float64
	Bytes   int64
	Allocs  int64
}

// Run is the init work of one process, in the order it ran.
type Run []Init

var lineRE = regexp.MustCompile(`init (\S+) @([0-9.]+) ms, ([0-9.]+) ms clock, ([0-9]+) bytes, ([0-9]+) allocs`)

// ParseTrace reads inittrace lines from r and ignores everything else. Each
// package's init runs once per process, so a package seen again starts a new
// run. That lets one file hold several runs of the same program.
func ParseTrace(r io.Reader) ([]Run, error) {
	var runs []Run
	var cur Run
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		m := lineRE.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		in := Init{Package: unescapeSymbol(m[1])}
		in.StartMS, _ = strconv.ParseFloat(m[2], 64)
		in.ClockMS, _ = strconv.ParseFloat(m[3], 64)
		in.Bytes, _ = strconv.ParseInt(m[4], 10, 64)
		in.Allocs, _ = strconv.ParseInt(m[5], 10, 64)
		if seen[in.Package] {
			runs = append(runs, cur)
			cur, seen = nil, map[string]bool{}
		}
		seen[in.Package] = true
		cur = append(cur, in)
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}
	return runs, sc.Err()
}

// unescapeSymbol undoes the linker's escaping of package paths in symbol
// names, which inittrace prints as is: a dot in the last path element shows
// up as %2e, as in gopkg.in/yaml%2ev3.
func unescapeSymbol(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Package is one package's init work, as the median over runs.
type Package struct {
	Path    string   `json:"path"`
	Module  string   `json:"module"`
	ClockMS float64  `json:"clock_ms"`
	Bytes   int64    `json:"bytes"`
	Allocs  int64    `json:"allocs"`
	Runs    int      `json:"runs"`
	Chain   []string `json:"chain,omitempty"`
}

// Module totals the init work of its packages.
type Module struct {
	Path     string  `json:"path"`
	ClockMS  float64 `json:"clock_ms"`
	Bytes    int64   `json:"bytes"`
	Allocs   int64   `json:"allocs"`
	Packages int     `json:"packages"`
}

// Profile is the summary of a set of runs.
type Profile struct {
	Runs     int        `json:"runs"`
	TotalMS  float64    `json:"total_clock_ms"`
	DoneMS   float64    `json:"init_done_ms"`
	Bytes    int64      `json:"bytes"`
	Packages []*Package `json:"packages"`
	Modules  []*Module  `json:"modules"`
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// Summarize takes the median of each package over the runs, assigns modules
// with modOf, and totals by module. Packages are sorted by clock, slowest first.
func Summarize(runs []Run, modOf func(string) string) *Profile {
	type acc struct {
		clock, bytes, allocs []float64
		first                int
	}
	byPkg := map[string]*acc{}
	order := 0
	var done []float64
	for _, r := range runs {
		end := 0.0
		for _, in := range r {
			a := byPkg[in.Package]
			if a == nil {
				a = &acc{first: order}
				order++
				byPkg[in.Package] = a
			}
			a.clock = append(a.clock, in.ClockMS)
			a.bytes = append(a.bytes, float64(in.Bytes))
			a.allocs = append(a.allocs, float64(in.Allocs))
			end = math.Max(end, in.StartMS+in.ClockMS)
		}
		done = append(done, end)
	}
	p := &Profile{Runs: len(runs), DoneMS: round(median(done))}
	mods := map[string]*Module{}
	for path, a := range byPkg {
		pk := &Package{
			Path:    path,
			Module:  modOf(path),
			ClockMS: round(median(a.clock)),
			Bytes:   int64(median(a.bytes)),
			Allocs:  int64(median(a.allocs)),
			Runs:    len(a.clock),
		}
		p.Packages = append(p.Packages, pk)
		p.TotalMS += pk.ClockMS
		p.Bytes += pk.Bytes
		m := mods[pk.Module]
		if m == nil {
			m = &Module{Path: pk.Module}
			mods[pk.Module] = m
		}
		m.ClockMS += pk.ClockMS
		m.Bytes += pk.Bytes
		m.Allocs += pk.Allocs
		m.Packages++
	}
	p.TotalMS = round(p.TotalMS)
	sort.Slice(p.Packages, func(i, j int) bool {
		a, b := p.Packages[i], p.Packages[j]
		if a.ClockMS != b.ClockMS {
			return a.ClockMS > b.ClockMS
		}
		return a.Path < b.Path
	})
	for _, m := range mods {
		m.ClockMS = round(m.ClockMS)
		p.Modules = append(p.Modules, m)
	}
	sort.Slice(p.Modules, func(i, j int) bool {
		a, b := p.Modules[i], p.Modules[j]
		if a.ClockMS != b.ClockMS {
			return a.ClockMS > b.ClockMS
		}
		return a.Path < b.Path
	})
	return p
}

func round(x float64) float64 { return math.Round(x*1000) / 1000 }

// isStd reports whether an import path belongs to the standard library: its
// first element has no dot.
func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".") && first != "main"
}

// Modules maps packages to modules by longest module path prefix.
type Modules struct {
	Main   string
	Paths  []string
	Source string
}

func (ms *Modules) Of(pkg string) string {
	if pkg == "main" {
		if ms.Main != "" {
			return ms.Main
		}
		return "main"
	}
	if isStd(pkg) {
		return "std"
	}
	best := ""
	for _, m := range ms.Paths {
		if (pkg == m || strings.HasPrefix(pkg, m+"/")) && len(m) > len(best) {
			best = m
		}
	}
	if best != "" {
		return best
	}
	return guessModule(pkg)
}

// guessModule is used when neither the binary nor the source is available.
// It knows the layout of common hosts, and otherwise takes the first element.
func guessModule(pkg string) string {
	parts := strings.Split(pkg, "/")
	n := 1
	switch parts[0] {
	case "github.com", "gitlab.com", "bitbucket.org", "golang.org", "gopkg.in", "go.etcd.io", "cloud.google.com":
		n = 3
		if parts[0] == "gopkg.in" || parts[0] == "cloud.google.com" || parts[0] == "go.etcd.io" {
			n = 2
		}
	case "k8s.io", "sigs.k8s.io", "google.golang.org", "go.uber.org", "go.opentelemetry.io":
		n = 2
	}
	if n > len(parts) {
		n = len(parts)
	}
	return strings.Join(parts[:n], "/")
}

// fromBuildInfo reads the module list Go records in a binary.
func fromBuildInfo(bin string) (*Modules, error) {
	bi, err := buildinfo.ReadFile(bin)
	if err != nil {
		return nil, fmt.Errorf("reading module list from %s: %w", bin, err)
	}
	ms := &Modules{Main: bi.Main.Path, Source: "the binary's build info"}
	if bi.Main.Path != "" {
		ms.Paths = append(ms.Paths, bi.Main.Path)
	}
	// Package paths keep the required module path even when it's replaced.
	for _, d := range bi.Deps {
		ms.Paths = append(ms.Paths, d.Path)
	}
	return ms, nil
}

// fromVersionM reads the text that `go version -m BINARY` prints, for when
// only a saved copy of it is at hand.
func fromVersionM(r io.Reader) (*Modules, error) {
	ms := &Modules{Source: "go version -m output"}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "mod":
			ms.Main = f[1]
			ms.Paths = append(ms.Paths, f[1])
		case "dep":
			ms.Paths = append(ms.Paths, f[1])
		}
	}
	if ms.Main == "" && len(ms.Paths) == 0 {
		return nil, errors.New("no mod or dep lines found; expected the output of go version -m")
	}
	return ms, sc.Err()
}

// listPkg is the part of `go list -json` output used here.
type listPkg struct {
	ImportPath string
	Name       string
	Imports    []string
	Module     *struct{ Path string }
	Error      *struct{ Err string }
}

// Graph is the import graph of a main package, from go list.
type Graph struct {
	Root    string
	Imports map[string][]string
	Module  map[string]string
}

// loadGraph runs go list offline in dir. It never downloads or builds.
func loadGraph(dir, pkg string) (*Graph, error) {
	cmd := exec.Command("go", "list", "-e", "-deps", "-json=ImportPath,Name,Imports,Module,Error", pkg)
	cmd.Dir = dir
	// -mod=readonly so go.mod and go.sum are never changed.
	mod := "-mod=readonly"
	if _, err := os.Stat(filepath.Join(dir, "vendor", "modules.txt")); err == nil {
		mod = "-mod=vendor"
	}
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS="+mod)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list in %s failed: %v\n%s", dir, err, strings.TrimSpace(stderr.String()))
	}
	g := &Graph{Imports: map[string][]string{}, Module: map[string]string{}}
	dec := json.NewDecoder(bytes.NewReader(out))
	var errs []string
	for {
		var p listPkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("reading go list output: %w", err)
		}
		if p.Error != nil {
			errs = append(errs, p.Error.Err)
		}
		g.Imports[p.ImportPath] = p.Imports
		if p.Module != nil {
			g.Module[p.ImportPath] = p.Module.Path
		}
		// With -deps the named package comes last.
		g.Root = p.ImportPath
	}
	if len(errs) > 0 {
		msg := errs[0]
		if strings.Contains(msg, "module lookup disabled") || strings.Contains(msg, "no required module") || strings.Contains(msg, "missing go.sum") {
			msg += "\nThe module cache is missing modules. Run `go mod download` in " + dir + " (it uses the network), then try again."
		}
		return nil, fmt.Errorf("go list could not load %s: %s", pkg, msg)
	}
	return g, nil
}

// chains finds the shortest import path from the root to each package.
func (g *Graph) chains() map[string][]string {
	prev := map[string]string{g.Root: ""}
	queue := []string{g.Root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		imps := append([]string(nil), g.Imports[cur]...)
		sort.Strings(imps)
		for _, imp := range imps {
			if _, ok := prev[imp]; ok {
				continue
			}
			prev[imp] = cur
			queue = append(queue, imp)
		}
	}
	out := map[string][]string{}
	for pkg := range prev {
		var c []string
		for p := pkg; p != ""; p = prev[p] {
			c = append([]string{p}, c...)
		}
		out[pkg] = c
	}
	return out
}

// tracePath turns a go list import path into the name inittrace prints. The
// main package is printed as "main", and vendored paths drop the vendor
// prefix.
func tracePath(p, root string) string {
	if p == root {
		return "main"
	}
	if i := strings.LastIndex(p, "/vendor/"); i >= 0 {
		return p[i+len("/vendor/"):]
	}
	return strings.TrimPrefix(p, "vendor/")
}

// Change compares one package between a base and a new profile.
type Change struct {
	Path    string   `json:"path"`
	Module  string   `json:"module"`
	BaseMS  float64  `json:"base_clock_ms"`
	NewMS   float64  `json:"new_clock_ms"`
	DeltaMS float64  `json:"delta_ms"`
	IsNew   bool     `json:"new"`
	Chain   []string `json:"chain,omitempty"`
}

// slowerRatio is how much slower a package must get, besides minMS, to be
// listed. Init clocks of the same binary vary a lot between runs, so smaller
// changes are mostly noise.
const slowerRatio = 1.5

// Compare lists packages that are new, or slower by at least minMS and by at
// least half again their base time.
func Compare(base, cur *Profile, minMS float64) []Change {
	b := map[string]*Package{}
	for _, p := range base.Packages {
		b[p.Path] = p
	}
	var out []Change
	for _, p := range cur.Packages {
		old, ok := b[p.Path]
		c := Change{Path: p.Path, Module: p.Module, NewMS: p.ClockMS, IsNew: !ok, Chain: p.Chain}
		if ok {
			c.BaseMS = old.ClockMS
		}
		c.DeltaMS = round(c.NewMS - c.BaseMS)
		if c.DeltaMS >= minMS && (c.IsNew || c.NewMS >= slowerRatio*c.BaseMS) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DeltaMS > out[j].DeltaMS })
	return out
}

// Report is the full result, also printed with --json.
type Report struct {
	Version      string   `json:"version"`
	ModuleSource string   `json:"module_source"`
	MinMS        float64  `json:"min_ms"`
	Profile      *Profile `json:"profile"`
	Base         *Profile `json:"base,omitempty"`
	Changes      []Change `json:"changes,omitempty"`
	OverBudget   bool     `json:"over_budget,omitempty"`
	BudgetMS     float64  `json:"budget_ms,omitempty"`
	MainModule   string   `json:"main_module,omitempty"`
	Notes        []string `json:"notes,omitempty"`
	modOf        func(string) string
}

func (r *Report) chain(c []string) string {
	return fmtChain(c, r.MainModule, r.modOf)
}

// fmtMS prints a measured time. Init clocks are noisy below 0.1 ms, so one
// decimal is enough.
func fmtMS(x float64) string {
	return strconv.FormatFloat(x, 'f', 1, 64) + " ms"
}

// fmtLimit prints a threshold the user gave, as given.
func fmtLimit(x float64) string {
	return strconv.FormatFloat(x, 'f', -1, 64) + " ms"
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// fmtChain shortens an import chain to the steps that matter: main, the
// last package of the main module, and the packages outside it, with the
// middle of a long outside part left out.
func fmtChain(c []string, mainMod string, modOf func(string) string) string {
	if len(c) < 2 {
		return ""
	}
	last := 0
	for i, p := range c {
		if p == "main" || modOf(p) == mainMod {
			last = i
		}
	}
	out := []string{c[0]}
	from := last
	if last == len(c)-1 && last > 1 {
		// The package is in the main module: show what imports it.
		from = last - 1
	}
	if from > 1 {
		out = append(out, "...")
	}
	for i := max(from, 1); i <= last; i++ {
		out = append(out, c[i])
	}
	outside := c[last+1:]
	if len(outside) > 4 {
		outside = append(append(append([]string(nil), outside[:2]...), "..."), outside[len(outside)-1])
	}
	return strings.Join(append(out, outside...), " -> ")
}

// Print writes the report for a person or an agent to read.
func Print(w io.Writer, r *Report, top int) {
	p := r.Profile
	runs := "1 run"
	if p.Runs > 1 {
		runs = fmt.Sprintf("median of %d runs", p.Runs)
	}
	fmt.Fprintf(w, "%s ran init work, %s of init clock in total, %s allocated (%s).\n",
		plural(len(p.Packages), "package"), fmtMS(p.TotalMS), fmtBytes(p.Bytes), runs)
	fmt.Fprintf(w, "The last init finished %s after the process started.\n", fmtMS(p.DoneMS))
	fmt.Fprintf(w, "Modules from %s.\n", r.ModuleSource)

	if r.Base != nil {
		printCompare(w, r)
	} else {
		printProfile(w, r, top)
	}
	if r.BudgetMS > 0 {
		if r.OverBudget {
			fmt.Fprintf(w, "\nOver budget: init clock %s is more than %s.\n", fmtMS(p.TotalMS), fmtLimit(r.BudgetMS))
		} else {
			fmt.Fprintf(w, "\nWithin budget: init clock %s is at most %s.\n", fmtMS(p.TotalMS), fmtLimit(r.BudgetMS))
		}
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "\nNote: %s\n", n)
	}
}

func printProfile(w io.Writer, r *Report, top int) {
	p := r.Profile
	var slow []*Package
	for _, pk := range p.Packages {
		if pk.ClockMS >= r.MinMS {
			slow = append(slow, pk)
		}
	}
	if len(slow) == 0 {
		fmt.Fprintf(w, "\nNo package's init took %s or more.\n", fmtLimit(r.MinMS))
		return
	}
	fmt.Fprintf(w, "\nModules by init clock:\n")
	shown := 0
	for _, m := range p.Modules {
		if m.ClockMS < r.MinMS || shown == top {
			break
		}
		shown++
		share := 0.0
		if p.TotalMS > 0 {
			share = 100 * m.ClockMS / p.TotalMS
		}
		fmt.Fprintf(w, "  %8s %4.0f%% %9s  %s (%s)\n", fmtMS(m.ClockMS), share, fmtBytes(m.Bytes), m.Path, plural(m.Packages, "package"))
	}
	fmt.Fprintf(w, "\n%s took %s or more:\n", plural(len(slow), "package"), fmtLimit(r.MinMS))
	for i, pk := range slow {
		if i == top {
			fmt.Fprintf(w, "  ... %d more, see --top or --json\n", len(slow)-top)
			break
		}
		fmt.Fprintf(w, "  %8s %9s  %s\n", fmtMS(pk.ClockMS), fmtBytes(pk.Bytes), pk.Path)
		if c := r.chain(pk.Chain); c != "" {
			fmt.Fprintf(w, "      imported by %s\n", c)
		}
	}
}

func printCompare(w io.Writer, r *Report) {
	b, p := r.Base, r.Profile
	fmt.Fprintf(w, "\nBase: %s, %s of init clock, last init finished at %s.\n",
		plural(len(b.Packages), "package"), fmtMS(b.TotalMS), fmtMS(b.DoneMS))
	fmt.Fprintf(w, "Change: %+.1f ms of init clock, %d packages added, %d removed.\n",
		p.TotalMS-b.TotalMS, countNew(b, p), countNew(p, b))
	if len(r.Changes) == 0 {
		fmt.Fprintf(w, "\nNo package is new or slower by %s or more.\n", fmtLimit(r.MinMS))
		return
	}
	fmt.Fprintf(w, "\n%s new or slower by %s or more:\n", plural(len(r.Changes), "package"), fmtLimit(r.MinMS))
	for _, c := range r.Changes {
		what := fmt.Sprintf("%s -> %s", fmtMS(c.BaseMS), fmtMS(c.NewMS))
		if c.IsNew {
			what = "new, " + fmtMS(c.NewMS)
		}
		fmt.Fprintf(w, "  %9s  %s (%s)\n", "+"+fmtMS(c.DeltaMS), c.Path, what)
		if s := r.chain(c.Chain); s != "" {
			fmt.Fprintf(w, "      imported by %s\n", s)
		}
	}
}

func countNew(base, cur *Profile) int {
	b := map[string]bool{}
	for _, p := range base.Packages {
		b[p.Path] = true
	}
	n := 0
	for _, p := range cur.Packages {
		if !b[p.Path] {
			n++
		}
	}
	return n
}

// collect runs a binary n times with inittrace on and returns the trace
// text. Each run is stopped after timeout; init work happens before main,
// so a program that keeps running still gives a full trace.
func collect(bin string, args []string, n int, timeout time.Duration, stderrCopy io.Writer) ([]byte, []string, error) {
	var all bytes.Buffer
	var notes []string
	killed := 0
	godebug := "inittrace=1"
	if old := os.Getenv("GODEBUG"); old != "" {
		godebug = old + ",inittrace=1"
	}
	for i := 0; i < n; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = append(os.Environ(), "GODEBUG="+godebug)
		var errBuf bytes.Buffer
		cmd.Stderr = &errBuf
		cmd.Stdout = io.Discard
		err := cmd.Run()
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancel()
		if timedOut {
			killed++
		} else if err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				return nil, nil, fmt.Errorf("running %s: %w", bin, err)
			}
		}
		if !lineRE.Match(errBuf.Bytes()) {
			return nil, nil, fmt.Errorf("%s printed no inittrace lines; is it a Go program built with Go 1.16 or newer?", bin)
		}
		for _, l := range strings.SplitAfter(errBuf.String(), "\n") {
			if lineRE.MatchString(l) {
				all.WriteString(l)
			} else if stderrCopy != nil && l != "" {
				io.WriteString(stderrCopy, l)
			}
		}
	}
	if killed > 0 {
		notes = append(notes, fmt.Sprintf("%d of %d runs were still running after %s and were stopped. Init work finishes before main starts, so their traces are complete.", killed, n, timeout))
	}
	return all.Bytes(), notes, nil
}

func readTraces(paths []string) ([]Run, error) {
	var runs []Run
	for _, p := range paths {
		var r io.Reader
		if p == "-" {
			r = os.Stdin
		} else {
			f, err := os.Open(p)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			r = f
		}
		rs, err := ParseTrace(r)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", p, err)
		}
		if len(rs) == 0 {
			return nil, fmt.Errorf("%s has no inittrace lines. Make one with: GODEBUG=inittrace=1 ./yourprogram --version 2> trace.txt", p)
		}
		runs = append(runs, rs...)
	}
	return runs, nil
}

const usage = `initblame: find which packages and modules make a Go program slow to start.

Usage:
  initblame [flags] TRACE...          read GODEBUG=inittrace=1 output (- for stdin)
  initblame run [flags] -- BINARY [ARGS...]
                                      run a Go binary with inittrace on and report

Make a trace with:  GODEBUG=inittrace=1 ./yourprogram --version 2> trace.txt

Flags:
`

func main() {
	os.Exit(Main(os.Args[1:], os.Stdout, os.Stderr))
}

// Main runs the command and returns the exit code: 0 for a report, 1 when
// --budget or --fail finds a problem, 2 for bad usage or input.
func Main(args []string, stdout, stderr io.Writer) int {
	runMode := len(args) > 0 && args[0] == "run"
	if runMode {
		args = args[1:]
	}
	fs := flag.NewFlagSet("initblame", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	minMS := fs.Float64("min", 1, "list packages whose init took at least this many ms (or grew by this much with --base)")
	top := fs.Int("top", 10, "show at most this many packages and modules")
	dir := fs.String("dir", "", "module root with the program's source, to show the import chain of each package (runs go list offline)")
	pkg := fs.String("pkg", "", "main package to load in --dir (default: the binary's main package, or .)")
	bin := fs.String("bin", "", "the Go binary that made the trace, to read its module list")
	verM := fs.String("modules", "", "a file with saved `go version -m BINARY` output, to read the module list")
	base := fs.String("base", "", "an earlier trace file to compare with, such as one from before a dependency upgrade")
	budget := fs.Float64("budget", 0, "exit 1 if total init clock is more than this many ms")
	fail := fs.Bool("fail", false, "with --base, exit 1 if any package is new or slower by --min or more")
	asJSON := fs.Bool("json", false, "print the full result as JSON")
	n := fs.Int("n", 5, "run: how many times to run the binary")
	timeout := fs.Duration("timeout", 10*time.Second, "run: stop each run after this long")
	save := fs.String("save", "", "run: also write the raw trace to this file")
	showVersion := fs.Bool("version", false, "print the version")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "initblame", version)
		return 0
	}
	rest := fs.Args()
	fail2 := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "initblame: "+format+"\n", a...)
		return 2
	}
	if *minMS < 0 || *top < 1 || *n < 1 {
		return fail2("--min must be 0 or more, and --top and -n at least 1")
	}

	var runs []Run
	var notes []string
	if runMode {
		if len(rest) == 0 {
			return fail2("run needs a binary: initblame run -- ./yourprogram --version")
		}
		path, err := exec.LookPath(rest[0])
		if err != nil {
			return fail2("%v", err)
		}
		if *bin == "" {
			*bin = path
		}
		raw, ns, err := collect(path, rest[1:], *n, *timeout, nil)
		if err != nil {
			return fail2("%v", err)
		}
		notes = append(notes, ns...)
		if *save != "" {
			if err := os.WriteFile(*save, raw, 0o644); err != nil {
				return fail2("%v", err)
			}
		}
		runs, err = ParseTrace(bytes.NewReader(raw))
		if err != nil {
			return fail2("%v", err)
		}
	} else {
		if len(rest) == 0 {
			fs.Usage()
			return 2
		}
		var err error
		runs, err = readTraces(rest)
		if err != nil {
			return fail2("%v", err)
		}
	}

	mods := &Modules{Source: "package paths (a guess; pass --bin, --modules or --dir for exact modules)"}
	switch {
	case *bin != "":
		m, err := fromBuildInfo(*bin)
		if err != nil {
			return fail2("%v", err)
		}
		mods = m
	case *verM != "":
		f, err := os.Open(*verM)
		if err != nil {
			return fail2("%v", err)
		}
		m, err := fromVersionM(f)
		f.Close()
		if err != nil {
			return fail2("%s: %v", *verM, err)
		}
		mods = m
	}

	var graph *Graph
	if *dir != "" {
		p := *pkg
		if p == "" && mods.Main != "" && *bin != "" {
			if bi, err := buildinfo.ReadFile(*bin); err == nil && bi.Path != "" {
				p = bi.Path
			}
		}
		if p == "" {
			p = "."
		}
		g, err := loadGraph(*dir, p)
		if err != nil {
			return fail2("%v", err)
		}
		graph = g
		if *bin == "" && *verM == "" {
			mods = &Modules{Source: "go list in " + *dir}
			seen := map[string]bool{}
			for imp, m := range g.Module {
				if imp == g.Root {
					mods.Main = m
				}
				if !seen[m] {
					seen[m] = true
					mods.Paths = append(mods.Paths, m)
				}
			}
		}
	}

	addChains := func(p *Profile) {
		if graph == nil {
			return
		}
		ch := graph.chains()
		byTrace := map[string][]string{}
		for imp, c := range ch {
			tc := make([]string, len(c))
			for i, x := range c {
				tc[i] = tracePath(x, graph.Root)
			}
			byTrace[tracePath(imp, graph.Root)] = tc
		}
		missing := 0
		for _, pk := range p.Packages {
			if c, ok := byTrace[pk.Path]; ok {
				pk.Chain = c
			} else if !isStd(pk.Path) || pk.Path == "main" {
				missing++
			}
		}
		if missing > 0 {
			notes = append(notes, fmt.Sprintf("%s in the trace aren't imported by %s in %s. Check that --dir and --pkg match the program that made the trace.", plural(missing, "package"), graph.Root, *dir))
		}
	}

	rep := &Report{Version: version, ModuleSource: mods.Source, MinMS: *minMS, MainModule: mods.Main, modOf: mods.Of}
	rep.Profile = Summarize(runs, mods.Of)
	addChains(rep.Profile)
	if rep.Profile.Runs == 1 {
		notes = append(notes, "Init times from one run are noisy. Run the program several times into one file, or use initblame run, to get medians.")
	}
	if *base != "" {
		bruns, err := readTraces([]string{*base})
		if err != nil {
			return fail2("%v", err)
		}
		rep.Base = Summarize(bruns, mods.Of)
		rep.Changes = Compare(rep.Base, rep.Profile, *minMS)
	}
	if *budget > 0 {
		rep.BudgetMS = *budget
		rep.OverBudget = rep.Profile.TotalMS > *budget
	}
	rep.Notes = notes

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return fail2("%v", err)
		}
	} else {
		Print(stdout, rep, *top)
	}
	if rep.OverBudget || (*fail && len(rep.Changes) > 0) {
		return 1
	}
	return 0
}
