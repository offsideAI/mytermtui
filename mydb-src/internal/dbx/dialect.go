package dbx

import "strings"

// Both v1 engines speak ANSI quoting: double-quoted identifiers with ""
// escaping, single-quoted literals with '' escaping (Postgres has
// standard_conforming_strings on by default). Engines that deviate get a
// per-driver dialect when they arrive.

// QuoteIdent double-quotes an identifier, escaping embedded quotes.
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// QuoteLiteral single-quotes a string literal, escaping embedded quotes.
func QuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ValidIdent rejects values that cannot be a sane identifier even
// quoted (empty, embedded NUL) — the last line of defense behind the
// form validation.
func ValidIdent(s string) bool {
	return s != "" && !strings.ContainsRune(s, 0)
}
