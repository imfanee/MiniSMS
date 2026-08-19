// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package routecache

import (
	"testing"

	"github.com/minisms/minisms/internal/db"
)

func TestLongestPrefix(t *testing.T) {
	entries := []RouteEntry{
		{Prefix: "*", PrimaryCarrierID: "catch"},
		{Prefix: "44", PrimaryCarrierID: "uk"},
		{Prefix: "447", PrimaryCarrierID: "uk7"},
	}
	got := longestPrefix(entries, "+447911123456")
	if got == nil || got.PrimaryCarrierID != "uk7" {
		t.Fatalf("got %+v", got)
	}
}

func TestTemplateAndAuthHeaderCacheSemantics(t *testing.T) {
	c := New()

	// Not warmed: both accessors report a miss so dispatch falls back to a DB read.
	if _, ok := c.Template("c1"); ok {
		t.Fatal("Template should miss on an unwarmed cache")
	}
	if _, ok := c.AuthHeaders("c1"); ok {
		t.Fatal("AuthHeaders should miss on an unwarmed cache")
	}

	// Warmed (as Reload would leave it): a carrier with config hits; a carrier the
	// bulk load did not return is correctly an empty-but-present set (no DB fallback),
	// while a template-less carrier reports a miss so the caller confirms via DB.
	c.mu.Lock()
	c.templates = map[string]*db.RequestTemplate{"c1": {ContentType: "application/json", BodyTemplate: "{}"}}
	c.headers = map[string][]db.AuthHeaderRow{"c1": {{HeaderName: "X-Api-Key", Value: "secret"}}}
	c.mu.Unlock()

	if tpl, ok := c.Template("c1"); !ok || tpl == nil || tpl.BodyTemplate != "{}" {
		t.Fatalf("Template(c1) should hit; got tpl=%v ok=%v", tpl, ok)
	}
	if _, ok := c.Template("no-template-carrier"); ok {
		t.Fatal("Template miss expected for a carrier with no template row")
	}
	if hs, ok := c.AuthHeaders("c1"); !ok || len(hs) != 1 || hs[0].Value != "secret" {
		t.Fatalf("AuthHeaders(c1) should hit with one header; got %v ok=%v", hs, ok)
	}
	if hs, ok := c.AuthHeaders("headerless-carrier"); !ok || len(hs) != 0 {
		t.Fatalf("a warmed cache should return an empty-but-present header set; got %v ok=%v", hs, ok)
	}
}
