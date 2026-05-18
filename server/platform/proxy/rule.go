package proxy

import (
	"bufio"
	"net"
	"strings"
	"sync"
)

// RuleSet holds a set of MITM rules that determine which hosts should have
// their HTTPS traffic intercepted. Rules are loaded from a simple text format:
//
//   - - match all hosts (MITM everything)
//     *.domain.com - match domain.com and all subdomains
//     domain.com   - match exactly domain.com
//     !domain.com  - negate: do NOT MITM domain.com
//
// Blank lines and lines starting with # are ignored.
// Rules are evaluated in order; the last matching rule wins.
type RuleSet struct {
	mu    sync.RWMutex
	rules []rule
}

type rule struct {
	raw        string
	isNeg      bool // negation rule (prefixed with !)
	isWildcard bool // *.domain form
	isAll      bool // matches everything
	domain     string
}

// Load parses the rule text and replaces the active rule set.
func (r *RuleSet) Load(text string) error {
	scanner := bufio.NewScanner(strings.NewReader(text))
	var rules []rule

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		isNeg := false
		if strings.HasPrefix(line, "!") {
			isNeg = true
			line = strings.TrimSpace(line[1:])
			if line == "" {
				continue
			}
		}

		if line == "*" {
			rules = append(rules, rule{raw: "*", isAll: true, isNeg: isNeg})
			continue
		}

		isWildcard := false
		domain := line
		if strings.HasPrefix(line, "*.") {
			isWildcard = true
			domain = line[2:]
		}

		rules = append(rules, rule{
			raw:        line,
			isNeg:      isNeg,
			isWildcard: isWildcard,
			domain:     strings.ToLower(domain),
		})
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	r.rules = rules
	r.mu.Unlock()
	return nil
}

// ShouldMITM returns true if the host should have its traffic decrypted.
// The host may include a port (e.g. "example.com:443"); it is stripped
// before matching.
func (r *RuleSet) ShouldMITM(host string) bool {
	h := host
	if strings.HasPrefix(h, "[") {
		if idx := strings.LastIndex(h, "]"); idx != -1 {
			h = h[:idx+1]
		}
	}
	if hp, _, err := net.SplitHostPort(host); err == nil {
		h = hp
	}
	h = strings.ToLower(strings.Trim(h, "[]"))

	r.mu.RLock()
	defer r.mu.RUnlock()

	action := false
	for _, rl := range r.rules {
		if rl.isAll {
			action = !rl.isNeg
			continue
		}
		if rl.isWildcard {
			if h == rl.domain || strings.HasSuffix(h, "."+rl.domain) {
				action = !rl.isNeg
			}
			continue
		}
		if h == rl.domain {
			action = !rl.isNeg
		}
	}
	return action
}
