// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
// Package watchdog periodically logs live load/capacity stats (in-flight HTTP requests, DB pool usage
// and acquire-waits, process CPU and RSS, goroutines) so an operator can confirm the connection-pool
// tuning holds up under heavy load and spot a bottleneck early. It has no external dependencies: CPU and
// memory are read from /proc, pool stats from pgxpool.
package watchdog

import (
	"bufio"
	"context"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// inFlight counts HTTP requests currently being served (see Middleware).
var inFlight int64

// Middleware increments the in-flight counter for the duration of each request. Mount it as the
// outermost router middleware so the count reflects true concurrency.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&inFlight, 1)
		defer atomic.AddInt64(&inFlight, -1)
		next.ServeHTTP(w, r)
	})
}

// InFlight returns the current number of in-flight HTTP requests.
func InFlight() int64 { return atomic.LoadInt64(&inFlight) }

// Start launches the stats logger until ctx is cancelled. interval<=0 disables it.
func Start(ctx context.Context, pool *pgxpool.Pool, interval time.Duration, log *slog.Logger) {
	if interval <= 0 || pool == nil {
		return
	}
	go func() {
		prevCPU := readProcCPUSeconds()
		prevAt := time.Now()
		ncpu := float64(runtime.NumCPU())
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				curCPU := readProcCPUSeconds()
				dt := now.Sub(prevAt).Seconds()
				cores := 0.0
				if dt > 0 && curCPU >= prevCPU {
					cores = (curCPU - prevCPU) / dt // CPU-seconds per wall-second = cores in use
				}
				prevCPU, prevAt = curCPU, now
				st := pool.Stat()
				log.Info("watchdog",
					"in_flight_requests", InFlight(),
					"db_conns_in_use", st.AcquiredConns(),
					"db_conns_idle", st.IdleConns(),
					"db_conns_total", st.TotalConns(),
					"db_conns_max", st.MaxConns(),
					"db_acquire_waits", st.EmptyAcquireCount(), // cumulative: times a caller waited for a free conn (pool pressure)
					"db_acquire_canceled", st.CanceledAcquireCount(),
					"cpu_cores_used", round(cores, 2),
					"cpu_pct", round(cores/ncpu*100, 1),
					"cpu_cores_total", int(ncpu),
					"rss_mb", readRSSMB(),
					"goroutines", runtime.NumGoroutine(),
				)
			}
		}
	}()
}

// clkTck is the kernel USER_HZ; 100 on essentially all Linux builds. Reading it exactly needs cgo
// (sysconf), which we avoid; a wrong value only scales CPU numbers, never affects correctness.
const clkTck = 100.0

// readProcCPUSeconds returns this process's total (user+system) CPU time in seconds from /proc/self/stat.
func readProcCPUSeconds() float64 {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	s := string(b)
	// The comm field (2nd) is wrapped in parens and may contain spaces; fields after it start right
	// after the last ')'. utime is field 14 and stime field 15 overall -> indices 11 and 12 after comm.
	rp := strings.LastIndexByte(s, ')')
	if rp < 0 || rp+2 >= len(s) {
		return 0
	}
	fields := strings.Fields(s[rp+2:])
	if len(fields) < 13 {
		return 0
	}
	utime, _ := strconv.ParseFloat(fields[11], 64)
	stime, _ := strconv.ParseFloat(fields[12], 64)
	return (utime + stime) / clkTck
}

// readRSSMB returns the process resident set size in MB from /proc/self/statm.
func readRSSMB() int64 {
	f, err := os.Open("/proc/self/statm")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 2 {
		return 0
	}
	residentPages, _ := strconv.ParseInt(fields[1], 10, 64)
	return residentPages * int64(os.Getpagesize()) / (1024 * 1024)
}

func round(v float64, places int) float64 {
	p := 1.0
	for i := 0; i < places; i++ {
		p *= 10
	}
	return float64(int64(v*p+0.5)) / p
}
