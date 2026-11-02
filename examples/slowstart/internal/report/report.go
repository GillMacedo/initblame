// Package report formats an order line. It imports geodata for one lookup.
package report

import (
	"fmt"

	"example.com/geodata"
	"example.com/slowstart/internal/rules"
)

// Line formats one order.
func Line(name string, zone int) string {
	return fmt.Sprintf("%s region=%s known=%v", name, geodata.Region(zone), rules.Match(name))
}
