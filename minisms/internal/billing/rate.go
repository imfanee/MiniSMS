// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package billing

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoRate = errors.New("SMS_ERR_NO_RATE")

type RateEntry struct {
	RateEntryID string
	Prefix      string
	RatePerSMS  string
}

var gsm7Runes = func() map[rune]struct{} {
	const chars = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞ\x1bÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà€"
	out := make(map[rune]struct{}, len(chars))
	for _, r := range chars {
		out[r] = struct{}{}
	}
	return out
}()

func LookupRate(ctx context.Context, pool *pgxpool.Pool, rateGroupID, destination string) (*RateEntry, error) {
	rows, err := pool.Query(ctx, `
		SELECT rate_entry_id::text, prefix, rate_per_sms::text
		FROM v_active_rate_entries
		WHERE rate_group_id = $1::uuid`, rateGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []RateEntry
	for rows.Next() {
		var e RateEntry
		if err := rows.Scan(&e.RateEntryID, &e.Prefix, &e.RatePerSMS); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	best := longestPrefixRate(entries, destination)
	if best == nil {
		return nil, ErrNoRate
	}
	return best, nil
}

func SegmentInfo(message string) (encoding string, segments int) {
	segmentSize := 160
	encoding = "GSM7"
	for _, r := range message {
		if _, ok := gsm7Runes[r]; !ok {
			segmentSize = 70
			encoding = "UCS2"
			break
		}
	}
	msgLen := len([]rune(message))
	segments = int(math.Ceil(float64(msgLen) / float64(segmentSize)))
	if segments < 1 {
		segments = 1
	}
	return encoding, segments
}

func longestPrefixRate(entries []RateEntry, destination string) *RateEntry {
	dst := normalizeDestination(destination)
	var catchAll *RateEntry
	var best *RateEntry
	bestLen := -1
	for i := range entries {
		e := &entries[i]
		if e.Prefix == "*" {
			if catchAll == nil {
				catchAll = e
			}
			continue
		}
		if strings.HasPrefix(dst, e.Prefix) && len(e.Prefix) > bestLen {
			best = e
			bestLen = len(e.Prefix)
		}
	}
	if best != nil {
		return best
	}
	return catchAll
}

func normalizeDestination(destination string) string {
	d := strings.TrimPrefix(strings.TrimSpace(destination), "+")
	var b strings.Builder
	for _, r := range d {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
