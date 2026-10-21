package provisioning

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func loadProvisioningUsers(ctx context.Context, tx store.StoreOps, cfg *config.ProvisioningConfig) (map[string]*models.User, error) {
	usersByEmail := make(map[string]*models.User)
	if !cfg.ManageTeams {
		return usersByEmail, nil
	}
	emails := make([]string, 0)
	seen := make(map[string]bool)
	teamNames := make([]string, 0, len(cfg.Teams))
	for _, team := range cfg.Teams {
		teamNames = append(teamNames, team.Name)
		for _, member := range team.Members {
			if !seen[member.Email] {
				emails = append(emails, member.Email)
				seen[member.Email] = true
			}
		}
	}
	users, err := tx.LockProvisioningUsers(ctx, emails, teamNames, cfg.Prune)
	if err != nil {
		return nil, fmt.Errorf("failed to load provisioning users: %w", err)
	}
	for _, user := range users {
		usersByEmail[user.Email] = user
	}
	return usersByEmail, nil
}
