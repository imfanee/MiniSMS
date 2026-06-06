// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package models

import "time"

// AdminSession is a row from admin_sessions (session_token stores SHA-256 hash of the cookie token, hex).
type AdminSession struct {
	SessionID     string
	SessionToken  string
	AdminUserID   *string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	LastActiveAt  time.Time
	IPAddress     *string
	UserAgent     *string
	IsRevoked     bool
}
