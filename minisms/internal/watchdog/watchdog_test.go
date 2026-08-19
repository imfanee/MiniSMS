// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package watchdog

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProcReadersReturnSaneValues(t *testing.T) {
	// On Linux these read /proc; assert they don't panic and return plausible (>=0) values.
	if cpu := readProcCPUSeconds(); cpu < 0 {
		t.Errorf("cpu seconds = %v", cpu)
	}
	if rss := readRSSMB(); rss < 0 {
		t.Errorf("rss mb = %v", rss)
	}
}

func TestRound(t *testing.T) {
	if got := round(1.2345, 2); got != 1.23 {
		t.Errorf("round(1.2345,2)=%v", got)
	}
	if got := round(1.006, 2); got != 1.01 {
		t.Errorf("round(1.006,2)=%v", got)
	}
}

func TestMiddlewareCountsInFlight(t *testing.T) {
	if InFlight() != 0 {
		t.Fatalf("expected 0 in-flight at start, got %d", InFlight())
	}
	var during int64
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		during = InFlight()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if during != 1 {
		t.Errorf("in-flight during request = %d, want 1", during)
	}
	if InFlight() != 0 {
		t.Errorf("in-flight after request = %d, want 0", InFlight())
	}
}
