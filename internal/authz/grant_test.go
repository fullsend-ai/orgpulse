package authz

import "testing"

func sampleTree() Tree {
	return Tree{
		Orgs: []Node{
			{ID: "rhai", AuthGroup: "rhai-org"},
			{ID: "central", AuthGroup: "central-org"},
		},
		Products: []Node{
			{ID: "p1", AuthGroup: "p1-group"},
			{ID: "p2", AuthGroup: "p2-group"},
		},
		Teams: []Node{
			{ID: "t1", AuthGroup: "t1-group"},
			{ID: "t2", AuthGroup: "t2-group"},
			{ID: "t3", AuthGroup: ""},
		},
		OrgProducts: map[string][]string{
			"rhai":    {"p1"},
			"central": {"p2"},
		},
		ProductTeams: map[string][]string{
			"p1": {"t1", "t2"},
			"p2": {"t3"},
		},
	}
}

func TestResolveTeamGrantDoesNotWalkUp(t *testing.T) {
	g := Resolve(sampleTree(), []string{"t1-group"}, nil)
	if !g.CanReadTeam("t1") || g.CanReadTeam("t2") || g.CanReadTeam("t3") {
		t.Fatalf("teams = %#v", g.Teams)
	}
	if g.CanAggregateProduct("p1") || g.CanAggregateOrg("rhai") {
		t.Fatal("team grant walked up to aggregate access")
	}
	if !g.CanReadProduct("p1") || !g.CanReadOrg("rhai") {
		t.Fatal("parent names should stay visible")
	}
	if g.CanReadOrg("central") || g.CanReadProduct("p2") {
		t.Fatal("team grant leaked the other organization")
	}
}

func TestResolveWidestGrantWins(t *testing.T) {
	g := Resolve(sampleTree(), []string{"t1-group", "P1-GROUP"}, nil)
	if !g.CanAggregateProduct("p1") {
		t.Fatal("product grant should be aggregate")
	}
	if !g.CanReadTeam("t1") || !g.CanReadTeam("t2") {
		t.Fatal("product grant should cover every team in the product")
	}
	if g.CanAggregateOrg("rhai") || g.CanChangeProductGroup("p1") {
		t.Fatal("product grant is not an organization grant")
	}
}

func TestResolveOrgGrant(t *testing.T) {
	g := Resolve(sampleTree(), []string{"RHAI-ORG"}, nil)
	if !g.CanAggregateOrg("rhai") || !g.CanAggregateProduct("p1") || !g.CanChangeProductGroup("p1") {
		t.Fatal("org grant should cover the organization and its products")
	}
	if !g.CanReadTeam("t1") || !g.CanReadTeam("t2") {
		t.Fatal("org grant should cover child teams")
	}
	if g.CanReadOrg("central") || g.CanReadTeam("t3") {
		t.Fatal("org grant leaked the other organization")
	}
	if !g.CanUpdateOrg("rhai") || !g.CanCreateProduct() || g.CanCreateOrg() {
		t.Fatal("org member can update that org and create a product, not a new org")
	}
}

func TestResolveBlankGroupAndUnknownUser(t *testing.T) {
	tree := sampleTree()
	g := Resolve(tree, []string{""}, nil)
	if len(g.Teams) != 0 || len(g.Orgs) != 0 {
		t.Fatalf("blank group matched something: %#v", g)
	}
	g = Resolve(tree, nil, nil)
	if g.CanReadTeam("t3") || g.CanReadTeam("t1") {
		t.Fatal("unknown user or blank team group must see nothing")
	}
}

func TestResolveOperatorSeesAll(t *testing.T) {
	g := Resolve(sampleTree(), []string{"team-only", "Ops"}, []string{"ops"})
	if !g.Operator || !g.CanReadOrg("central") || !g.CanReadTeam("t3") || !g.CanCreateOrg() {
		t.Fatalf("operator grant = %#v", g)
	}
	summary := g.Summary()
	if !summary.Operator || len(summary.Orgs) != 2 || len(summary.Teams) != 3 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestUnionDirectory(t *testing.T) {
	dir := Union(
		MapDirectory{"ada": {"a"}},
		MapDirectory{"ada": {"A", "b"}},
		nil,
	)
	groups := dir.Groups("Ada")
	if len(groups) != 2 || groups[0] != "a" || groups[1] != "b" {
		t.Fatalf("groups = %v", groups)
	}
}
