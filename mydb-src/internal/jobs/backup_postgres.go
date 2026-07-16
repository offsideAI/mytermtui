package jobs

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// PGConn is the connection detail a Postgres backup/restore needs to
// spawn the client tools.
type PGConn struct {
	Host     string
	Port     int
	DBName   string
	Username string
	Password string
}

func (c PGConn) env() []string {
	env := os.Environ()
	if c.Password != "" {
		env = append(env, "PGPASSWORD="+c.Password)
	}
	return env
}

func (c PGConn) args() []string {
	var a []string
	if c.Host != "" {
		a = append(a, "-h", c.Host)
	}
	if c.Port > 0 {
		a = append(a, "-p", strconv.Itoa(c.Port))
	}
	if c.Username != "" {
		a = append(a, "-U", c.Username)
	}
	return a
}

// Tool probes whether a client binary is on PATH and returns its version
// string; ok is false when it's missing.
func Tool(name string) (version string, ok bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return "", true
	}
	return strings.TrimSpace(string(out)), true
}

// PGDumpBackup returns a Run using `pg_dump -Fc -v` (custom format,
// restorable with pg_restore). Progress is bytes written to dest plus
// the current phase scraped from -v stderr; pg_dump gives no percentage,
// so BytesTot stays -1 (the UI shows a spinner + bytes, not a bar).
func PGDumpBackup(conn PGConn, dest string) Run {
	return func(ctx context.Context, report Report) error {
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("%s already exists", dest)
		}
		args := append(conn.args(), "-Fc", "-v", "-f", dest, conn.DBName)
		cmd := exec.CommandContext(ctx, "pg_dump", args...)
		cmd.Env = conn.env()
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}

		stop := make(chan struct{})
		go pollSize(dest, report, stop)
		scanPhases(stderr, report, "dumping")
		close(stop)

		if err := cmd.Wait(); err != nil {
			os.Remove(dest)
			return dumpErr(err)
		}
		if fi, err := os.Stat(dest); err == nil {
			report(fi.Size(), fi.Size(), "done")
		}
		return nil
	}
}

// PGRestore returns a Run using `pg_restore -v` for a custom-format
// archive, or psql-style execution for a plain .sql file.
func PGRestore(conn PGConn, src string) Run {
	return func(ctx context.Context, report Report) error {
		if _, err := os.Stat(src); err != nil {
			return err
		}
		report(0, -1, "restoring")
		args := append(conn.args(), "-v", "-d", conn.DBName, src)
		cmd := exec.CommandContext(ctx, "pg_restore", args...)
		cmd.Env = conn.env()
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		scanPhases(stderr, report, "restoring")
		if err := cmd.Wait(); err != nil {
			return dumpErr(err)
		}
		report(0, -1, "done")
		return nil
	}
}

// pollSize reports the artifact's growing size until stop closes.
func pollSize(dest string, report Report, stop <-chan struct{}) {
	t := time.NewTicker(300 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if fi, err := os.Stat(dest); err == nil {
				report(fi.Size(), -1, "")
			}
		}
	}
}

// scanPhases turns pg_dump/-restore -v lines into phase updates. Lines
// look like "pg_dump: dumping contents of table \"public.users\"".
func scanPhases(r interface{ Read([]byte) (int, error) }, report Report, verb string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "contents of table"); i >= 0 {
			report(-1, -1, verb+" table"+afterColon(line[i+len("contents of table"):]))
		} else if strings.Contains(line, "creating") {
			report(-1, -1, strings.TrimSpace(strings.TrimPrefix(line, "pg_restore:")))
		}
	}
}

func afterColon(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"`)
	if len(s) > 40 {
		s = s[:40]
	}
	return " " + s
}

// dumpErr surfaces the tool's own message when it exited non-zero.
func dumpErr(err error) error {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		return fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}
