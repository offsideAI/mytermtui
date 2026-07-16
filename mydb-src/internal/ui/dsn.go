package ui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// connParts is what parseConnString extracts from a pasted connection
// string; zero-valued fields mean "not specified".
type connParts struct {
	Engine   string // "sqlite" | "postgres"
	Path     string
	Host     string
	Port     int
	DBName   string
	Username string
	Password string
}

// parseConnString understands the common shapes people paste:
//
//	postgres://user:pass@host:5432/db?sslmode=disable
//	postgresql://…
//	host=localhost port=5432 user=me password=x dbname=app
//	sqlite:///abs/path.db · file:/abs/path.db · plain paths (~ ok)
//
// Unknown query params / DSN keys are ignored; anything else errors.
func parseConnString(s string) (connParts, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return connParts{}, fmt.Errorf("empty connection string")
	}

	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return parsePGURL(s)
	case strings.HasPrefix(lower, "sqlite://"), strings.HasPrefix(lower, "file:"):
		return parseSQLiteURL(s)
	case strings.Contains(s, "://"):
		scheme := s[:strings.Index(s, "://")]
		return connParts{}, fmt.Errorf("unsupported scheme %q (postgres://, sqlite://, file:)", scheme)
	case strings.Contains(s, "="):
		return parseKVDSN(s)
	default:
		// A bare string is a SQLite file path.
		return connParts{Engine: "sqlite", Path: s}, nil
	}
}

func parsePGURL(s string) (connParts, error) {
	u, err := url.Parse(s)
	if err != nil {
		return connParts{}, err
	}
	p := connParts{Engine: "postgres", Host: u.Hostname()}
	if u.Port() != "" {
		p.Port, err = strconv.Atoi(u.Port())
		if err != nil || p.Port < 0 || p.Port > 65535 {
			return connParts{}, fmt.Errorf("bad port %q", u.Port())
		}
	}
	if u.User != nil {
		p.Username = u.User.Username()
		p.Password, _ = u.User.Password()
	}
	p.DBName = strings.TrimPrefix(u.Path, "/")
	if strings.Contains(p.DBName, "/") {
		return connParts{}, fmt.Errorf("bad database path %q", u.Path)
	}
	return p, nil
}

func parseSQLiteURL(s string) (connParts, error) {
	u, err := url.Parse(s)
	if err != nil {
		return connParts{}, err
	}
	path := u.Path
	if path == "" {
		path = u.Opaque // file:relative/path parses as opaque
	}
	if u.Host != "" && u.Host != "localhost" {
		return connParts{}, fmt.Errorf("sqlite URLs cannot have a host (%q)", u.Host)
	}
	if path == "" {
		return connParts{}, fmt.Errorf("no path in %q", s)
	}
	return connParts{Engine: "sqlite", Path: path}, nil
}

func parseKVDSN(s string) (connParts, error) {
	p := connParts{Engine: "postgres"}
	for _, field := range strings.Fields(s) {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			return connParts{}, fmt.Errorf("bad DSN field %q (want key=value)", field)
		}
		v = strings.Trim(v, "'")
		switch k {
		case "host":
			p.Host = v
		case "port":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 65535 {
				return connParts{}, fmt.Errorf("bad port %q", v)
			}
			p.Port = n
		case "user":
			p.Username = v
		case "password":
			p.Password = v
		case "dbname", "database":
			p.DBName = v
		default:
			// sslmode, connect_timeout, application_name… — ignored in v1.
		}
	}
	if p.Host == "" && p.DBName == "" {
		return connParts{}, fmt.Errorf("DSN has neither host nor dbname")
	}
	return p, nil
}
