package server

import (
	"context"
	"reflect"
	"testing"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

func TestFreshnessTeamIDs(t *testing.T) {
	ctx := context.Background()
	store := database.NewMemStore()
	if err := store.CreateOrg(ctx, database.OrgRow{ID: "org-a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProduct(ctx, database.ProductRow{ID: "product-a"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"team-a", "team-b"} {
		if err := store.CreateTeam(ctx, database.TeamRow{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ReplaceOrgProductLinks(ctx, "org-a", []database.RelEntry{{ID: "product-a"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceProductTeamLinks(ctx, "product-a", []database.RelEntry{{ID: "team-a"}, {ID: "team-b"}}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		ownerType string
		ownerID   string
		want      []string
	}{
		{name: "team", ownerType: "team", ownerID: "team-a", want: []string{"team-a"}},
		{name: "product", ownerType: "product", ownerID: "product-a", want: []string{"team-a", "team-b"}},
		{name: "org", ownerType: "org", ownerID: "org-a", want: []string{"team-a", "team-b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := freshnessTeamIDs(ctx, store, tt.ownerType, tt.ownerID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("team IDs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFreshnessTeamIDsRejectsUnknownOwner(t *testing.T) {
	_, err := freshnessTeamIDs(context.Background(), database.NewMemStore(), "unknown", "id")
	if err == nil {
		t.Fatal("expected unsupported owner type error")
	}
}
