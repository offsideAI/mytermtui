package dbx

import (
	"reflect"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"SELECT 1", []string{"SELECT 1"}},
		{"SELECT 1; SELECT 2;", []string{"SELECT 1", "SELECT 2"}},
		{"INSERT INTO t VALUES ('a;b'); SELECT 1", []string{"INSERT INTO t VALUES ('a;b')", "SELECT 1"}},
		{"SELECT 'it''s; fine'; SELECT 2", []string{"SELECT 'it''s; fine'", "SELECT 2"}},
		{`SELECT ";" FROM "we;ird"`, []string{`SELECT ";" FROM "we;ird"`}},
		{"SELECT 1 -- trailing; comment\n; SELECT 2", []string{"SELECT 1 -- trailing; comment", "SELECT 2"}},
		{"/* lead; ing */ SELECT 1", []string{"/* lead; ing */ SELECT 1"}},
		{"-- only a comment\n;  ;", nil},
		{"", nil},
	}
	for _, tc := range cases {
		if got := SplitStatements(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitStatements(%q)\n got %#v\nwant %#v", tc.in, got, tc.want)
		}
	}
}

func TestReturnsRows(t *testing.T) {
	cases := map[string]bool{
		"SELECT 1":                        true,
		"  with x as (select 1) select 1": true,
		"PRAGMA table_info(t)":            true,
		"EXPLAIN QUERY PLAN SELECT 1":     true,
		"VALUES (1)":                      true,
		"-- note\nSELECT 1":               true,
		"/* c */ SELECT 1":                true,
		"INSERT INTO t VALUES (1)":        false,
		"UPDATE t SET x = 1":              false,
		"CREATE TABLE t (x)":              false,
	}
	for in, want := range cases {
		if got := ReturnsRows(in); got != want {
			t.Errorf("ReturnsRows(%q) = %v, want %v", in, got, want)
		}
	}
}
