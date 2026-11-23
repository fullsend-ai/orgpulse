package authz

import (
	"sort"
	"strings"
)

// Node is an organization, product, or team and the Rover group that grants it.
type Node struct {
	ID        string
	AuthGroup string
}

// Tree is the configured organization, product, and team hierarchy.
// OrgProducts maps an organization id to its product ids.
// ProductTeams maps a product id to its team ids.
type Tree struct {
	Orgs         []Node
	Products     []Node
	Teams        []Node
	OrgProducts  map[string][]string
	ProductTeams map[string][]string
}

// Grant is the set of organizations, products, and teams a person may see.
// Aggregate access includes that object's dashboards and its full child list.
// ProductFromOrg marks products covered by an organization grant.
type Grant struct {
	Bypass           bool
	Operator         bool
	Orgs             map[string]struct{}
	Products         map[string]struct{}
	Teams            map[string]struct{}
	OrgAggregate     map[string]struct{}
	ProductAggregate map[string]struct{}
	ProductFromOrg   map[string]struct{}
}

// Scope is one visible organization or product in an identity payload.
type Scope struct {
	ID        string `json:"id"`
	Aggregate bool   `json:"aggregate"`
}

// Summary is the grant returned by GET /api/v1/auth/me.
type Summary struct {
	Operator bool     `json:"operator"`
	Orgs     []Scope  `json:"orgs"`
	Products []Scope  `json:"products"`
	Teams    []string `json:"teams"`
}

func newGrant() Grant {
	return Grant{
		Orgs:             map[string]struct{}{},
		Products:         map[string]struct{}{},
		Teams:            map[string]struct{}{},
		OrgAggregate:     map[string]struct{}{},
		ProductAggregate: map[string]struct{}{},
		ProductFromOrg:   map[string]struct{}{},
	}
}

// AllowAll marks every node visible and aggregate, and bypasses further checks.
func AllowAll(tree Tree) Grant {
	g := cover(tree)
	g.Operator = false
	g.Bypass = true
	return g
}

// Resolve matches groups to auth_group values. operatorGroups see the whole tree.
// A blank auth group matches nobody. Team access does not walk up to the product
// or organization. Wider grants add every child of that node.
func Resolve(tree Tree, groups, operatorGroups []string) Grant {
	g := newGrant()
	have := groupSet(groups)
	if overlaps(have, groupSet(operatorGroups)) {
		covered := cover(tree)
		covered.Operator = true
		return covered
	}
	orgGrant := map[string]struct{}{}
	productGrant := map[string]struct{}{}
	teamGrant := map[string]struct{}{}
	for _, org := range tree.Orgs {
		if member(have, org.AuthGroup) {
			orgGrant[org.ID] = struct{}{}
		}
	}
	for _, product := range tree.Products {
		if member(have, product.AuthGroup) {
			productGrant[product.ID] = struct{}{}
		}
	}
	for _, team := range tree.Teams {
		if member(have, team.AuthGroup) {
			teamGrant[team.ID] = struct{}{}
		}
	}
	expand(tree, g, orgGrant, productGrant, teamGrant)
	return g
}

func cover(tree Tree) Grant {
	g := newGrant()
	for _, org := range tree.Orgs {
		g.Orgs[org.ID] = struct{}{}
		g.OrgAggregate[org.ID] = struct{}{}
	}
	for _, product := range tree.Products {
		g.Products[product.ID] = struct{}{}
		g.ProductAggregate[product.ID] = struct{}{}
		g.ProductFromOrg[product.ID] = struct{}{}
	}
	for _, team := range tree.Teams {
		g.Teams[team.ID] = struct{}{}
	}
	return g
}

func expand(tree Tree, g Grant, orgGrant, productGrant, teamGrant map[string]struct{}) {
	for id := range orgGrant {
		g.Orgs[id] = struct{}{}
		g.OrgAggregate[id] = struct{}{}
		for _, productID := range tree.OrgProducts[id] {
			markProductFromOrg(g, productID)
			for _, teamID := range tree.ProductTeams[productID] {
				g.Teams[teamID] = struct{}{}
			}
		}
	}
	for id := range productGrant {
		g.Products[id] = struct{}{}
		g.ProductAggregate[id] = struct{}{}
		for _, teamID := range tree.ProductTeams[id] {
			g.Teams[teamID] = struct{}{}
		}
	}
	for id := range teamGrant {
		g.Teams[id] = struct{}{}
	}
	addParents(tree, g)
}

func markProductFromOrg(g Grant, productID string) {
	g.Products[productID] = struct{}{}
	g.ProductAggregate[productID] = struct{}{}
	g.ProductFromOrg[productID] = struct{}{}
}

func addParents(tree Tree, g Grant) {
	productOrg := map[string]string{}
	for orgID, products := range tree.OrgProducts {
		for _, productID := range products {
			productOrg[productID] = orgID
		}
	}
	teamProducts := map[string][]string{}
	for productID, teams := range tree.ProductTeams {
		for _, teamID := range teams {
			teamProducts[teamID] = append(teamProducts[teamID], productID)
		}
	}
	for teamID := range g.Teams {
		for _, productID := range teamProducts[teamID] {
			g.Products[productID] = struct{}{}
			if orgID := productOrg[productID]; orgID != "" {
				g.Orgs[orgID] = struct{}{}
			}
		}
	}
}

func (g Grant) Summary() Summary {
	summary := Summary{
		Operator: g.Operator,
		Orgs:     []Scope{},
		Products: []Scope{},
		Teams:    []string{},
	}
	for id := range g.Orgs {
		_, aggregate := g.OrgAggregate[id]
		summary.Orgs = append(summary.Orgs, Scope{ID: id, Aggregate: aggregate || g.Bypass || g.Operator})
	}
	for id := range g.Products {
		_, aggregate := g.ProductAggregate[id]
		summary.Products = append(summary.Products, Scope{ID: id, Aggregate: aggregate || g.Bypass || g.Operator})
	}
	for id := range g.Teams {
		summary.Teams = append(summary.Teams, id)
	}
	sort.Slice(summary.Orgs, func(i, j int) bool { return summary.Orgs[i].ID < summary.Orgs[j].ID })
	sort.Slice(summary.Products, func(i, j int) bool { return summary.Products[i].ID < summary.Products[j].ID })
	sort.Strings(summary.Teams)
	return summary
}

func (g Grant) CanReadOrg(id string) bool      { return g.allowed(g.Orgs, id) }
func (g Grant) CanReadProduct(id string) bool  { return g.allowed(g.Products, id) }
func (g Grant) CanReadTeam(id string) bool     { return g.allowed(g.Teams, id) }
func (g Grant) CanAggregateOrg(id string) bool { return g.allowed(g.OrgAggregate, id) }
func (g Grant) CanAggregateProduct(id string) bool {
	return g.allowed(g.ProductAggregate, id)
}

func (g Grant) CanCreateOrg() bool { return g.Bypass || g.Operator }

func (g Grant) CanUpdateOrg(id string) bool { return g.allowed(g.OrgAggregate, id) }

func (g Grant) CanCreateProduct() bool {
	return g.Bypass || g.Operator || len(g.OrgAggregate) > 0
}

func (g Grant) CanUpdateProduct(id string) bool { return g.allowed(g.ProductAggregate, id) }

func (g Grant) CanChangeProductGroup(id string) bool {
	return g.allowed(g.ProductFromOrg, id)
}

func (g Grant) CanUpdateTeam(id string) bool { return g.allowed(g.Teams, id) }

func (g Grant) CanOnboard(productID string) bool {
	if productID == "" {
		return g.Bypass || g.Operator
	}
	return g.CanAggregateProduct(productID)
}

func (g Grant) allowed(set map[string]struct{}, id string) bool {
	if g.Bypass || g.Operator {
		return true
	}
	_, ok := set[id]
	return ok
}

func groupSet(groups []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		set[strings.ToLower(group)] = struct{}{}
	}
	return set
}

func member(set map[string]struct{}, authGroup string) bool {
	authGroup = strings.TrimSpace(authGroup)
	if authGroup == "" {
		return false
	}
	_, ok := set[strings.ToLower(authGroup)]
	return ok
}

func overlaps(a, b map[string]struct{}) bool {
	for key := range a {
		if _, ok := b[key]; ok {
			return true
		}
	}
	return false
}
