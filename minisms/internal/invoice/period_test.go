// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package invoice

import (
	"testing"
	"time"
)

func TestDefaultPeriodLastWeek(t *testing.T) {
	// Thursday 2026-06-04 → last week Mon 2026-05-25, Sun 2026-05-31
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	from, to := DefaultPeriod(now)
	if from.Format("2006-01-02") != "2026-05-25" {
		t.Fatalf("from: %s", from.Format("2006-01-02"))
	}
	if to.Format("2006-01-02") != "2026-05-31" {
		t.Fatalf("to: %s", to.Format("2006-01-02"))
	}
}
