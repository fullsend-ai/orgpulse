package server

import (
	"context"
	"strings"

	"github.com/fullsend-ai/orgpulse/internal/authz"
	"github.com/fullsend-ai/orgpulse/internal/database"
)

// Authorizer resolves a caller's Rover grant from the configured directory
// and the organization, product, and team records.
type Authorizer struct {
	store database.ConfigStore
	cfg   AuthConfig
	dir   authz.Directory
}

func newAuthorizer(store database.ConfigStore, cfg AuthConfig, dir authz.Directory) *Authorizer {
	if store == nil {
		return nil
	}
	return &Authorizer{store: store, cfg: cfg, dir: dir}
}

func (a *Authorizer) Resolve(ctx context.Context, id Identity) (authz.Grant, error) {
	tree, err := loadTree(ctx, a.store)
	if err != nil {
		return authz.Grant{}, err
	}
	if id.IsAdmin {
		return authz.AllowAll(tree), nil
	}
	groups := groupsFor(a.dir, id, a.cfg.EmailDomains)
	if a.cfg.TestMode {
		groups = mergeGroups(groups, a.cfg.TestGroups)
	}
	return authz.Resolve(tree, groups, a.cfg.OperatorGroups), nil
}

func groupsFor(dir authz.Directory, id Identity, emailDomains []string) []string {
	if dir == nil {
		return nil
	}
	keys := identityKeys(id, emailDomains)
	seen := map[string]struct{}{}
	var out []string
	for _, key := range keys {
		for _, group := range dir.Groups(key) {
			group = strings.TrimSpace(group)
			if group == "" {
				continue
			}
			lower := strings.ToLower(group)
			if _, ok := seen[lower]; ok {
				continue
			}
			seen[lower] = struct{}{}
			out = append(out, group)
		}
	}
	return out
}

func identityKeys(id Identity, emailDomains []string) []string {
	var keys []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			keys = append(keys, value)
		}
	}
	add(id.Username)
	add(id.Email)
	if strings.Contains(id.Email, "@") {
		if local, ok := emailLocalPart(id.Email, emailDomains); ok {
			add(local)
		}
	}
	return keys
}

func mergeGroups(groups, extra []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, group := range append(append([]string{}, groups...), extra...) {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		lower := strings.ToLower(group)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, group)
	}
	return out
}

func loadTree(ctx context.Context, store database.ConfigStore) (authz.Tree, error) {
	orgs, err := store.ListOrgRows(ctx)
	if err != nil {
		return authz.Tree{}, err
	}
	products, err := store.ListProductRows(ctx)
	if err != nil {
		return authz.Tree{}, err
	}
	teams, err := store.ListTeamRows(ctx)
	if err != nil {
		return authz.Tree{}, err
	}
	tree := authz.Tree{
		OrgProducts:  map[string][]string{},
		ProductTeams: map[string][]string{},
	}
	for _, org := range orgs {
		tree.Orgs = append(tree.Orgs, authz.Node{ID: org.ID, AuthGroup: org.AuthGroup})
		links, err := store.GetOrgProductLinks(ctx, org.ID)
		if err != nil {
			return authz.Tree{}, err
		}
		for _, link := range links {
			tree.OrgProducts[org.ID] = append(tree.OrgProducts[org.ID], link.ID)
		}
	}
	for _, product := range products {
		tree.Products = append(tree.Products, authz.Node{ID: product.ID, AuthGroup: product.AuthGroup})
	}
	for _, team := range teams {
		tree.Teams = append(tree.Teams, authz.Node{ID: team.ID, AuthGroup: team.AuthGroup})
	}
	links, err := store.ListAllTeamProductLinks(ctx)
	if err != nil {
		return authz.Tree{}, err
	}
	for _, link := range links {
		tree.ProductTeams[link.ProductID] = append(tree.ProductTeams[link.ProductID], link.TeamID)
	}
	return tree, nil
}
