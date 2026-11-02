// Package tests builds the example programs, runs them with inittrace on
// through initblame the way the skill does, and checks the answers against
// what the programs really do at init. It also runs every `$ ` command in
// the README and compares the output.
package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var (
	root    string // repo root
	tool    string // built initblame binary
	binDir  string
	slow    string // examples/slowstart, eager geodata
	lazy    string // examples/slowstart with -tags lazy
	quick   string // examples/quickstart
	sleeper string // a program that never exits on its own
)

func TestMain(m *testing.M) {
	code, err := setup(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func setup(m *testing.M) (int, error) {
	var err error
	root, err = filepath.Abs("..")
	if err != nil {
		return 0, err
	}
	binDir, err = os.MkdirTemp("", "initblame-tests")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(binDir)
	tool = filepath.Join(binDir, "initblame")
	slow = filepath.Join(binDir, "slowstart")
	lazy = filepath.Join(binDir, "slowstart-lazy")
	quick = filepath.Join(binDir, "quickstart")
	sleeper = filepath.Join(binDir, "sleeper")
	sleepDir := filepath.Join(binDir, "sleepsrc")
	if err := os.MkdirAll(sleepDir, 0o755); err != nil {
		return 0, err
	}
	os.WriteFile(filepath.Join(sleepDir, "go.mod"), []byte("module example.com/sleeper\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(sleepDir, "main.go"), []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"time\"\n)\n\nfunc main() {\n\tfmt.Println(\"serving\")\n\ttime.Sleep(time.Minute)\n}\n"), 0o644)
	for _, b := range []struct{ dir, out, pkg, tags string }{
		{root, tool, "./plugin/scripts", ""},
		{filepath.Join(root, "examples/slowstart"), slow, "./cmd/slowstart", ""},
		{filepath.Join(root, "examples/slowstart"), lazy, "./cmd/slowstart", "lazy"},
		{filepath.Join(root, "examples/quickstart"), quick, ".", ""},
		{sleepDir, sleeper, ".", ""},
	} {
		args := []string{"build", "-o", b.out}
		if b.tags != "" {
			args = append(args, "-tags", b.tags)
		}
		cmd := exec.Command("go", append(args, b.pkg)...)
		cmd.Dir = b.dir
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			return 0, fmt.Errorf("building %s in %s: %v\n%s", b.pkg, b.dir, err, out)
		}
	}
	return m.Run(), nil
}

type report struct {
	ModuleSource string `json:"module_source"`
	Profile      struct {
		Runs     int
		Packages []struct {
			Path    string
			Module  string
			ClockMS float64 `json:"clock_ms"`
			Bytes   int64
			Runs    int
			Chain   []string
		}
	}
	Changes []struct {
		Path    string
		IsNew   bool    `json:"new"`
		DeltaMS float64 `json:"delta_ms"`
	}
	Notes []string
}

func initblame(t *testing.T, args ...string) (report, int, string) {
	t.Helper()
	cmd := exec.Command(tool, args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	var r report
	if json.Valid(out) {
		if err := json.Unmarshal(out, &r); err != nil {
			t.Fatal(err)
		}
	}
	return r, code, string(out) + stderr.String()
}

// The slowstart example builds a 100,000-entry map in geodata's init. It
// must come out on top, with the chain through internal/report.
func TestSlowDependencyFound(t *testing.T) {
	r, code, out := initblame(t, "run", "-n", "3", "--json", "--dir", "examples/slowstart", "--", slow, "--version")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if r.Profile.Runs != 3 || r.ModuleSource != "the binary's build info" {
		t.Errorf("runs %d, modules from %q", r.Profile.Runs, r.ModuleSource)
	}
	top := r.Profile.Packages[0]
	if top.Path != "example.com/geodata" || top.Module != "example.com/geodata" {
		t.Fatalf("slowest package = %+v", top)
	}
	if top.ClockMS < 2 || top.Bytes < 1<<20 {
		t.Errorf("geodata init looks too small to be the 100,000-entry table: %+v", top)
	}
	want := []string{"main", "example.com/slowstart/internal/report", "example.com/geodata"}
	if !reflect.DeepEqual(top.Chain, want) {
		t.Errorf("chain = %v, want %v", top.Chain, want)
	}
	var rules bool
	for _, p := range r.Profile.Packages {
		if p.Path == "example.com/slowstart/internal/rules" {
			rules = p.Module == "example.com/slowstart" && p.Bytes > 100<<10
		}
	}
	if !rules {
		t.Error("internal/rules, which compiles 300 regexps at init, is missing or has the wrong module")
	}
}

// Making geodata lazy removes its cost at init. Comparing the eager build
// against the lazy one as the base must flag geodata and fail.
func TestCompareFlagsRegression(t *testing.T) {
	base := filepath.Join(t.TempDir(), "lazy.txt")
	if _, code, out := initblame(t, "run", "-n", "3", "--save", base, "--", lazy, "--version"); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	lr, _, _ := initblame(t, "--json", base)
	for _, p := range lr.Profile.Packages {
		if p.Path == "example.com/geodata" && p.ClockMS >= 1 {
			t.Errorf("lazy geodata still takes %v ms at init", p.ClockMS)
		}
	}
	r, code, out := initblame(t, "run", "-n", "3", "--json", "--base", base, "--fail", "--", slow, "--version")
	if code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, out)
	}
	if len(r.Changes) == 0 || r.Changes[0].Path != "example.com/geodata" {
		t.Errorf("changes = %+v", r.Changes)
	}
}

// The healthy case: a small program that imports net/http and flag. It must
// stay quiet and within a budget. The 5 ms threshold leaves room for slow CI
// machines; on a laptop every package here takes well under 1 ms.
func TestHealthyProgramIsQuiet(t *testing.T) {
	_, code, out := initblame(t, "run", "-n", "3", "--min", "5", "--budget", "50", "--", quick, "--version")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "No package's init took 5 ms or more.") || !strings.Contains(out, "Within budget") {
		t.Errorf("output:\n%s", out)
	}
	// Comparing a program with itself finds nothing new.
	tr := filepath.Join(t.TempDir(), "q.txt")
	initblame(t, "run", "-n", "3", "--save", tr, "--", quick, "--version")
	_, code, out = initblame(t, "run", "-n", "3", "--base", tr, "--min", "5", "--fail", "--", quick, "--version")
	if code != 0 || !strings.Contains(out, "No package is new or slower") {
		t.Errorf("self-compare: exit %d:\n%s", code, out)
	}
}

// Every package the runtime reports in one run must be counted once.
func TestCountsMatchRuntime(t *testing.T) {
	cmd := exec.Command(quick, "--version")
	cmd.Env = append(os.Environ(), "GODEBUG=inittrace=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(stderr.String(), "init ")
	tr := filepath.Join(t.TempDir(), "one.txt")
	os.WriteFile(tr, stderr.Bytes(), 0o644)
	r, _, _ := initblame(t, "--json", tr)
	if lines < 20 || len(r.Profile.Packages) != lines {
		t.Errorf("runtime printed %d init lines, initblame counted %d packages", lines, len(r.Profile.Packages))
	}
}

// A program that keeps running is stopped after --timeout, and its trace is
// still complete because init runs before main.
func TestTimeoutStopsLongRunningProgram(t *testing.T) {
	r, code, out := initblame(t, "run", "-n", "1", "--timeout", "1s", "--json", "--", sleeper)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if len(r.Notes) == 0 || !strings.Contains(r.Notes[0], "still running after 1s") {
		t.Errorf("notes = %v", r.Notes)
	}
	var hasOS bool
	for _, p := range r.Profile.Packages {
		hasOS = hasOS || p.Path == "os"
	}
	if !hasOS || len(r.Profile.Packages) < 10 {
		t.Errorf("trace of the stopped program has %d packages, want os among 10 or more", len(r.Profile.Packages))
	}
}

func TestRunRejectsNonGoProgram(t *testing.T) {
	sh, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true command")
	}
	_, code, out := initblame(t, "run", "-n", "1", "--", sh)
	if code != 2 || !strings.Contains(out, "printed no inittrace lines") {
		t.Errorf("exit %d: %s", code, out)
	}
}

// TestReadmeCommands runs each `$ ` line in README.md from the repo root and
// checks the indented lines after it are the output.
func TestReadmeCommands(t *testing.T) {
	f, err := os.Open(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type example struct {
		cmd  string
		want []string
	}
	var exs []example
	sc := bufio.NewScanner(f)
	var cur *example
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "    $ "):
			exs = append(exs, example{cmd: strings.TrimPrefix(line, "    $ ")})
			cur = &exs[len(exs)-1]
		case cur != nil && (strings.HasPrefix(line, "    ") || line == ""):
			cur.want = append(cur.want, strings.TrimPrefix(line, "    "))
		default:
			cur = nil
		}
	}
	if len(exs) < 3 {
		t.Fatalf("found %d README examples, want at least 3", len(exs))
	}
	for _, ex := range exs {
		args := strings.Fields(ex.cmd)
		if len(args) < 3 || args[0] != "go" || args[1] != "run" || args[2] != "./plugin/scripts" {
			t.Errorf("README example %q doesn't run ./plugin/scripts", ex.cmd)
			continue
		}
		cmd := exec.Command(tool, args[3:]...)
		cmd.Dir = root
		out, _ := cmd.CombinedOutput()
		got := strings.TrimRight(string(out), "\n")
		want := strings.TrimRight(strings.Join(ex.want, "\n"), "\n")
		if got != want {
			t.Errorf("README example %q\n--- got\n%s\n--- want\n%s", ex.cmd, got, want)
		}
	}
}
