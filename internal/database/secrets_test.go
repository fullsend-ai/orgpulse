package database

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func testKey() []byte { return bytes.Repeat([]byte{7}, 32) }

func TestParseSecretKey(t *testing.T) {
	key := testKey()
	for _, in := range []string{base64.StdEncoding.EncodeToString(key), hex.EncodeToString(key), " " + base64.StdEncoding.EncodeToString(key) + "\n"} {
		got, err := ParseSecretKey(in)
		if err != nil || !bytes.Equal(got, key) {
			t.Errorf("ParseSecretKey(%q) = %x, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "short", base64.StdEncoding.EncodeToString(key[:16])} {
		if _, err := ParseSecretKey(in); err == nil {
			t.Errorf("ParseSecretKey(%q) accepted", in)
		}
	}
}

func TestIsSecretConfigKey(t *testing.T) {
	for k, want := range map[string]bool{
		"devlake_github_token":        true,
		"devlake_jira_token":          true,
		"personal_gitlab_token":       true,
		"devlake_github_token_expiry": false,
		"devlake_jira_username":       false,
		"my_custom_token":             false,
		"personal_github_endpoint":    false,
	} {
		if got := IsSecretConfigKey(k); got != want {
			t.Errorf("IsSecretConfigKey(%q) = %v, want %v", k, got, want)
		}
	}
}

func TestConfigValueEncryption(t *testing.T) {
	s := &MySQLStore{}
	if err := s.EnableSecretEncryption(testKey()); err != nil {
		t.Fatal(err)
	}
	enc, err := s.encodeConfigValue("devlake_github_token", "ghp_secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, encryptedPrefix) || strings.Contains(enc, "ghp_secret") {
		t.Fatalf("not encrypted: %q", enc)
	}
	again, _ := s.encodeConfigValue("devlake_github_token", "ghp_secret")
	if again == enc {
		t.Error("nonce reused")
	}
	if got := s.decodeConfigValue("team", "t", "devlake_github_token", enc); got != "ghp_secret" {
		t.Errorf("decode = %q", got)
	}
	if twice, _ := s.encodeConfigValue("devlake_github_token", enc); twice != enc {
		t.Error("already-encrypted value encrypted again")
	}
	for k, v := range map[string]string{"devlake_jira_username": "bob", "devlake_github_token": "", "color": "blue"} {
		if got, _ := s.encodeConfigValue(k, v); got != v {
			t.Errorf("%s=%q changed to %q", k, v, got)
		}
	}
	if got := s.decodeConfigValue("team", "t", "devlake_github_token", "plain-legacy"); got != "plain-legacy" {
		t.Errorf("legacy plaintext changed: %q", got)
	}

	other := &MySQLStore{}
	if err := other.EnableSecretEncryption(bytes.Repeat([]byte{9}, 32)); err != nil {
		t.Fatal(err)
	}
	if got := other.decodeConfigValue("team", "t", "devlake_github_token", enc); got != "" {
		t.Errorf("wrong key decoded to %q", got)
	}
	if got := (&MySQLStore{}).decodeConfigValue("team", "t", "devlake_github_token", enc); got != "" {
		t.Errorf("no key decoded to %q", got)
	}
	if got, _ := (&MySQLStore{}).encodeConfigValue("devlake_github_token", "ghp"); got != "ghp" {
		t.Errorf("encryption disabled but value changed: %q", got)
	}
}
