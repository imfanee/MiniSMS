// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package minisms

import "testing"

// Module root package: placeholder to verify embed fs exists in tests.
func TestEmbedPath(t *testing.T) {
	_, err := TemplateFS.ReadFile("templates/layout/base.html")
	if err != nil {
		t.Fatal(err)
	}
	_, err = StaticFS.ReadFile("static/css/app.css")
	if err != nil {
		t.Fatal(err)
	}
}
