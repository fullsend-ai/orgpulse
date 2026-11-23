package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

type APIKeyInfo struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	IsAdmin    bool   `json:"is_admin"`
	ScopedUser string `json:"scoped_user,omitempty"`
	CreatedAt  string `json:"created_at"`
}

func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", h)
}

func (s *MySQLStore) ValidateAPIKey(ctx context.Context, keyHash string) (*APIKeyInfo, error) {
	var info APIKeyInfo
	var createdAt time.Time
	var scopedUser sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, is_admin, scoped_user, created_at FROM api_keys WHERE key_hash=?",
		keyHash).Scan(&info.ID, &info.Name, &info.IsAdmin, &scopedUser, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info.ScopedUser = scopedUser.String
	info.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	go s.updateKeyLastUsed(ctx, info.ID)
	return &info, nil
}

func (s *MySQLStore) updateKeyLastUsed(parent context.Context, id int64) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, "UPDATE api_keys SET last_used_at=NOW() WHERE id=?", id)
	if err != nil {
		slog.Error("update key last used", "error", err)
	}
}

func (s *MySQLStore) CreateAPIKey(ctx context.Context, name, keyHash string, isAdmin bool, scopedUser string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO api_keys (name, key_hash, is_admin, scoped_user) VALUES (?, ?, ?, ?)",
		name, keyHash, isAdmin, nullIfEmpty(scopedUser))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *MySQLStore) ListAPIKeys(ctx context.Context) ([]APIKeyInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, is_admin, scoped_user, created_at FROM api_keys ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKeyInfo
	for rows.Next() {
		var k APIKeyInfo
		var createdAt time.Time
		var scopedUser sql.NullString
		if err := rows.Scan(&k.ID, &k.Name, &k.IsAdmin, &scopedUser, &createdAt); err != nil {
			return nil, err
		}
		k.ScopedUser = scopedUser.String
		k.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *MySQLStore) DeleteAPIKey(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM api_keys WHERE id=?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *MySQLStore) EnsureAdminKey(ctx context.Context, rawKey string) error {
	h := HashAPIKey(rawKey)
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM api_keys WHERE key_hash=?", h).Scan(&exists)
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO api_keys (name, key_hash, is_admin) VALUES ('bootstrap-admin', ?, TRUE)", h)
	return err
}
