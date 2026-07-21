// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"testing"
	"time"

	"github.com/minisms/minisms/internal/config"
)

func TestRetryBackoff_GrowsAndCaps(t *testing.T) {
	r := &QueueRunner{backoffBase: 5 * time.Second}
	if got := r.retryBackoff(0); got != 5*time.Second {
		t.Fatalf("attempt 0 backoff=%v want floor 5s", got)
	}
	if got := r.retryBackoff(3); got != 15*time.Second {
		t.Fatalf("attempt 3 backoff=%v want 15s", got)
	}
	if got := r.retryBackoff(100); got != 60*time.Second {
		t.Fatalf("attempt 100 backoff=%v want capped 60s", got)
	}
}

func TestNewQueueRunner_FromConfig(t *testing.T) {
	svc := &Service{Config: &config.Config{
		SendQueueWorkers: 8, SendQueueBatch: 80, SendRetryBackoffSecs: 3, SendStuckSecs: 120,
	}}
	r := NewQueueRunner(svc)
	if r.workers != 8 || r.batch != 80 || r.backoffBase != 3*time.Second {
		t.Fatalf("runner not built from config: %+v", r)
	}
}
