package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Two runs of the same program, with other stderr output mixed in, as
// written by `for i in 1 2; do GODEBUG=inittrace=1 ./prog; done 2> trace.txt`.
const twoRuns = `init internal/bytealg @0 ms, 0 ms clock, 0 bytes, 0 allocs
init runtime @0.016 ms, 0.053 ms clock, 0 bytes, 0 allocs
init gopkg.in/yaml%2ev3 @0.5 ms, 0.4 ms clock, 2048 bytes, 10 allocs
init github.com/acme/geo/table @0.9 ms, 12 ms clock, 3000000 bytes, 4000 allocs
init main @13 ms, 0.002 ms clock, 0 bytes, 0 allocs
some log line the program printed
init internal/bytealg @0 ms, 0 ms clock, 0 bytes, 0 allocs
init runtime @0.02 ms, 0.06 ms clock, 0 bytes, 0 allocs
init gopkg.in/yaml%2ev3 @0.6 ms, 0.6 ms clock, 2048 bytes, 10 allocs
init github.com/acme/geo/table @1.2 ms, 20 ms clock, 3000000 bytes, 4000 allocs
init main @21.3 ms, 0.003 ms clock, 0 bytes, 0 allocs
`

func TestParseTraceSplitsRuns(t *testing.T) {
	runs, err := ParseTrace(strings.NewReader(twoRuns))
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || len(runs[0]) != 5 || len(runs[1]) != 5 {
		t.Fatalf("got %d runs, want 2 of 5 lines: %+v", len(runs), runs)
	}
	got := runs[0][3]
	want := Init{Package: "github.com/acme/geo/table", StartMS: 0.9, ClockMS: 12, Bytes: 3000000, Allocs: 4000}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if runs[1][2].Package != "gopkg.in/yaml.v3" {
		t.Errorf("escaped dot not undone: %q", runs[1][2].Package)
	}
}

func TestParseTraceIgnoresPrefixes(t *testing.T) {
	// Log collectors often add a timestamp in front of each stderr line.
	in := "2026-11-02T10:00:00Z stderr F init os @0.6 ms, 0.013 ms clock, 416 bytes, 9 allocs\n"
	runs, err := ParseTrace(strings.NewReader(in))
	if err != nil || len(runs) != 1 || runs[0][0].Package != "os" {
		t.Fatalf("got %+v, %v", runs, err)
	}
}

func TestUnescapeSymbol(t *testing.T) {
	for in, want := range map[string]string{
		"go.opentelemetry.io/otel/semconv/v1%2e37%2e0": "go.opentelemetry.io/otel/semconv/v1.37.0",
		"plain/path":  "plain/path",
		"odd%zz":      "odd%zz",
		"trailing%2":  "trailing%2",
		"a%25b":       "a%b",
		"x/y%2etxt/z": "x/y.txt/z",
	} {
		if got := unescapeSymbol(in); got != want {
			t.Errorf("unescapeSymbol(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSummarizeTakesMedians(t *testing.T) {
	runs, _ := ParseTrace(strings.NewReader(twoRuns))
	ms := &Modules{Main: "example.com/prog", Paths: []string{"example.com/prog", "github.com/acme/geo", "gopkg.in/yaml.v3"}}
	p := Summarize(runs, ms.Of)
	if p.Runs != 2 {
		t.Errorf("runs = %d", p.Runs)
	}
	top := p.Packages[0]
	if top.Path != "github.com/acme/geo/table" || top.ClockMS != 16 || top.Module != "github.com/acme/geo" {
		t.Errorf("top package = %+v", top)
	}
	if p.Modules[0].Path != "github.com/acme/geo" || p.Modules[0].Packages != 1 {
		t.Errorf("top module = %+v", p.Modules[0])
	}
	// Median of the end of the last init: (13.002 + 21.303) / 2.
	if p.DoneMS != 17.153 {
		t.Errorf("DoneMS = %v", p.DoneMS)
	}
	var mainMod string
	for _, pk := range p.Packages {
		if pk.Path == "main" {
			mainMod = pk.Module
		}
	}
	if mainMod != "example.com/prog" {
		t.Errorf("main package module = %q", mainMod)
	}
}

func TestModuleOf(t *testing.T) {
	ms := &Modules{Paths: []string{"github.com/a/b", "github.com/a/b/v2", "github.com/a/bc"}}
	for pkg, want := range map[string]string{
		"github.com/a/b/x":                    "github.com/a/b",
		"github.com/a/b/v2/x":                 "github.com/a/b/v2",
		"github.com/a/bc":                     "github.com/a/bc",
		"net/http":                            "std",
		"vendor/golang.org/x/net/http2/hpack": "std",
		"k8s.io/client-go/rest":               "k8s.io/client-go",
		"github.com/z/y/w":                    "github.com/z/y",
		"gopkg.in/yaml.v3":                    "gopkg.in/yaml.v3",
		"go.uber.org/zap/zapcore":             "go.uber.org/zap",
		"example.com":                         "example.com",
	} {
		if got := ms.Of(pkg); got != want {
			t.Errorf("Of(%q) = %q, want %q", pkg, got, want)
		}
	}
}

func TestFromVersionM(t *testing.T) {
	in := `bin/prog: go1.27.3
	path	example.com/prog/cmd/prog
	mod	example.com/prog	(devel)
	dep	github.com/acme/geo	v1.2.0	h1:abc=
	dep	golang.org/x/text	v0.20.0	h1:def=
	=>	golang.org/x/text	v0.21.0	h1:ghi=
	build	-compiler=gc
`
	ms, err := fromVersionM(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"example.com/prog", "github.com/acme/geo", "golang.org/x/text"}
	if ms.Main != "example.com/prog" || !reflect.DeepEqual(ms.Paths, want) {
		t.Errorf("got %+v", ms)
	}
	if _, err := fromVersionM(strings.NewReader("not version output\n")); err == nil {
		t.Error("want an error for text without mod or dep lines")
	}
}

func TestFromBuildInfoReadsThisTestBinary(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ms, err := fromBuildInfo(exe)
	if err != nil {
		t.Fatal(err)
	}
	if ms.Main != "github.com/GillMacedo/initblame" {
		t.Errorf("main module = %q", ms.Main)
	}
	if _, err := fromBuildInfo(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("want an error for a missing binary")
	}
}

func prof(pkgs map[string]float64) *Profile {
	p := &Profile{}
	for path, ms := range pkgs {
		p.Packages = append(p.Packages, &Package{Path: path, ClockMS: ms})
	}
	return p
}

func TestCompareSkipsNoise(t *testing.T) {
	base := prof(map[string]float64{"a": 10, "b": 2, "c": 0.5})
	cur := prof(map[string]float64{"a": 12, "b": 4, "c": 0.6, "d": 3, "e": 0.2})
	got := Compare(base, cur, 1)
	var names []string
	for _, c := range got {
		names = append(names, c.Path)
	}
	// a grew by 2 ms but only 20%, which is within run-to-run noise. c and e
	// are under 1 ms.
	if want := []string{"d", "b"}; !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
	if !got[0].IsNew || got[1].IsNew {
		t.Errorf("IsNew wrong: %+v", got)
	}
}

func TestFmtChain(t *testing.T) {
	ms := &Modules{Main: "m.com/app", Paths: []string{"m.com/app"}}
	for _, tc := range []struct {
		chain []string
		want  string
	}{
		{[]string{"main"}, ""},
		{[]string{"main", "x.com/lib"}, "main -> x.com/lib"},
		{[]string{"main", "m.com/app/a", "m.com/app/b", "x.com/lib"}, "main -> ... -> m.com/app/b -> x.com/lib"},
		{[]string{"main", "m.com/app/a", "m.com/app/b"}, "main -> m.com/app/a -> m.com/app/b"},
		{[]string{"main", "m.com/app/a", "m.com/app/b", "m.com/app/c"}, "main -> ... -> m.com/app/b -> m.com/app/c"},
		{[]string{"main", "x.com/1", "x.com/2", "x.com/3", "x.com/4", "x.com/5"}, "main -> x.com/1 -> x.com/2 -> ... -> x.com/5"},
	} {
		if got := fmtChain(tc.chain, ms.Main, ms.Of); got != tc.want {
			t.Errorf("fmtChain(%v) = %q, want %q", tc.chain, got, tc.want)
		}
	}
}

func TestTracePath(t *testing.T) {
	if got := tracePath("example.com/app/cmd/x", "example.com/app/cmd/x"); got != "main" {
		t.Error(got)
	}
	if got := tracePath("example.com/app/vendor/github.com/a/b", "r"); got != "github.com/a/b" {
		t.Error(got)
	}
	if got := tracePath("vendor/golang.org/x/net/idna", "r"); got != "golang.org/x/net/idna" {
		t.Error(got)
	}
}

func writeTrace(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "trace.txt")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Main(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestMainReport(t *testing.T) {
	tr := writeTrace(t, twoRuns)
	code, out, _ := run(tr)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		"5 packages ran init work, 16.6 ms of init clock in total",
		"median of 2 runs",
		"16.0 ms",
		"github.com/acme/geo/table",
		"Modules from package paths (a guess",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "yaml") {
		t.Errorf("a 0.5 ms package was listed with --min 1:\n%s", out)
	}
}

func TestMainQuiet(t *testing.T) {
	tr := writeTrace(t, twoRuns)
	code, out, _ := run("--min", "50", tr)
	if code != 0 || !strings.Contains(out, "No package's init took 50 ms or more.") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestMainBudget(t *testing.T) {
	tr := writeTrace(t, twoRuns)
	if code, out, _ := run("--budget", "10", tr); code != 1 || !strings.Contains(out, "Over budget") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if code, out, _ := run("--budget", "100", tr); code != 0 || !strings.Contains(out, "Within budget") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestMainCompareFail(t *testing.T) {
	base := writeTrace(t, strings.ReplaceAll(twoRuns, "github.com/acme/geo/table", "github.com/acme/geo/small"))
	cur := writeTrace(t, twoRuns)
	code, out, _ := run("--base", base, "--fail", cur)
	if code != 1 || !strings.Contains(out, "github.com/acme/geo/table (new, 16.0 ms)") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	code, out, _ = run("--base", cur, "--fail", cur)
	if code != 0 || !strings.Contains(out, "No package is new or slower") {
		t.Errorf("same trace: exit %d:\n%s", code, out)
	}
}

func TestMainJSON(t *testing.T) {
	tr := writeTrace(t, twoRuns)
	code, out, _ := run("--json", tr)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var r Report
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Profile.Runs != 2 || r.Profile.Packages[0].Path != "github.com/acme/geo/table" {
		t.Errorf("got %+v", r.Profile)
	}
}

func TestMainErrors(t *testing.T) {
	empty := writeTrace(t, "no trace here\n")
	for _, args := range [][]string{
		{},
		{empty},
		{filepath.Join(t.TempDir(), "missing.txt")},
		{"--top", "0", empty},
		{"run"},
		{"--modules", empty, writeTrace(t, twoRuns)},
	} {
		if code, _, _ := run(args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	_, _, errOut := run(empty)
	if !strings.Contains(errOut, "GODEBUG=inittrace=1") {
		t.Errorf("empty trace error should say how to make one: %s", errOut)
	}
}
