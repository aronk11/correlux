package kubeconfig

import (
	"regexp"
	"strings"
)

// Classifier decides whether a context is production. Getting this wrong in the
// safe direction (extra confirmation) is cheap; getting it wrong in the unsafe
// direction is an outage, so unparseable patterns are dropped rather than
// silently matching everything.
type Classifier struct {
	patterns []*regexp.Regexp
	explicit map[string]struct{}
}

// NewClassifier compiles the given patterns (case-insensitively) and marks the
// listed context names as production unconditionally. Invalid patterns are
// returned as errors but do not prevent the remaining ones from working.
func NewClassifier(patterns, explicitContexts []string) (*Classifier, []error) {
	c := &Classifier{explicit: make(map[string]struct{}, len(explicitContexts))}
	var errs []error
	for _, p := range patterns {
		if strings.TrimSpace(p) == "" {
			continue
		}
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c.patterns = append(c.patterns, re)
	}
	for _, name := range explicitContexts {
		c.explicit[name] = struct{}{}
	}
	return c, errs
}

// DefaultClassifier uses the built-in production patterns.
func DefaultClassifier() *Classifier {
	c, _ := NewClassifier([]string{`(^|[-_./])(prod|prd|production|live)([-_./]|$)`}, nil)
	return c
}

// IsProduction reports whether any of the context identifiers look like
// production. The API server host is matched too, so a context named "eu" that
// points at api.prod.example.com is still flagged.
func (c *Classifier) IsProduction(contextName, clusterName, server string) bool {
	if c == nil {
		return false
	}
	if _, ok := c.explicit[contextName]; ok {
		return true
	}
	for _, candidate := range []string{contextName, clusterName, server} {
		if candidate == "" {
			continue
		}
		for _, re := range c.patterns {
			if re.MatchString(candidate) {
				return true
			}
		}
	}
	return false
}
