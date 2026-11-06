package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBackupProducesUsableCopy(t *testing.T) {
	dir := t.TempDir()
	d, err := OpenPath(filepath.Join(dir, "main.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	ctx := context.Background()
	if _, err := d.ExecContext(ctx, `INSERT INTO kv(key, value) VALUES('probe', 'ok')`); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "backups", "copy.db")
	if err := Backup(ctx, d, dst); err != nil {
		t.Fatal(err)
	}
	if err := Backup(ctx, d, dst); err == nil {
		t.Fatal("backup must not overwrite an existing file")
	}
	c, err := OpenPath(dst)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	var v string
	if err := c.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='probe'`).Scan(&v); err != nil || v != "ok" {
		t.Fatalf("restored value %q, err %v", v, err)
	}
}
