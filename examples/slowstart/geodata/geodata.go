//go:build !lazy

// Package geodata is a made-up dependency that builds a large lookup table
// when it's imported, the way some real data packages do.
package geodata

var table = build()

// Region returns the region code for a numeric zone.
func Region(zone int) string { return table[zone] }
