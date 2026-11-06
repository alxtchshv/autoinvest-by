// Package db открывает SQLite (pure Go, без CGO) и применяет миграции.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// Open открывает базу в каталоге dataDir и накатывает миграции.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "autoinvest.db")
	return OpenPath(path)
}

// OpenPath — то же для произвольного пути (":memory:" в тестах не годится из-за пула соединений,
// поэтому тесты используют временный файл).
func OpenPath(path string) (*sql.DB, error) {
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	d, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite пишет одним писателем; небольшой пул убирает лишние SQLITE_BUSY.
	d.SetMaxOpenConns(4)
	if err := d.Ping(); err != nil {
		return nil, fmt.Errorf("sqlite ping: %w", err)
	}
	if err := migrate(context.Background(), d); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func migrate(ctx context.Context, d *sql.DB) error {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	for i, m := range migrations {
		v := i + 1
		var n int
		if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=?`, v).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", v, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?, strftime('%s','now'))`, v); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Backup делает согласованную копию базы в файл dst через VACUUM INTO: сервис продолжает
// работать, копия не содержит «половинок» транзакций и сразу пригодна для восстановления.
func Backup(ctx context.Context, d *sql.DB, dst string) error {
	dst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("файл %s уже существует", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if _, err := d.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	// проверяем, что копия открывается и цела
	chk, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(dst)+"?mode=ro")
	if err != nil {
		return err
	}
	defer chk.Close()
	var res string
	if err := chk.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("копия повреждена: %s", res)
	}
	return nil
}

// Tx выполняет fn в транзакции.
func Tx(ctx context.Context, d *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
