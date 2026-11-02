//go:build lazy

// Package geodata is the same table as geodata.go, built on first use. The
// lazy build tag shows the fix: init does no work, and the cost moves to the
// first call to Region.
package geodata

import "sync"

var table = sync.OnceValue(build)

// Region returns the region code for a numeric zone.
func Region(zone int) string { return table()[zone] }
