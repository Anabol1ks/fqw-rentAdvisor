package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

// Defaults
const (
	defaultSchema = "public"
	defaultTable  = "listing_raw"
)

func main() {
	// Load .env if present
	_ = godotenv.Load()

	mode := flag.String("mode", "copy", "Mode: export | import | copy")
	schema := flag.String("schema", defaultSchema, "Schema name")
	table := flag.String("table", defaultTable, "Table name")
	file := flag.String("file", "", "Path to file for export/import (required for export/import)")

	// DSN overrides via flags
	srcDSNFlag := flag.String("src", "", "Source Postgres DSN (overrides env)")
	dstDSNFlag := flag.String("dst", "", "Destination Postgres DSN (overrides env)")
	flag.Parse()

	ctx := context.Background()

	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "export":
		if *file == "" {
			fatalf("--file is required for export mode")
		}
		srcDSN := firstNonEmpty(*srcDSNFlag,
			dsnFromEnvPrefix("SRC_"),
			dsnFromEnvPrefix(""),
		)
		if srcDSN == "" {
			fatalf("no source DSN found: provide --src or set SRC_POSTGRES_DSN_URL / SRC_DB_* or POSTGRES_DSN_URL / DB_*")
		}
		must(exportToFile(ctx, srcDSN, *schema, *table, *file))
		log.Printf("Exported %s.%s to %s", *schema, *table, *file)

	case "import":
		if *file == "" {
			fatalf("--file is required for import mode")
		}
		dstDSN := firstNonEmpty(*dstDSNFlag,
			dsnFromEnvPrefix("DST_"),
			dsnFromEnvPrefix(""),
		)
		if dstDSN == "" {
			fatalf("no destination DSN found: provide --dst or set DST_POSTGRES_DSN_URL / DST_DB_* or POSTGRES_DSN_URL / DB_*")
		}
		must(importFromFile(ctx, dstDSN, *schema, *table, *file))
		log.Printf("Imported %s into %s.%s", *file, *schema, *table)

	case "copy":
		srcDSN := firstNonEmpty(*srcDSNFlag,
			dsnFromEnvPrefix("SRC_"),
			dsnFromEnvPrefix(""),
		)
		dstDSN := firstNonEmpty(*dstDSNFlag,
			dsnFromEnvPrefix("DST_"),
			dsnFromEnvPrefix(""),
		)
		if srcDSN == "" || dstDSN == "" {
			fatalf("both source and destination DSNs required: use --src/--dst or env SRC_*/DST_* or DB_*")
		}
		n, err := copyBetween(ctx, srcDSN, dstDSN, *schema, *table)
		must(err)
		log.Printf("Copied %d bytes from %s to %s (%s.%s)", n, redactDSN(srcDSN), redactDSN(dstDSN), *schema, *table)

	default:
		fatalf("unknown --mode=%q (expected export|import|copy)", *mode)
	}
}

// exportToFile streams COPY TO STDOUT (binary) into a local file
func exportToFile(ctx context.Context, dsn, schema, table, path string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	cols, err := tableColumns(ctx, conn, schema, table)
	if err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sql := buildCopySelect(schema, table, cols)
	_, err = conn.PgConn().CopyTo(ctx, f, sql)
	return err
}

// importFromFile streams local file into COPY FROM STDIN (binary)
func importFromFile(ctx context.Context, dsn, schema, table, path string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	cols, err := tableColumns(ctx, conn, schema, table)
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sql := buildCopyInsert(schema, table, cols)
	_, err = conn.PgConn().CopyFrom(ctx, f, sql)
	return err
}

// copyBetween performs direct COPY stream from source to destination
func copyBetween(ctx context.Context, srcDSN, dstDSN, schema, table string) (int64, error) {
	src, err := pgx.Connect(ctx, srcDSN)
	if err != nil {
		return 0, err
	}
	defer src.Close(ctx)

	dst, err := pgx.Connect(ctx, dstDSN)
	if err != nil {
		return 0, err
	}
	defer dst.Close(ctx)

	cols, err := tableColumns(ctx, src, schema, table)
	if err != nil {
		return 0, err
	}

	pr, pw := io.Pipe()
	// counting writer wraps the pipe writer to count bytes
	cw := &countingWriter{w: pw}

	var wg sync.WaitGroup
	var copyToErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		sql := buildCopySelect(schema, table, cols)
		// Write from source into pipe
		_, copyToErr = src.PgConn().CopyTo(ctx, cw, sql)
		_ = pw.Close()
	}()

	sqlIns := buildCopyInsert(schema, table, cols)
	_, err = dst.PgConn().CopyFrom(ctx, pr, sqlIns)
	// Close reader side to unblock goroutine if needed
	_ = pr.Close()
	wg.Wait()
	if err != nil {
		return cw.n, err
	}
	if copyToErr != nil {
		return cw.n, copyToErr
	}
	return cw.n, nil
}

// tableColumns returns column names in physical order for a table
func tableColumns(ctx context.Context, conn *pgx.Conn, schema, table string) ([]string, error) {
	rows, err := conn.Query(ctx, `
        SELECT a.attname
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_attribute a ON a.attrelid = c.oid
        WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
        ORDER BY a.attnum
    `, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols = append(cols, pgx.Identifier{name}.Sanitize())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("no columns found for %s.%s", schema, table)
	}
	return cols, nil
}

// copyOutSource opens COPY TO STDOUT (binary) and returns a reader
func buildCopySelect(schema, table string, cols []string) string {
	selectList := join(cols, ", ")
	return fmt.Sprintf("COPY (SELECT %s FROM %s.%s) TO STDOUT WITH (FORMAT binary)", selectList,
		pgx.Identifier{schema}.Sanitize(), pgx.Identifier{table}.Sanitize())
}

func buildCopyInsert(schema, table string, cols []string) string {
	selectList := join(cols, ", ")
	return fmt.Sprintf("COPY %s.%s (%s) FROM STDIN WITH (FORMAT binary)",
		pgx.Identifier{schema}.Sanitize(), pgx.Identifier{table}.Sanitize(), selectList)
}

// dsnFromEnvPrefix builds DSN from env vars with optional prefix ("SRC_" or "DST_"),
// falling back to POSTGRES_DSN_URL if provided.
func dsnFromEnvPrefix(prefix string) string {
	// Full DSN first
	if v := os.Getenv(prefix + "POSTGRES_DSN_URL"); v != "" {
		return v
	}
	if prefix == "" { // also support unprefixed alias
		if v := os.Getenv("POSTGRES_DSN_URL"); v != "" {
			return v
		}
	}

	host := getenv(prefix+"DB_HOST", getenv("DB_HOST", ""))
	port := getenv(prefix+"DB_PORT", getenv("DB_PORT", "5432"))
	user := getenv(prefix+"DB_USER", getenv("DB_USER", ""))
	pass := getenv(prefix+"DB_PASSWORD", getenv("DB_PASSWORD", ""))
	name := getenv(prefix+"DB_NAME", getenv("DB_NAME", ""))
	sslm := getenv(prefix+"DB_SSLMODE", getenv("DB_SSLMODE", ""))

	if host == "" || user == "" || name == "" {
		return ""
	}
	// Append sslmode only if specified
	opts := ""
	if sslm != "" {
		opts = "?sslmode=" + sslm
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s%s", urlEncode(user), urlEncode(pass), host, port, name, opts)
}

// urlEncode minimal percent-escape for '@', ':' etc. (very small helper)
func urlEncode(s string) string {
	r := strings.NewReplacer("@", "%40", ":", "%3A", "/", "%2F", "?", "%3F", "#", "%23")
	return r.Replace(s)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatalf(format string, args ...any) { log.Fatalf(format, args...) }
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// join is an allocation-lean joiner for []string
func join(ss []string, sep string) string {
	switch len(ss) {
	case 0:
		return ""
	case 1:
		return ss[0]
	}
	n := 0
	for _, s := range ss {
		n += len(s)
	}
	n += len(sep) * (len(ss) - 1)
	b := make([]byte, 0, n)
	b = append(b, ss[0]...)
	for _, s := range ss[1:] {
		b = append(b, sep...)
		b = append(b, s...)
	}
	return string(b)
}

// redactDSN hides password in logs
func redactDSN(dsn string) string {
	// very rough redaction: user:pass@
	at := strings.Index(dsn, "@")
	if at == -1 {
		return dsn
	}
	start := strings.LastIndex(dsn[:at], ":")
	if start == -1 {
		return dsn
	}
	return dsn[:start+1] + "***" + dsn[at:]
}

// ensure we import errors for potential future use
var _ = errors.New

type countingWriter struct {
	w io.WriteCloser
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
func (c *countingWriter) Close() error { return c.w.Close() }
