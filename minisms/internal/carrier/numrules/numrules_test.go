// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package numrules

import "testing"

func mustCompile(t *testing.T, cfg Config) *Compiled {
	t.Helper()
	c, err := Compile(cfg)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return c
}

func TestIndividualRules(t *testing.T) {
	cases := []struct {
		name string
		rule Rule
		in   string
		want string
	}{
		{"strip leading zeros", Rule{Type: StripLeadingZeros}, "00243812345", "243812345"},
		{"strip leading zeros none", Rule{Type: StripLeadingZeros}, "243", "243"},
		{"strip leading plus", Rule{Type: StripLeadingPlus}, "+243812345", "243812345"},
		{"strip digits", Rule{Type: StripDigits}, "MiniSMS123", "MiniSMS"},
		{"strip alpha", Rule{Type: StripAlpha}, "AB243CD", "243"},
		{"strip symbols", Rule{Type: StripSymbols}, "+243 (81) 234-5678", "243812345678"},
		{"strip symbols keeps alnum", Rule{Type: StripSymbols}, "Mini SMS!", "MiniSMS"},
		{"uppercase all alpha", Rule{Type: Uppercase}, "BrandX-90", "BRANDX-90"},
		{"uppercase leaves non-alpha", Rule{Type: Uppercase}, "+243abc", "+243ABC"},
		{"add prefix missing", Rule{Type: AddPrefix, Prefix: "243"}, "812345", "243812345"},
		{"add prefix present", Rule{Type: AddPrefix, Prefix: "243"}, "243812345", "243812345"},
		{"add plus missing", Rule{Type: AddPrefix, Prefix: "+"}, "243812345", "+243812345"},
		{"add plus present", Rule{Type: AddPrefix, Prefix: "+"}, "+243812345", "+243812345"},
		{"regex simple", Rule{Type: RegexReplace, Pattern: "^0", Replacement: "243"}, "0812345", "243812345"},
		{"regex capture group", Rule{Type: RegexReplace, Pattern: `^\+?0*(\d+)$`, Replacement: "00$1"}, "+00812345", "00812345"},
		{"regex named group", Rule{Type: RegexReplace, Pattern: `^(?P<cc>243)(?P<sn>\d+)$`, Replacement: "+${cc}-${sn}"}, "243812345", "+243-812345"},
		{"regex no match leaves input", Rule{Type: RegexReplace, Pattern: `^999`, Replacement: "X"}, "243812345", "243812345"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmp := mustCompile(t, Config{Sender: []Rule{c.rule}})
			if got := cmp.Sender(c.in); got != c.want {
				t.Fatalf("Sender(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestOrderedComposition(t *testing.T) {
	// A realistic chain: strip +, strip leading zeros, then force E.164 with a country code.
	cfg := Config{Destination: []Rule{
		{Type: StripLeadingPlus},
		{Type: StripLeadingZeros},
		{Type: AddPrefix, Prefix: "243"},
		{Type: AddPrefix, Prefix: "+"},
	}}
	cmp := mustCompile(t, cfg)
	cases := map[string]string{
		"+00812345678": "+243812345678",
		"0812345678":   "+243812345678",
		"243812345678": "+243812345678",
		"812345678":    "+243812345678",
	}
	for in, want := range cases {
		if got := cmp.Destination(in); got != want {
			t.Errorf("Destination(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSenderAndDestinationAreIndependent(t *testing.T) {
	cfg := Config{
		Sender:      []Rule{{Type: StripDigits}},            // sender: drop digits
		Destination: []Rule{{Type: AddPrefix, Prefix: "+"}}, // dest: ensure +
	}
	cmp := mustCompile(t, cfg)
	if got := cmp.Sender("Brand123"); got != "Brand" {
		t.Errorf("Sender = %q", got)
	}
	if got := cmp.Destination("243812345"); got != "+243812345" {
		t.Errorf("Destination = %q", got)
	}
}

func TestNilCompiledIsPassThrough(t *testing.T) {
	var c *Compiled
	if got := c.Sender("abc"); got != "abc" {
		t.Errorf("nil Sender = %q", got)
	}
	if got := c.Destination("xyz"); got != "xyz" {
		t.Errorf("nil Destination = %q", got)
	}
	// An empty config compiles to nil (no rules).
	empty, err := Compile(Config{})
	if err != nil || empty != nil {
		t.Fatalf("empty Compile = %v, %v; want nil, nil", empty, err)
	}
}

func TestCompileValidation(t *testing.T) {
	if _, err := Compile(Config{Sender: []Rule{{Type: AddPrefix}}}); err == nil {
		t.Error("add_prefix without prefix should fail")
	}
	if _, err := Compile(Config{Sender: []Rule{{Type: RegexReplace, Pattern: "("}}}); err == nil {
		t.Error("invalid regex should fail")
	}
	if _, err := Compile(Config{Destination: []Rule{{Type: "bogus"}}}); err == nil {
		t.Error("unknown rule type should fail")
	}
}
