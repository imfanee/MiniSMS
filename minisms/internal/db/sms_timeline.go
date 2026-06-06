// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/minisms/minisms/internal/smslog"
)

type timelineExec interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func SetSMSEventTimeline(ctx context.Context, q timelineExec, messageID string, events []smslog.TimelineEvent) error {
	if events == nil {
		events = []smslog.TimelineEvent{}
	}
	raw, err := json.Marshal(events)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		UPDATE sms_logs SET event_timeline = $2::jsonb WHERE message_id = $1::uuid`,
		messageID, string(raw),
	)
	return err
}

func AppendSMSEventTimeline(ctx context.Context, q timelineExec, messageID string, events ...smslog.TimelineEvent) error {
	if len(events) == 0 {
		return nil
	}
	raw, err := json.Marshal(events)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		UPDATE sms_logs SET event_timeline = event_timeline || $2::jsonb WHERE message_id = $1::uuid`,
		messageID, string(raw),
	)
	return err
}
