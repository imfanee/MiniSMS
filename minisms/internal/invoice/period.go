// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package invoice

import "time"

// DefaultPeriod returns first and last day of the previous calendar week (Mon–Sun, UTC).
func DefaultPeriod(now time.Time) (from, to time.Time) {
	now = now.UTC()
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	thisMonday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(wd - 1))
	lastMonday := thisMonday.AddDate(0, 0, -7)
	lastSunday := lastMonday.AddDate(0, 0, 6)
	return lastMonday, lastSunday
}
