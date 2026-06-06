// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package pathutil

import "testing"

func TestResolveUnderRejectsTraversal(t *testing.T) {
	_, err := ResolveUnder("/opt/minisms", "../etc/passwd")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveUnderAllowsNested(t *testing.T) {
	p, err := ResolveUnder("/opt/minisms", "invoices/client/id/file.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if p == "" {
		t.Fatal("empty path")
	}
}

func TestValidateRelativeDataPath(t *testing.T) {
	if err := ValidateRelativeDataPath("assets/invoice_header.png", "assets"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRelativeDataPath("../etc/passwd", "assets"); err == nil {
		t.Fatal("expected reject")
	}
}
