package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

func TestIdentityFromRequest_BothHeadersPresent(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-User", "alice")
	r.Header.Set("X-Forwarded-Email", "alice@example.com")

	id := identityFromRequest(r)
	if id.Username != "alice" {
		t.Errorf("expected username=alice, got %q", id.Username)
	}
	if id.Email != "alice@example.com" {
		t.Errorf("expected email=alice@example.com, got %q", id.Email)
	}
	if !id.Known() {
		t.Error("expected Known() to be true")
	}
}

func TestIdentityFromRequest_NoHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)

	id := identityFromRequest(r)
	if id.Known() {
		t.Error("expected Known() to be false with no forwarded headers")
	}
	if id.Username != "" || id.Email != "" {
		t.Errorf("expected empty identity, got %+v", id)
	}
}

func TestIdentityFromRequest_UsernameOnly_NoEmail(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-User", "bob")

	id := identityFromRequest(r)
	if !id.Known() {
		t.Error("expected Known() to be true with only X-Forwarded-User set")
	}
	if id.Email != "" {
		t.Errorf("expected empty email, got %q", id.Email)
	}
}

func TestIdentity_Known(t *testing.T) {
	tests := []struct {
		name string
		id   Identity
		want bool
	}{
		{"empty", Identity{}, false},
		{"username only", Identity{Username: "alice"}, true},
		{"email only, no username", Identity{Email: "alice@example.com"}, true},
		{"api key only", Identity{IsAPIKey: true, KeyName: "ci"}, true},
		{"both set", Identity{Username: "alice", Email: "alice@example.com"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.id.Known(); got != tt.want {
				t.Errorf("Known() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdentity_HasRole(t *testing.T) {
	id := Identity{Roles: []string{"Admin", "contributor"}}
	if !id.HasRole("admin") {
		t.Error("expected HasRole('admin') to be true (case-insensitive)")
	}
	if !id.HasRole("CONTRIBUTOR") {
		t.Error("expected HasRole('CONTRIBUTOR') to be true")
	}
	if id.HasRole("viewer") {
		t.Error("expected HasRole('viewer') to be false")
	}
}

func TestCheckAdminUser(t *testing.T) {
	tests := []struct {
		name       string
		username   string
		email      string
		adminUsers []string
		want       bool
	}{
		{"exact match username", "jdoe", "", []string{"jdoe", "alice"}, true},
		{"case-insensitive match username", "JDOE", "", []string{"jdoe"}, true},
		{"case-insensitive admin in list", "jdoe", "", []string{"JDOE"}, true},
		{"whitespace in admin list entry", "jdoe", "", []string{"  jdoe  "}, true},
		{"whitespace in username", "  jdoe  ", "", []string{"jdoe"}, true},
		{"exact match email", "", "jdoe@example.com", []string{"jdoe@example.com"}, true},
		{"case-insensitive email", "", "JDOE@EXAMPLE.COM", []string{"jdoe@example.com"}, true},
		{"email prefix match", "", "jdoe@example.com", []string{"jdoe"}, true},
		{"case-insensitive email prefix match", "", "JDOE@EXAMPLE.COM", []string{"jdoe"}, true},
		{"wildcard match", "stranger", "stranger@example.com", []string{"*"}, true},
		{"wildcard in list", "stranger", "", []string{"alice", "*", "bob"}, true},
		{"no match", "stranger", "stranger@example.com", []string{"jdoe", "alice"}, false},
		{"empty admin list", "jdoe", "jdoe@example.com", nil, false},
		{"empty user and email", "", "", []string{"jdoe"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkAdminUser(tt.username, tt.email, tt.adminUsers)
			if got != tt.want {
				t.Errorf("checkAdminUser(%q, %q, %v) = %v, want %v", tt.username, tt.email, tt.adminUsers, got, tt.want)
			}
		})
	}
}

func TestParseAdminUsers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "standard comma list with whitespace",
			input: "alice, bob,  jdoe ,",
			want:  []string{"alice", "bob", "jdoe"},
		},
		{
			name:  "double and single quotes with spaces",
			input: `"alice", 'bob', " jdoe " , ' charlie ' , `,
			want:  []string{"alice", "bob", "jdoe", "charlie"},
		},
		{
			name:  "empty and whitespace only tokens",
			input: `"", '', "  ",   , `,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAdminUsers(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseAdminUsers(%q) len = %d, want %d (%v vs %v)", tt.input, len(got), len(tt.want), got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("ParseAdminUsers(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestAuthenticateRequest_DevAdminOverride(t *testing.T) {
	cfg := AuthConfig{DevAdminOverride: true}
	r := httptest.NewRequest("GET", "/", nil)
	id, err := authenticateRequest(r, nil, cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !id.IsAdmin || !id.HasRole("admin") || !id.HasRole("contributor") {
		t.Errorf("expected admin identity with dev override, got %+v", id)
	}
	if id.Username != "dev-admin" || id.Email != "dev-admin@example.com" {
		t.Errorf("expected default dev-admin credentials, got %+v", id)
	}
}

func TestAuthenticateRequest_ForwardedIdentity_Admin(t *testing.T) {
	cfg := AuthConfig{AdminUsers: []string{"alice"}}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-User", "alice")
	r.Header.Set("X-Forwarded-Email", "alice@example.com")

	id, err := authenticateRequest(r, nil, cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !id.IsAdmin || !id.HasRole("admin") {
		t.Errorf("expected admin identity for alice, got %+v", id)
	}
	if id.Username != "alice" || id.Email != "alice@example.com" {
		t.Errorf("expected alice identity, got %+v", id)
	}
}

func TestAuthenticateRequest_ForwardedIdentity_NonAdmin(t *testing.T) {
	cfg := AuthConfig{AdminUsers: []string{"alice"}}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-User", "bob")
	r.Header.Set("X-Forwarded-Email", "bob@example.com")

	id, err := authenticateRequest(r, nil, cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id.IsAdmin || id.HasRole("admin") {
		t.Errorf("expected non-admin identity for bob, got %+v", id)
	}
	if !id.HasRole("contributor") {
		t.Errorf("expected contributor role for bob, got %+v", id)
	}
}

func TestAuthenticateRequest_APIKey(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	_, err := store.CreateAPIKey(ctx, "ci-runner", database.HashAPIKey("valid-admin-key"), true, "")
	if err != nil {
		t.Errorf("CreateAPIKey: %v", err)
	}
	_, err = store.CreateAPIKey(ctx, "viewer-bot", database.HashAPIKey("valid-user-key"), false, "")
	if err != nil {
		t.Errorf("CreateAPIKey: %v", err)
	}

	// Admin API key
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-API-Key", "valid-admin-key")
	id, err := authenticateRequest(r, store, AuthConfig{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !id.IsAdmin || !id.IsAPIKey || id.Username != "ci-runner" {
		t.Errorf("expected admin api key identity, got %+v", id)
	}

	// Non-admin API key
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-API-Key", "valid-user-key")
	id, err = authenticateRequest(r, store, AuthConfig{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id.IsAdmin || !id.IsAPIKey || id.Username != "viewer-bot" {
		t.Errorf("expected regular api key identity, got %+v", id)
	}
}

func TestWithIdentity_RoundTrip(t *testing.T) {
	want := Identity{Username: "alice", Email: "alice@example.com"}
	ctx := withIdentity(context.Background(), want)

	got, ok := identityFromContext(ctx)
	if !ok {
		t.Fatal("expected identityFromContext to report ok=true")
	}
	if got.Username != want.Username || got.Email != want.Email {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestIdentityFromContext_NotSet(t *testing.T) {
	_, ok := identityFromContext(context.Background())
	if ok {
		t.Error("expected ok=false for a context with no identity attached")
	}
}
