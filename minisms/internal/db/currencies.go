package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDuplicateCurrencyCode = errors.New("duplicate currency code")

type Currency struct {
	Code          string
	Name          string
	Symbol        string
	DecimalPlaces int16
	IsActive      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func ListActiveCurrencies(ctx context.Context, pool *pgxpool.Pool) ([]Currency, error) {
	return listCurrencies(ctx, pool, "WHERE is_active = TRUE")
}

func ListAllCurrencies(ctx context.Context, pool *pgxpool.Pool) ([]Currency, error) {
	return listCurrencies(ctx, pool, "")
}

func listCurrencies(ctx context.Context, pool *pgxpool.Pool, where string) ([]Currency, error) {
	rows, err := pool.Query(ctx, `
		SELECT code::text, name, symbol, decimal_places, is_active, created_at, updated_at
		FROM currencies `+where+`
		ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Currency
	for rows.Next() {
		var c Currency
		if err := rows.Scan(&c.Code, &c.Name, &c.Symbol, &c.DecimalPlaces, &c.IsActive, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func GetCurrency(ctx context.Context, pool *pgxpool.Pool, code string) (*Currency, error) {
	var c Currency
	err := pool.QueryRow(ctx, `
		SELECT code::text, name, symbol, decimal_places, is_active, created_at, updated_at
		FROM currencies
		WHERE code = $1`, code).Scan(&c.Code, &c.Name, &c.Symbol, &c.DecimalPlaces, &c.IsActive, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func CreateCurrency(ctx context.Context, pool *pgxpool.Pool, c Currency) error {
	var exists int
	err := pool.QueryRow(ctx, `SELECT 1 FROM currencies WHERE code = $1`, c.Code).Scan(&exists)
	if err == nil {
		return ErrDuplicateCurrencyCode
	}
	if err != pgx.ErrNoRows {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO currencies (code, name, symbol, decimal_places, is_active)
		VALUES ($1, $2, $3, $4, COALESCE($5, TRUE))`,
		c.Code, c.Name, c.Symbol, c.DecimalPlaces, c.IsActive)
	return err
}

func UpdateCurrency(ctx context.Context, pool *pgxpool.Pool, c Currency) error {
	_, err := pool.Exec(ctx, `
		UPDATE currencies
		SET name = $1, symbol = $2, decimal_places = $3, updated_at = now()
		WHERE code = $4`, c.Name, c.Symbol, c.DecimalPlaces, c.Code)
	return err
}

func ToggleCurrencyActive(ctx context.Context, pool *pgxpool.Pool, code string) error {
	_, err := pool.Exec(ctx, `
		UPDATE currencies
		SET is_active = NOT is_active, updated_at = now()
		WHERE code = $1`, code)
	return err
}
