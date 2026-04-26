package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SMSByClientRow struct {
	ClientID       string
	ClientName     string
	TotalMessages  int64
	TotalSegments  int64
	TotalBilled    float64
	SuccessCount   int64
	FailedCount    int64
	RejectedCount  int64
	SuccessRatePct float64
}

type SMSByCarrierRow struct {
	CarrierID      string
	CarrierName    string
	TotalMessages  int64
	AsPrimary      int64
	AsFailover1    int64
	AsFailover2    int64
	SuccessCount   int64
	FailedCount    int64
	SuccessRatePct float64
}

type CarrierPrefixRow struct {
	CarrierID      string
	CarrierName    string
	PrefixMatched  string
	Total          int64
	SuccessCount   int64
	FailedCount    int64
	SuccessRatePct float64
}

type BillVsCostRow struct {
	ClientID      string
	ClientName    string
	CarrierID     string
	CarrierName   string
	TotalMessages int64
	ClientBilled  float64
	CarrierCost   float64
	Margin        float64
	MarginPct     float64
}

type CarrierCostRow struct {
	CarrierID     string
	CarrierName   string
	TotalMessages int64
	ClientRevenue float64
	CarrierCost   float64
	NetMargin     float64
	MarginPct     float64
}

func GetSMSByClientReport(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]SMSByClientRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT cl.client_id::text, cl.name,
			COUNT(*)::bigint, COALESCE(SUM(sl.segments),0)::bigint,
			COALESCE(SUM(sl.total_charged),0)::float8,
			COUNT(*) FILTER (WHERE sl.status IN ('sent','delivered'))::bigint,
			COUNT(*) FILTER (WHERE sl.status = 'failed')::bigint,
			COUNT(*) FILTER (WHERE sl.status = 'rejected')::bigint,
			COALESCE(ROUND(COUNT(*) FILTER (WHERE sl.status IN ('sent','delivered'))::NUMERIC/NULLIF(COUNT(*),0)*100,2),0)::float8
		FROM sms_logs sl JOIN clients cl ON cl.client_id=sl.client_id
		WHERE sl.received_at >= $1::timestamptz AND sl.received_at < ($2::timestamptz + INTERVAL '1 day')
		GROUP BY cl.client_id, cl.name
		ORDER BY COUNT(*) DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SMSByClientRow
	for rows.Next() {
		var x SMSByClientRow
		if err := rows.Scan(&x.ClientID, &x.ClientName, &x.TotalMessages, &x.TotalSegments, &x.TotalBilled, &x.SuccessCount, &x.FailedCount, &x.RejectedCount, &x.SuccessRatePct); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func GetSMSByCarrierReport(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]SMSByCarrierRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT ca.carrier_id::text, ca.name,
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE sl.failover_sequence=0)::bigint,
			COUNT(*) FILTER (WHERE sl.failover_sequence=1)::bigint,
			COUNT(*) FILTER (WHERE sl.failover_sequence=2)::bigint,
			COUNT(*) FILTER (WHERE sl.status IN ('sent','delivered'))::bigint,
			COUNT(*) FILTER (WHERE sl.status='failed')::bigint,
			COALESCE(ROUND(COUNT(*) FILTER (WHERE sl.status IN ('sent','delivered'))::NUMERIC/NULLIF(COUNT(*),0)*100,2),0)::float8
		FROM sms_logs sl JOIN carriers ca ON ca.carrier_id=sl.carrier_id
		WHERE sl.received_at >= $1::timestamptz AND sl.received_at < ($2::timestamptz + INTERVAL '1 day')
		  AND sl.carrier_id IS NOT NULL
		GROUP BY ca.carrier_id, ca.name
		ORDER BY COUNT(*) DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SMSByCarrierRow
	for rows.Next() {
		var x SMSByCarrierRow
		if err := rows.Scan(&x.CarrierID, &x.CarrierName, &x.TotalMessages, &x.AsPrimary, &x.AsFailover1, &x.AsFailover2, &x.SuccessCount, &x.FailedCount, &x.SuccessRatePct); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func GetCarrierPrefixSuccessReport(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]CarrierPrefixRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT sl.carrier_id::text, ca.name, sl.prefix_matched,
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE sl.status IN ('sent','delivered'))::bigint,
			COUNT(*) FILTER (WHERE sl.status = 'failed')::bigint,
			COALESCE(ROUND(COUNT(*) FILTER (WHERE sl.status IN ('sent','delivered'))::NUMERIC/NULLIF(COUNT(*),0)*100,2),0)::float8
		FROM sms_logs sl JOIN carriers ca ON ca.carrier_id=sl.carrier_id
		WHERE sl.received_at >= $1::timestamptz AND sl.received_at < ($2::timestamptz + INTERVAL '1 day')
		  AND sl.carrier_id IS NOT NULL AND sl.prefix_matched IS NOT NULL
		GROUP BY sl.carrier_id, ca.name, sl.prefix_matched
		ORDER BY ca.name, length(sl.prefix_matched) DESC, sl.prefix_matched`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CarrierPrefixRow
	for rows.Next() {
		var x CarrierPrefixRow
		if err := rows.Scan(&x.CarrierID, &x.CarrierName, &x.PrefixMatched, &x.Total, &x.SuccessCount, &x.FailedCount, &x.SuccessRatePct); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func GetBillVsCostReport(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]BillVsCostRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT cl.client_id::text, cl.name, ca.carrier_id::text, ca.name,
			COUNT(*)::bigint, COALESCE(SUM(sl.total_charged),0)::float8,
			COALESCE(SUM(cbe.amount),0)::float8,
			(COALESCE(SUM(sl.total_charged),0)-COALESCE(SUM(cbe.amount),0))::float8,
			COALESCE(ROUND((COALESCE(SUM(sl.total_charged),0)-COALESCE(SUM(cbe.amount),0))/NULLIF(COALESCE(SUM(sl.total_charged),0),0)*100,2),0)::float8
		FROM sms_logs sl
		JOIN clients cl ON cl.client_id=sl.client_id
		JOIN carriers ca ON ca.carrier_id=sl.carrier_id
		LEFT JOIN carrier_balance_entries cbe ON cbe.message_id=sl.message_id AND cbe.entry_type='charge'
		WHERE sl.received_at >= $1::timestamptz AND sl.received_at < ($2::timestamptz + INTERVAL '1 day')
		  AND sl.status IN ('accepted','sent','delivered') AND sl.carrier_id IS NOT NULL
		GROUP BY cl.client_id, cl.name, ca.carrier_id, ca.name`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BillVsCostRow
	for rows.Next() {
		var x BillVsCostRow
		if err := rows.Scan(&x.ClientID, &x.ClientName, &x.CarrierID, &x.CarrierName, &x.TotalMessages, &x.ClientBilled, &x.CarrierCost, &x.Margin, &x.MarginPct); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func GetCarrierCostComparisonReport(ctx context.Context, pool *pgxpool.Pool, from, to time.Time) ([]CarrierCostRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT ca.carrier_id::text, ca.name,
			COUNT(*)::bigint,
			COALESCE(SUM(sl.total_charged),0)::float8,
			COALESCE(SUM(cbe.amount),0)::float8,
			(COALESCE(SUM(sl.total_charged),0)-COALESCE(SUM(cbe.amount),0))::float8,
			COALESCE(ROUND((COALESCE(SUM(sl.total_charged),0)-COALESCE(SUM(cbe.amount),0))/NULLIF(COALESCE(SUM(sl.total_charged),0),0)*100,2),0)::float8
		FROM sms_logs sl JOIN carriers ca ON ca.carrier_id=sl.carrier_id
		LEFT JOIN carrier_balance_entries cbe ON cbe.message_id=sl.message_id AND cbe.entry_type='charge'
		WHERE sl.received_at >= $1::timestamptz AND sl.received_at < ($2::timestamptz + INTERVAL '1 day')
		  AND sl.status IN ('accepted','sent','delivered') AND sl.carrier_id IS NOT NULL
		GROUP BY ca.carrier_id, ca.name
		ORDER BY COALESCE(SUM(sl.total_charged),0) DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CarrierCostRow
	for rows.Next() {
		var x CarrierCostRow
		if err := rows.Scan(&x.CarrierID, &x.CarrierName, &x.TotalMessages, &x.ClientRevenue, &x.CarrierCost, &x.NetMargin, &x.MarginPct); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
