package database

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// encryptedPrefix marks an entity_config value sealed with the config
// encryption key. Values without it are plaintext and are read as-is, so
// enabling encryption needs no data migration up front.
const encryptedPrefix = "enc:v1:"

// IsSecretConfigKey reports whether an entity_config key holds a credential
// that is encrypted at rest when a key is configured.
func IsSecretConfigKey(key string) bool {
	return strings.HasSuffix(key, "_token") &&
		(strings.HasPrefix(key, "devlake_") || strings.HasPrefix(key, "personal_"))
}

// ParseSecretKey decodes CONFIG_ENCRYPTION_KEY: 32 bytes as base64 or hex
// (e.g. `openssl rand -base64 32`).
func ParseSecretKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, errors.New("CONFIG_ENCRYPTION_KEY must be 32 bytes, base64- or hex-encoded (generate with: openssl rand -base64 32)")
}

type secretBox struct {
	aead cipher.AEAD
}

func newSecretBox(key []byte) (*secretBox, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &secretBox{aead: aead}, nil
}

func (b *secretBox) seal(plaintext string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (b *secretBox) open(value string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
	if err != nil {
		return "", err
	}
	n := b.aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("ciphertext too short")
	}
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func isEncryptedValue(v string) bool {
	return strings.HasPrefix(v, encryptedPrefix)
}

// EnableSecretEncryptionFromEnv enables encryption when CONFIG_ENCRYPTION_KEY
// is set and reports whether it did.
func (s *MySQLStore) EnableSecretEncryptionFromEnv() (bool, error) {
	v := os.Getenv("CONFIG_ENCRYPTION_KEY")
	if v == "" {
		return false, nil
	}
	key, err := ParseSecretKey(v)
	if err != nil {
		return false, err
	}
	return true, s.EnableSecretEncryption(key)
}

// EnableSecretEncryption makes the store encrypt credential values on write
// and decrypt them on read. Without it, encrypted values cannot be read.
func (s *MySQLStore) EnableSecretEncryption(key []byte) error {
	box, err := newSecretBox(key)
	if err != nil {
		return err
	}
	s.secrets = box
	return nil
}

// encodeConfigValue encrypts a credential value when encryption is enabled.
func (s *MySQLStore) encodeConfigValue(key, value string) (string, error) {
	if s.secrets == nil || value == "" || !IsSecretConfigKey(key) || isEncryptedValue(value) {
		return value, nil
	}
	return s.secrets.seal(value)
}

// decodeConfigValue decrypts an encrypted value. A value that cannot be
// decrypted reads as unset, so one bad row does not break the whole config;
// CheckSecretEncryption catches a missing or wrong key at startup.
func (s *MySQLStore) decodeConfigValue(entityType, entityID, key, value string) string {
	if !isEncryptedValue(value) {
		return value
	}
	if s.secrets == nil {
		slog.Error("encrypted config value but CONFIG_ENCRYPTION_KEY is not set", "entity_type", entityType, "entity_id", entityID, "key", key)
		return ""
	}
	plain, err := s.secrets.open(value)
	if err != nil {
		slog.Error("cannot decrypt config value", "entity_type", entityType, "entity_id", entityID, "key", key, "err", err)
		return ""
	}
	return plain
}

// CheckSecretEncryption fails when stored values are encrypted but no key is
// configured, or when the configured key cannot decrypt them.
func (s *MySQLStore) CheckSecretEncryption(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM entity_config WHERE config_value LIKE ?", encryptedPrefix+"%").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	if s.secrets == nil {
		return fmt.Errorf("%d entity_config values are encrypted but CONFIG_ENCRYPTION_KEY is not set", count)
	}
	var sample string
	if err := s.db.QueryRowContext(ctx,
		"SELECT config_value FROM entity_config WHERE config_value LIKE ? LIMIT 1", encryptedPrefix+"%").Scan(&sample); err != nil {
		return err
	}
	if _, err := s.secrets.open(sample); err != nil {
		return fmt.Errorf("CONFIG_ENCRYPTION_KEY cannot decrypt stored values: %w", err)
	}
	return nil
}

// EncryptExistingSecrets encrypts credential values still stored in
// plaintext and returns how many were changed.
func (s *MySQLStore) EncryptExistingSecrets(ctx context.Context) (int, error) {
	if s.secrets == nil {
		return 0, errors.New("encryption is not enabled")
	}
	return s.rewriteSecrets(ctx, func(v string) (string, bool, error) {
		if v == "" || isEncryptedValue(v) {
			return v, false, nil
		}
		sealed, err := s.secrets.seal(v)
		return sealed, true, err
	})
}

// DecryptAllSecrets writes every encrypted credential back as plaintext, for
// rolling back to a version without encryption support.
func (s *MySQLStore) DecryptAllSecrets(ctx context.Context) (int, error) {
	if s.secrets == nil {
		return 0, errors.New("encryption is not enabled")
	}
	return s.rewriteSecrets(ctx, func(v string) (string, bool, error) {
		if !isEncryptedValue(v) {
			return v, false, nil
		}
		plain, err := s.secrets.open(v)
		return plain, true, err
	})
}

type secretRow struct{ typ, id, key, value, orig string }

func (s *MySQLStore) collectSecrets(ctx context.Context, convert func(string) (string, bool, error)) ([]secretRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT entity_type, entity_id, config_key, COALESCE(config_value, '') FROM entity_config WHERE config_key LIKE ?", "%\\_token")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []secretRow
	for rows.Next() {
		var r secretRow
		if err := rows.Scan(&r.typ, &r.id, &r.key, &r.value); err != nil {
			return nil, err
		}
		if !IsSecretConfigKey(r.key) {
			continue
		}
		v, changed, err := convert(r.value)
		if err != nil {
			return nil, fmt.Errorf("%s %s %s: %w", r.typ, r.id, r.key, err)
		}
		if changed {
			r.orig, r.value = r.value, v
			pending = append(pending, r)
		}
	}
	return pending, rows.Err()
}

func (s *MySQLStore) rewriteSecrets(ctx context.Context, convert func(string) (string, bool, error)) (int, error) {
	pending, err := s.collectSecrets(ctx, convert)
	if err != nil {
		return 0, err
	}
	tx, err := s.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer rollbackTx(tx)
	changed := 0
	for _, r := range pending {
		// Matching the old value skips rows another writer changed meanwhile.
		res, err := tx.ExecContext(ctx,
			"UPDATE entity_config SET config_value=? WHERE entity_type=? AND entity_id=? AND config_key=? AND config_value=?",
			r.value, r.typ, r.id, r.key, r.orig)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err == nil {
			changed += int(n)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}
