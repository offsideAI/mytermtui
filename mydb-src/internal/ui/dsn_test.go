package ui

import "testing"

func TestParseConnString(t *testing.T) {
	cases := []struct {
		in   string
		want connParts
		err  bool
	}{
		{
			in: "postgres://alice:pw@db.example.com:5433/appdb?sslmode=disable",
			want: connParts{Engine: "postgres", Host: "db.example.com", Port: 5433,
				DBName: "appdb", Username: "alice", Password: "pw"},
		},
		{
			in:   "postgresql://bob@localhost/app",
			want: connParts{Engine: "postgres", Host: "localhost", DBName: "app", Username: "bob"},
		},
		{
			in: "host=localhost port=5432 user=me password=x dbname=app sslmode=require",
			want: connParts{Engine: "postgres", Host: "localhost", Port: 5432,
				DBName: "app", Username: "me", Password: "x"},
		},
		{
			in:   "dbname=app",
			want: connParts{Engine: "postgres", DBName: "app"},
		},
		{
			in:   "~/data/app.db",
			want: connParts{Engine: "sqlite", Path: "~/data/app.db"},
		},
		{
			in:   "sqlite:///abs/x.db",
			want: connParts{Engine: "sqlite", Path: "/abs/x.db"},
		},
		{
			in:   "file:/abs/y.db",
			want: connParts{Engine: "sqlite", Path: "/abs/y.db"},
		},
		{in: "mysql://h/db", err: true},
		{in: "postgres://h:notaport/db", err: true},
		{in: "", err: true},
		{in: "host=", err: true},
	}
	for _, tc := range cases {
		got, err := parseConnString(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("%q: want error, got %+v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q:\n got %+v\nwant %+v", tc.in, got, tc.want)
		}
	}
}

func TestConnFormURLFillsFields(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "B")
	f := m.modal.(*connForm)
	for _, r := range "postgres://alice:pw@dbhost:5433/appdb" {
		press(t, m, string(r))
	}
	press(t, m, "enter") // parse + advance to name

	if engines[f.engine] != "postgres" {
		t.Fatalf("engine = %s", engines[f.engine])
	}
	for id, want := range map[string]string{
		"host": "dbhost", "port": "5433", "dbname": "appdb",
		"username": "alice", "password": "pw",
	} {
		if got := f.fields[id].Value(); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
	if f.fieldOrder()[f.focus] != "name" {
		t.Fatalf("focus should advance to name, is on %s", f.fieldOrder()[f.focus])
	}

	press(t, m, "v", "i", "a", "u", "r", "l") // name
	press(t, m, "ctrl+s")                     // save (re-applies the URL, validates)
	if m.modal != nil {
		t.Fatalf("form should close, err=%q", f.errMsg)
	}
	conns, err := m.reg.Connections()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range conns {
		if c.Name == "viaurl" {
			if c.Host != "dbhost" || c.Port != 5433 || c.DBName != "appdb" ||
				c.Username != "alice" || c.Secret != "pw" || c.Engine != "postgres" {
				t.Fatalf("saved connection wrong: %+v", c)
			}
			return
		}
	}
	t.Fatal("viaurl connection not saved")
}

func TestConnFormBadURLStays(t *testing.T) {
	m, _ := fixture(t)
	press(t, m, "B")
	f := m.modal.(*connForm)
	for _, r := range "mysql://h/db" {
		press(t, m, string(r))
	}
	press(t, m, "enter")
	if f.errMsg == "" || f.fieldOrder()[f.focus] != "url" {
		t.Fatalf("bad URL should error and keep focus (err=%q focus=%s)",
			f.errMsg, f.fieldOrder()[f.focus])
	}
	if f.fields["host"].Value() != "" {
		t.Fatal("bad URL must not touch the fields")
	}
	press(t, m, "esc")
}
