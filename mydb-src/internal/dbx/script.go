package dbx

import "strings"

// SplitStatements splits a SQL script on semicolons, respecting single
// and double quotes, line comments (--) and block comments (/* */).
// Empty / comment-only statements are dropped. Engines whose wire
// protocol runs whole scripts (Postgres simple protocol) don't need
// this; engines that execute one statement at a time (SQLite) do.
func SplitStatements(script string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		if s != "" && !commentOnly(s) {
			out = append(out, s)
		}
	}

	runes := []rune(script)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == '\'' || r == '"':
			quote := r
			cur.WriteRune(r)
			i++
			for i < len(runes) {
				cur.WriteRune(runes[i])
				if runes[i] == quote {
					// '' inside a '-string is an escaped quote, not the end.
					if quote == '\'' && i+1 < len(runes) && runes[i+1] == '\'' {
						i++
						cur.WriteRune(runes[i])
					} else {
						break
					}
				}
				i++
			}
			i++
		case r == '-' && i+1 < len(runes) && runes[i+1] == '-':
			for i < len(runes) && runes[i] != '\n' {
				cur.WriteRune(runes[i])
				i++
			}
		case r == '/' && i+1 < len(runes) && runes[i+1] == '*':
			for i < len(runes) {
				cur.WriteRune(runes[i])
				if runes[i] == '/' && i > 0 && runes[i-1] == '*' && cur.Len() > 2 {
					i++
					break
				}
				i++
			}
		case r == ';':
			flush()
			i++
		default:
			cur.WriteRune(r)
			i++
		}
	}
	flush()
	return out
}

// commentOnly reports whether a trimmed statement consists solely of
// comments and whitespace.
func commentOnly(s string) bool {
	for {
		s = strings.TrimSpace(s)
		switch {
		case s == "":
			return true
		case strings.HasPrefix(s, "--"):
			nl := strings.IndexByte(s, '\n')
			if nl < 0 {
				return true
			}
			s = s[nl+1:]
		case strings.HasPrefix(s, "/*"):
			end := strings.Index(s, "*/")
			if end < 0 {
				return true
			}
			s = s[end+2:]
		default:
			return false
		}
	}
}

// ReturnsRows guesses whether a statement produces a result set, for
// engines that must choose between Query and Exec per statement.
func ReturnsRows(stmt string) bool {
	for {
		stmt = strings.TrimSpace(stmt)
		if strings.HasPrefix(stmt, "--") {
			nl := strings.IndexByte(stmt, '\n')
			if nl < 0 {
				return false
			}
			stmt = stmt[nl+1:]
			continue
		}
		if strings.HasPrefix(stmt, "/*") {
			end := strings.Index(stmt, "*/")
			if end < 0 {
				return false
			}
			stmt = stmt[end+2:]
			continue
		}
		break
	}
	word := strings.ToUpper(firstWord(stmt))
	switch word {
	case "SELECT", "VALUES", "PRAGMA", "EXPLAIN", "WITH":
		return true
	}
	return false
}

func firstWord(s string) string {
	end := 0
	for end < len(s) && (isAlpha(s[end])) {
		end++
	}
	return s[:end]
}

func isAlpha(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
