package process

import (
	"regexp"
	"sync"
)

// patternCache caches compiled regexps to avoid recompiling on every line.
var patternCache = struct {
	mu    sync.RWMutex
	cache map[string]*regexp.Regexp
}{
	cache: make(map[string]*regexp.Regexp),
}

// compilePattern returns a compiled regexp for the given pattern string,
// fetching from the cache if available. Returns nil if the pattern is empty
// or invalid.
func compilePattern(pattern string) *regexp.Regexp {
	if pattern == "" {
		return nil
	}

	patternCache.mu.RLock()
	re, ok := patternCache.cache[pattern]
	patternCache.mu.RUnlock()

	if ok {
		return re
	}

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		// Invalid regex — treat as no match rather than panicking.
		return nil
	}

	patternCache.mu.Lock()
	patternCache.cache[pattern] = compiled
	patternCache.mu.Unlock()

	return compiled
}

// matchPattern reports whether line contains a match for the given regex pattern.
// Returns false if the pattern is empty or fails to compile.
func matchPattern(pattern, line string) bool {
	re := compilePattern(pattern)
	if re == nil {
		return false
	}
	return re.MatchString(line)
}
