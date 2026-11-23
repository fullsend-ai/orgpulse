package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

// Identity is the caller identity determined by forwarded proxy headers,
// dev/test overrides, or API keys.
type Identity struct {
	Username string   `json:"username"`
	Email    string   `json:"email"`
	IsAdmin  bool     `json:"is_admin"`
	IsAPIKey bool     `json:"is_api_key"`
	KeyName  string   `json:"key_name,omitempty"`
	Roles    []string `json:"roles"`
}

// Known reports whether the identity has been authenticated (via proxy, dev override,
// test mode, or API key).
func (id Identity) Known() bool {
	return id.Username != "" || id.Email != "" || id.IsAPIKey
}

// HasRole reports whether the identity possesses the specified role (case-insensitive).
func (id Identity) HasRole(role string) bool {
	for _, r := range id.Roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}

// checkAdminUser checks whether the given username or email matches any entry in adminUsers.
// Supports case-insensitive matching, email prefix matching, and wildcard "*".
func checkAdminUser(username, email string, adminUsers []string) bool {
	return matchAdminUser(username, email, adminUsers, nil)
}

// matchAdminUser is checkAdminUser with the email-prefix match limited to
// emailDomains. An empty emailDomains keeps prefix matching for every domain.
func matchAdminUser(username, email string, adminUsers, emailDomains []string) bool {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	for _, admin := range adminUsers {
		admin = strings.TrimSpace(admin)
		if admin == "" {
			continue
		}
		if admin == "*" {
			return true
		}
		if username != "" && strings.EqualFold(admin, username) {
			return true
		}
		if email != "" {
			if strings.EqualFold(admin, email) {
				return true
			}
			if local, ok := emailLocalPart(email, emailDomains); ok && strings.EqualFold(admin, local) {
				return true
			}
		}
	}
	return false
}

// emailLocalPart returns the part of email before "@" when its domain is in
// domains, or when domains is empty. A bare name in ADMIN_USERS or a Rover
// export only identifies someone within a known email domain.
func emailLocalPart(email string, domains []string) (string, bool) {
	local, domain, found := strings.Cut(strings.TrimSpace(email), "@")
	if !found {
		return local, len(domains) == 0
	}
	if len(domains) == 0 {
		return local, true
	}
	for _, d := range domains {
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(d), "@"), domain) {
			return local, true
		}
	}
	return "", false
}

// identityFromRequest reads the identity the ose-oauth-proxy-rhel9 sidecar
// forwards on every request it has already authenticated.
func identityFromRequest(r *http.Request) Identity {
	return Identity{
		Username: strings.TrimSpace(r.Header.Get("X-Forwarded-User")),
		Email:    strings.TrimSpace(r.Header.Get("X-Forwarded-Email")),
	}
}

// AuthConfig defines authorization and identity evaluation configuration.
type AuthConfig struct {
	AdminUsers       []string
	DevAdminOverride bool
	TestMode         bool
	TestEmail        string
	TestIsAdmin      bool
	TestGroups       []string
	OperatorGroups   []string
	// EmailDomains limits email-prefix matching against ADMIN_USERS and the
	// Rover directory to these domains (AUTH_EMAIL_DOMAINS). Empty keeps the
	// legacy behaviour of matching the prefix of any domain.
	EmailDomains []string
}

// ParseAdminUsers splits a comma-separated string of usernames into a slice.
func ParseAdminUsers(s string) []string {
	var res []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, `"'`)
		part = strings.TrimSpace(part)
		if part != "" {
			res = append(res, part)
		}
	}
	return res
}

// authenticateRequest evaluates the caller identity from the request according to the RBAC hierarchy:
// 1. Dev Admin Override
// 2. Test Mode
// 3. Forwarded Identity (X-Forwarded-User, X-Forwarded-Email)
// 4. API Key Auth
// 5. Anonymous Fallback
func authenticateRequest(r *http.Request, store database.ConfigStore, cfg AuthConfig) (Identity, error) {
	// 1. Dev Admin Override
	if cfg.DevAdminOverride {
		fwdUser := strings.TrimSpace(r.Header.Get("X-Forwarded-User"))
		fwdEmail := strings.TrimSpace(r.Header.Get("X-Forwarded-Email"))
		username := fwdUser
		email := fwdEmail
		if username == "" && email == "" {
			if cfg.TestMode && cfg.TestEmail != "" {
				username = cfg.TestEmail
				email = cfg.TestEmail
			} else {
				username = "dev-admin"
				email = "dev-admin@example.com"
			}
		} else if email == "" && strings.Contains(username, "@") {
			email = username
		}
		return Identity{
			Username: username,
			Email:    email,
			IsAdmin:  true,
			Roles:    []string{"admin", "contributor"},
		}, nil
	}

	// 2. Test Mode
	if cfg.TestMode {
		isAdmin := cfg.TestIsAdmin || matchAdminUser(cfg.TestEmail, cfg.TestEmail, cfg.AdminUsers, cfg.EmailDomains)
		roles := []string{"contributor"}
		if isAdmin {
			roles = []string{"admin", "contributor"}
		}
		return Identity{
			Username: cfg.TestEmail,
			Email:    cfg.TestEmail,
			IsAdmin:  isAdmin,
			Roles:    roles,
		}, nil
	}

	// 3. Forwarded Identity from OAuth Proxy
	fwdUser := strings.TrimSpace(r.Header.Get("X-Forwarded-User"))
	fwdEmail := strings.TrimSpace(r.Header.Get("X-Forwarded-Email"))
	if fwdUser != "" || fwdEmail != "" {
		isAdmin := matchAdminUser(fwdUser, fwdEmail, cfg.AdminUsers, cfg.EmailDomains)
		roles := []string{"contributor"}
		if isAdmin {
			roles = []string{"admin", "contributor"}
		}
		return Identity{
			Username: fwdUser,
			Email:    fwdEmail,
			IsAdmin:  isAdmin,
			Roles:    roles,
		}, nil
	}

	// 4. API Key Auth
	key := extractAPIKey(r)
	if key != "" && store != nil {
		info, err := store.ValidateAPIKey(r.Context(), database.HashAPIKey(key))
		if err != nil {
			return Identity{}, err
		}
		if info != nil {
			roles := []string{"contributor"}
			if info.IsAdmin {
				roles = []string{"admin", "contributor"}
			}
			username := info.Name
			email := ""
			if info.ScopedUser != "" {
				username = info.ScopedUser
				if strings.Contains(info.ScopedUser, "@") {
					email = info.ScopedUser
				}
				isAdmin := info.IsAdmin || matchAdminUser(username, email, cfg.AdminUsers, cfg.EmailDomains)
				if isAdmin {
					roles = []string{"admin", "contributor"}
				}
			}
			return Identity{
				Username: username,
				Email:    email,
				IsAdmin:  info.IsAdmin,
				IsAPIKey: true,
				KeyName:  info.Name,
				Roles:    roles,
			}, nil
		}
	}

	// 5. Anonymous Fallback
	return Identity{
		Roles: []string{"anonymous"},
	}, nil
}

type identityContextKey struct{}

func withIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, id)
}

// identityFromContext returns the identity apiKeyAuth/RBAC attached to the
// request context, if any.
func identityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityContextKey{}).(Identity)
	return id, ok
}
