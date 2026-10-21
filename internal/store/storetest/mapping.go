package storetest

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func testTeamMapping(t *testing.T, ctx context.Context, s store.Store) {
	user := mkUser(t, ctx, s, "team-mapping@test.dev")
	source := mkSource(t, ctx, s, "team_mapping")
	for _, managed := range []bool{false, true} {
		t.Run(fmt.Sprint(managed), func(t *testing.T) {
			team := &models.Team{Name: fmt.Sprintf("mapping-%t", managed), Description: "team description"}
			if err := s.CreateTeam(ctx, team); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := s.DeleteTeam(ctx, team.ID); err != nil {
					t.Error(err)
				}
			})
			if err := s.SetTeamManaged(ctx, team.ID, managed); err != nil {
				t.Fatal(err)
			}
			team.Managed = managed
			persisted, err := s.GetTeam(ctx, team.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.UpdatedAt.IsZero() || persisted.UpdatedAt.Before(team.UpdatedAt) {
				t.Fatalf("invalid UpdatedAt: %v", persisted.UpdatedAt)
			}
			team.UpdatedAt = persisted.UpdatedAt
			if err := s.AddTeamMember(ctx, team.ID, user.ID, models.TeamRoleAdmin); err != nil {
				t.Fatal(err)
			}
			if err := s.AddTeamSource(ctx, team.ID, source.ID); err != nil {
				t.Fatal(err)
			}
			assertTeamQueries(t, ctx, s, team, user.ID, source.ID)
			assertTeamMembership(t, ctx, s, team, user)
		})
	}
}

func assertTeamQueries(t *testing.T, ctx context.Context, s store.Store, team *models.Team, userID models.UserID, sourceID models.SourceID) {
	t.Helper()
	queries := []struct {
		name        string
		get         func() ([]*models.Team, error)
		memberCount int
	}{
		{"GetTeam", func() ([]*models.Team, error) {
			v, err := s.GetTeam(ctx, team.ID)
			return []*models.Team{v}, err
		}, 0},
		{"GetTeamByName", func() ([]*models.Team, error) {
			v, err := s.GetTeamByName(ctx, team.Name)
			return []*models.Team{v}, err
		}, 0},
		{"ListTeams", func() ([]*models.Team, error) { return s.ListTeams(ctx) }, 1},
		{"ListUserTeams", func() ([]*models.Team, error) { return s.ListUserTeams(ctx, userID) }, 0},
		{"ListSourceTeams", func() ([]*models.Team, error) { return s.ListSourceTeams(ctx, sourceID) }, 0},
		{"ListManagedTeams", func() ([]*models.Team, error) { return s.ListManagedTeams(ctx) }, 0},
	}

	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			teams, err := query.get()
			if err != nil {
				t.Fatal(err)
			}
			want := *team
			want.MemberCount = query.memberCount
			found := false
			for _, got := range teams {
				if got.ID == team.ID {
					found = true
					if !reflect.DeepEqual(got, &want) {
						t.Errorf("got %+v, want %+v", got, &want)
					}
				}
			}
			wantFound := query.name != "ListManagedTeams" || team.Managed
			if found != wantFound {
				t.Errorf("team present = %t, want %t", found, wantFound)
			}
		})
	}
}

func assertTeamMembership(t *testing.T, ctx context.Context, s store.Store, team *models.Team, user *models.User) {
	t.Helper()
	details, err := s.ListTeamsForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	wantDetails := models.UserTeamDetails{
		ID:          team.ID,
		Name:        team.Name,
		Description: team.Description,
		CreatedAt:   team.CreatedAt,
		UpdatedAt:   team.UpdatedAt,
		MemberCount: 1,
		Role:        models.TeamRoleAdmin,
	}
	for _, got := range details {
		if got.ID == team.ID {
			found = true
			if !reflect.DeepEqual(*got, wantDetails) {
				t.Errorf("ListTeamsForUser = %+v, want %+v", got, wantDetails)
			}
		}
	}
	if !found {
		t.Fatal("ListTeamsForUser omitted team")
	}
	member, err := s.GetTeamMember(ctx, team.ID, user.ID)
	if err != nil || member == nil {
		t.Fatalf("GetTeamMember = %+v, %v", member, err)
	}
	if member.TeamID != team.ID || member.UserID != user.ID || member.Role != models.TeamRoleAdmin || member.CreatedAt.IsZero() {
		t.Fatalf("GetTeamMember fields did not round-trip: %+v", member)
	}
	members, err := s.ListTeamMembers(ctx, team.ID)
	if err != nil || len(members) != 1 || !reflect.DeepEqual(members[0], member) {
		t.Fatalf("ListTeamMembers = %+v, %v, want %+v", members, err, member)
	}
	member.Email, member.FullName, member.AccountType = user.Email, user.FullName, models.UserAccountTypeHuman
	members, err = s.ListTeamMembersWithDetails(ctx, team.ID)
	if err != nil || len(members) != 1 || !reflect.DeepEqual(members[0], member) {
		t.Fatalf("ListTeamMembersWithDetails = %+v, %v, want %+v", members, err, member)
	}
}

func resultError[T any](_ T, err error) error { return err }

func testGetNotFound(t *testing.T, ctx context.Context, s store.Store) {
	tests := []struct {
		name string
		err  error
	}{
		{"GetUser", resultError(s.GetUser(ctx, -1))},
		{"GetUserByEmail", resultError(s.GetUserByEmail(ctx, "missing@test.dev"))},
		{"GetSession", resultError(s.GetSession(ctx, "missing"))},
		{"GetUserPreferencesJSON", resultError(s.GetUserPreferencesJSON(ctx, -1))},
		{"GetSource", resultError(s.GetSource(ctx, -1))},
		{"GetSourceByIdentityKey", resultError(s.GetSourceByIdentityKey(ctx, "missing"))},
		{"GetSourceByNameForProvisioning", resultError(s.GetSourceByNameForProvisioning(ctx, "missing"))},
		{"GetTeam", resultError(s.GetTeam(ctx, -1))},
		{"GetTeamByName", resultError(s.GetTeamByName(ctx, "missing"))},
		{"GetSavedQuery", resultError(s.GetSavedQuery(ctx, -1))},
		{"GetCollection", resultError(s.GetCollection(ctx, -1))},
		{"GetPersonalCollection", resultError(s.GetPersonalCollection(ctx, -1))},
		{"GetCollectionMember", resultError(s.GetCollectionMember(ctx, -1, -1))},
		{"GetDashboard", resultError(s.GetDashboard(ctx, -1))},
		{"GetAlert", resultError(s.GetAlert(ctx, -1))},
		{"GetLatestUnresolvedAlertHistory", resultError(s.GetLatestUnresolvedAlertHistory(ctx, -1))},
		{"GetAlertHistoryEntry", resultError(s.GetAlertHistoryEntry(ctx, -1, -1))},
		{"GetExportJob", resultError(s.GetExportJob(ctx, "missing"))},
		{"GetQueryShare", resultError(s.GetQueryShare(ctx, "missing"))},
		{"GetUserTeamForSource", resultError(s.GetUserTeamForSource(ctx, -1, -1))},
		{"GetSetting", resultError(s.GetSetting(ctx, "missing"))},
		{"GetAPIToken", resultError(s.GetAPIToken(ctx, -1))},
		{"GetAPITokenByHash", resultError(s.GetAPITokenByHash(ctx, "missing"))},
		{"GetOAuthAuthRequest", resultError(s.GetOAuthAuthRequest(ctx, "missing"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !models.IsNotFound(test.err) {
				t.Errorf("error = %v, want models.IsNotFound", test.err)
			}
		})
	}
	if member, err := s.GetTeamMember(ctx, -1, -1); member != nil || err != nil {
		t.Errorf("GetTeamMember = %+v, %v, want nil, nil", member, err)
	}
}

func testCollectionMapping(t *testing.T, ctx context.Context, s store.Store) {
	user := mkUser(t, ctx, s, "collection-mapping@test.dev")
	for _, personal := range []bool{true, false} {
		t.Run(fmt.Sprintf("personal-%t", personal), func(t *testing.T) {
			name := fmt.Sprintf("mapping-%t", personal)
			collection, err := s.CreateCollection(ctx, name, "collection description", personal, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := s.DeleteCollection(ctx, collection.ID); err != nil {
					t.Error(err)
				}
			})
			wantCreated := &models.Collection{
				ID:          collection.ID,
				Name:        name,
				Description: "collection description",
				IsPersonal:  personal,
				CreatedBy:   &user.ID,
				CreatedAt:   collection.CreatedAt,
				UpdatedAt:   collection.UpdatedAt,
			}
			if collection.CreatedAt.IsZero() || collection.UpdatedAt.IsZero() || !reflect.DeepEqual(collection, wantCreated) {
				t.Fatalf("CreateCollection = %+v, want %+v", collection, wantCreated)
			}
			got, err := s.GetCollection(ctx, collection.ID)
			if err != nil || !reflect.DeepEqual(got, collection) {
				t.Fatalf("GetCollection = %+v, %v, want %+v", got, err, collection)
			}
			if personal {
				got, err := s.GetPersonalCollection(ctx, user.ID)
				if err != nil || !reflect.DeepEqual(got, collection) {
					t.Fatalf("GetPersonalCollection = %+v, %v, want %+v", got, err, collection)
				}
			}
			assertCollectionMembership(t, ctx, s, collection, user)
			collections, err := s.ListCollectionsForUser(ctx, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := *collection
			want.CallerRole = models.CollectionRoleOwner
			want.MemberCount, want.TeamCount, want.ItemCount = 1, 0, 0
			matches := 0
			for _, got := range collections {
				if got.ID == collection.ID {
					matches++
					if !reflect.DeepEqual(got, &want) {
						t.Errorf("ListCollectionsForUser = %+v, want %+v", got, &want)
					}
				}
			}
			if matches != 1 {
				t.Errorf("ListCollectionsForUser returned collection %d times, want 1", matches)
			}
		})
	}
}

func assertCollectionMembership(t *testing.T, ctx context.Context, s store.Store, collection *models.Collection, user *models.User) {
	t.Helper()
	if err := s.AddCollectionMember(ctx, collection.ID, user.ID, models.CollectionRoleOwner, &user.ID); err != nil {
		t.Fatal(err)
	}
	member, err := s.GetCollectionMember(ctx, collection.ID, user.ID)
	if err != nil || member == nil {
		t.Fatalf("GetCollectionMember = %+v, %v", member, err)
	}
	wantMember := &models.CollectionMember{
		CollectionID: collection.ID,
		UserID:       user.ID,
		Role:         models.CollectionRoleOwner,
		AddedBy:      &user.ID,
		CreatedAt:    member.CreatedAt,
	}
	if member.CreatedAt.IsZero() || !reflect.DeepEqual(member, wantMember) {
		t.Fatalf("GetCollectionMember = %+v, want %+v", member, wantMember)
	}
}
