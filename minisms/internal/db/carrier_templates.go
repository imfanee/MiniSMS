package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RequestTemplate is the carrier's outbound request template.
type RequestTemplate struct {
	TemplateID    string
	ContentType   string
	BodyTemplate  string
	QueryTemplate string
	UpdatedAt     *string
}

// GetRequestTemplate fetches the template, or (nil, nil) if not yet created.
func GetRequestTemplate(ctx context.Context, pool *pgxpool.Pool, carrierID string) (*RequestTemplate, error) {
	var t RequestTemplate
	var b, q, up *string
	err := pool.QueryRow(ctx, `
		SELECT template_id::text, content_type, body_template, query_template, updated_at::text
		FROM carrier_request_templates
		WHERE carrier_id = $1::uuid`, carrierID,
	).Scan(&t.TemplateID, &t.ContentType, &b, &q, &up)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if b != nil {
		t.BodyTemplate = *b
	}
	if q != nil {
		t.QueryTemplate = *q
	}
	t.UpdatedAt = up
	return &t, nil
}

// UpsertRequestTemplate inserts or updates the single row per carrier.
func UpsertRequestTemplate(ctx context.Context, pool *pgxpool.Pool, carrierID, contentType, body, query string) error {
	var b, q any
	if body == "" {
		b = nil
	} else {
		b = body
	}
	if query == "" {
		q = nil
	} else {
		q = query
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO carrier_request_templates (carrier_id, content_type, body_template, query_template, updated_at)
		VALUES ($1::uuid, $2, $3, $4, now())
		ON CONFLICT (carrier_id) DO UPDATE SET
			content_type = EXCLUDED.content_type,
			body_template = EXCLUDED.body_template,
			query_template = EXCLUDED.query_template,
			updated_at = now()`,
		carrierID, contentType, b, q,
	)
	return err
}
