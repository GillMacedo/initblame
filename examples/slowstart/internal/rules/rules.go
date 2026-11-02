// Package rules compiles its patterns into package variables, so every
// program that imports it pays for them at startup.
package rules

import (
	"fmt"
	"regexp"
)

var patterns = compileAll()

func compileAll() []*regexp.Regexp {
	var out []*regexp.Regexp
	for i := 0; i < 300; i++ {
		out = append(out, regexp.MustCompile(fmt.Sprintf(`^(?i)order-%d-[a-z]{2,8}-(\d+)(\.(csv|json|xml))?$`, i)))
	}
	return out
}

// Match reports whether name matches any pattern.
func Match(name string) bool {
	for _, p := range patterns {
		if p.MatchString(name) {
			return true
		}
	}
	return false
}
