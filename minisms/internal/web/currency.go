package web

import (
	"fmt"
	"strconv"
	"strings"
)

// CurrencySymbol for common ISO 4217 codes; fallback: code.
func CurrencySymbol(iso3 string) string {
	switch strings.ToUpper(strings.TrimSpace(iso3)) {
	case "GBP":
		return "£"
	case "USD":
		return "$"
	case "EUR":
		return "€"
	default:
		return iso3 + " "
	}
}

// FormatBalance2dp shows NUMERIC as string with 2 decimal places and symbol.
func FormatBalance2dp(balance, currency string) string {
	if balance == "" {
		balance = "0"
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(balance), 64)
	if err != nil {
		return CurrencySymbol(currency) + balance
	}
	return fmt.Sprintf("%s%.2f", CurrencySymbol(currency), f)
}
