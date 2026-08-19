// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
// Package numrules applies per-carrier number translation/manipulation rules to the A-number (sender ID)
// and B-number (destination) just before a message is handed to a carrier. Rules are an ordered list
// applied left to right, so operators compose them (for example: strip '+', strip leading zeros, then
// add a country prefix). All rules are pure string transforms; regex uses Go RE2 (linear time, no
// catastrophic backtracking), so untrusted operator patterns cannot cause a ReDoS.
package numrules

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// RuleType enumerates the supported transforms.
type RuleType string

const (
	StripLeadingZeros RuleType = "strip_leading_zeros" // remove a leading run of '0'
	StripLeadingPlus  RuleType = "strip_leading_plus"  // remove a leading run of '+'
	StripDigits       RuleType = "strip_digits"        // remove every digit
	StripAlpha        RuleType = "strip_alpha"         // remove every letter
	StripSymbols      RuleType = "strip_symbols"       // remove everything that is not a letter or digit
	AddPrefix         RuleType = "add_prefix"          // prepend Prefix when the value does not already start with it
	RegexReplace      RuleType = "regex_replace"       // replace Pattern with Replacement ($1 / ${name} capture substitution)
)

// Rule is one transform. Only the fields relevant to Type are used.
type Rule struct {
	Type        RuleType `json:"type"`
	Prefix      string   `json:"prefix,omitempty"`      // add_prefix
	Pattern     string   `json:"pattern,omitempty"`     // regex_replace
	Replacement string   `json:"replacement,omitempty"` // regex_replace
}

// Config is the per-carrier rule configuration: independent ordered lists for the A-number (sender ID)
// and the B-number (destination). It is what is persisted (JSON) on the carrier.
type Config struct {
	Sender      []Rule `json:"sender"`
	Destination []Rule `json:"destination"`
}

// Compiled is a validated, ready-to-apply rule set (regexes pre-compiled). Build once per carrier at
// cache load, then Apply per message.
type Compiled struct {
	sender []compiledRule
	dest   []compiledRule
}

type compiledRule struct {
	rule Rule
	re   *regexp.Regexp
}

// Compile validates a Config and pre-compiles its regexes. A nil return with a nil error means "no
// rules" (Apply then returns the input unchanged). An invalid regex or an unknown rule type is an error,
// so misconfiguration is caught at save/load time, never per message.
func Compile(cfg Config) (*Compiled, error) {
	s, err := compileList(cfg.Sender)
	if err != nil {
		return nil, fmt.Errorf("sender rules: %w", err)
	}
	d, err := compileList(cfg.Destination)
	if err != nil {
		return nil, fmt.Errorf("destination rules: %w", err)
	}
	if len(s) == 0 && len(d) == 0 {
		return nil, nil
	}
	return &Compiled{sender: s, dest: d}, nil
}

func compileList(rules []Rule) ([]compiledRule, error) {
	out := make([]compiledRule, 0, len(rules))
	for i, r := range rules {
		cr := compiledRule{rule: r}
		switch r.Type {
		case StripLeadingZeros, StripLeadingPlus, StripDigits, StripAlpha, StripSymbols:
			// no params
		case AddPrefix:
			if r.Prefix == "" {
				return nil, fmt.Errorf("rule %d (add_prefix): prefix is required", i+1)
			}
		case RegexReplace:
			re, err := regexp.Compile(r.Pattern)
			if err != nil {
				return nil, fmt.Errorf("rule %d (regex_replace): invalid pattern %q: %w", i+1, r.Pattern, err)
			}
			cr.re = re
		default:
			return nil, fmt.Errorf("rule %d: unknown type %q", i+1, r.Type)
		}
		out = append(out, cr)
	}
	return out, nil
}

// Sender applies the A-number rules; Destination applies the B-number rules. Both are safe on a nil
// receiver (returns the input unchanged), so callers need no nil check.
func (c *Compiled) Sender(in string) string      { return apply(cListOf(c, true), in) }
func (c *Compiled) Destination(in string) string { return apply(cListOf(c, false), in) }

func cListOf(c *Compiled, sender bool) []compiledRule {
	if c == nil {
		return nil
	}
	if sender {
		return c.sender
	}
	return c.dest
}

func apply(rules []compiledRule, in string) string {
	out := in
	for _, cr := range rules {
		out = cr.apply(out)
	}
	return out
}

func (cr compiledRule) apply(in string) string {
	switch cr.rule.Type {
	case StripLeadingZeros:
		return strings.TrimLeft(in, "0")
	case StripLeadingPlus:
		return strings.TrimLeft(in, "+")
	case StripDigits:
		return removeFunc(in, unicode.IsDigit)
	case StripAlpha:
		return removeFunc(in, unicode.IsLetter)
	case StripSymbols:
		return removeFunc(in, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	case AddPrefix:
		if !strings.HasPrefix(in, cr.rule.Prefix) {
			return cr.rule.Prefix + in
		}
		return in
	case RegexReplace:
		if cr.re == nil {
			return in
		}
		return cr.re.ReplaceAllString(in, cr.rule.Replacement)
	default:
		return in
	}
}

// removeFunc drops every rune for which drop returns true.
func removeFunc(s string, drop func(rune) bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !drop(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValidRuleType reports whether t is a supported rule type (for form validation).
func ValidRuleType(t string) bool {
	switch RuleType(t) {
	case StripLeadingZeros, StripLeadingPlus, StripDigits, StripAlpha, StripSymbols, AddPrefix, RegexReplace:
		return true
	}
	return false
}
