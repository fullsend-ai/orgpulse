package server

import (
	"context"
	"net/http"

	"github.com/fullsend-ai/orgpulse/internal/authz"
	"github.com/fullsend-ai/orgpulse/internal/database"
)

type grantContextKey struct{}

func withGrant(ctx context.Context, grant authz.Grant) context.Context {
	return context.WithValue(ctx, grantContextKey{}, grant)
}

func grantFromContext(ctx context.Context) (authz.Grant, bool) {
	grant, ok := ctx.Value(grantContextKey{}).(authz.Grant)
	return grant, ok
}

func attachGrant(ctx context.Context, id Identity, az *Authorizer) (context.Context, error) {
	if id.IsAdmin && az == nil {
		return withGrant(ctx, authz.AllowAll(authz.Tree{})), nil
	}
	if az == nil {
		return withGrant(ctx, authz.Grant{}), nil
	}
	grant, err := az.Resolve(ctx, id)
	if err != nil {
		return ctx, err
	}
	return withGrant(ctx, grant), nil
}

// tenantAllows reports whether this request may perform allow.
// Handlers invoked without a grant, such as direct unit tests, are unchanged.
func tenantAllows(r *http.Request, allow func(authz.Grant) bool) bool {
	grant, ok := grantFromContext(r.Context())
	if !ok {
		return true
	}
	if grant.Bypass {
		return true
	}
	return allow(grant)
}

func visible(w http.ResponseWriter, r *http.Request, allow func(authz.Grant) bool) bool {
	if tenantAllows(r, allow) {
		return true
	}
	writeError(w, http.StatusNotFound, "not found")
	return false
}

func writable(w http.ResponseWriter, r *http.Request, allow func(authz.Grant) bool) bool {
	if tenantAllows(r, allow) {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden")
	return false
}

func filterOrgs(grant authz.Grant, orgs []database.Org) []database.Org {
	if grant.Bypass || grant.Operator {
		if orgs == nil {
			return []database.Org{}
		}
		return orgs
	}
	out := make([]database.Org, 0, len(orgs))
	for _, org := range orgs {
		if !grant.CanReadOrg(org.ID) {
			continue
		}
		if !grant.CanAggregateOrg(org.ID) {
			org.Dashboards = nil
			org.Config = nil
			org.Products = filterIDs(org.Products, grant.CanReadProduct)
		}
		out = append(out, org)
	}
	return out
}

func filterProducts(grant authz.Grant, products []database.Product) []database.Product {
	if grant.Bypass || grant.Operator {
		if products == nil {
			return []database.Product{}
		}
		return products
	}
	out := make([]database.Product, 0, len(products))
	for _, product := range products {
		if !grant.CanReadProduct(product.ID) {
			continue
		}
		if !grant.CanAggregateProduct(product.ID) {
			product.Dashboards = nil
			product.Config = nil
			product.Teams = filterIDs(product.Teams, grant.CanReadTeam)
		}
		out = append(out, product)
	}
	return out
}

func filterTeams(grant authz.Grant, teams []database.Team) []database.Team {
	if grant.Bypass || grant.Operator {
		if teams == nil {
			return []database.Team{}
		}
		return teams
	}
	out := make([]database.Team, 0, len(teams))
	for _, team := range teams {
		if grant.CanReadTeam(team.ID) {
			out = append(out, team)
		}
	}
	return out
}

func filterIDs(ids []string, allow func(string) bool) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if allow(id) {
			out = append(out, id)
		}
	}
	return out
}

func canReadOwner(grant authz.Grant, ownerType, ownerID string) bool {
	switch ownerType {
	case "org":
		return grant.CanAggregateOrg(ownerID)
	case "product":
		return grant.CanAggregateProduct(ownerID)
	case "team":
		return grant.CanReadTeam(ownerID)
	default:
		return false
	}
}

func canWriteOwner(grant authz.Grant, ownerType, ownerID string) bool {
	switch ownerType {
	case "org":
		return grant.CanUpdateOrg(ownerID)
	case "product":
		return grant.CanUpdateProduct(ownerID)
	case "team":
		return grant.CanUpdateTeam(ownerID)
	default:
		return false
	}
}
