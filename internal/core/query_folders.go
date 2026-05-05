package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	ErrQueryFolderNotFound     = fmt.Errorf("query folder not found")
	ErrQueryFolderNameRequired = fmt.Errorf("folder name is required")
	ErrQueryFolderColorInvalid = fmt.Errorf("invalid folder color")
)

var allowedQueryFolderColors = map[string]struct{}{
	"gray":   {},
	"red":    {},
	"orange": {},
	"amber":  {},
	"yellow": {},
	"green":  {},
	"teal":   {},
	"cyan":   {},
	"blue":   {},
	"indigo": {},
	"violet": {},
	"pink":   {},
}

// IsValidQueryFolderColor validates the fixed product color palette.
func IsValidQueryFolderColor(color string) bool {
	_, ok := allowedQueryFolderColors[strings.ToLower(strings.TrimSpace(color))]
	return ok
}

// CreateQueryFolder creates a team-level saved-query folder.
func CreateQueryFolder(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, name, description, color string, createdBy models.UserID) (*models.QueryFolder, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	color = strings.ToLower(strings.TrimSpace(color))

	if name == "" {
		return nil, ErrQueryFolderNameRequired
	}
	if !IsValidQueryFolderColor(color) {
		return nil, ErrQueryFolderColorInvalid
	}

	folder := &models.QueryFolder{
		TeamID:      teamID,
		Name:        name,
		Description: description,
		Color:       color,
		CreatedBy:   &createdBy,
	}
	if err := db.CreateQueryFolder(ctx, folder); err != nil {
		log.Error("failed to create query folder", "error", err, "team_id", teamID, "name", name)
		return nil, fmt.Errorf("error creating query folder: %w", err)
	}

	return folder, nil
}

// UpdateQueryFolder updates an existing team-level query folder.
func UpdateQueryFolder(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, folderID int, name, description, color string) (*models.QueryFolder, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	color = strings.ToLower(strings.TrimSpace(color))

	if name == "" {
		return nil, ErrQueryFolderNameRequired
	}
	if !IsValidQueryFolderColor(color) {
		return nil, ErrQueryFolderColorInvalid
	}

	if err := db.UpdateQueryFolder(ctx, teamID, folderID, name, description, color); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQueryFolderNotFound
		}
		log.Error("failed to update query folder", "error", err, "team_id", teamID, "folder_id", folderID)
		return nil, fmt.Errorf("error updating query folder: %w", err)
	}

	folder, err := db.GetQueryFolder(ctx, teamID, folderID)
	if err != nil {
		return nil, fmt.Errorf("query folder updated but failed to fetch latest state: %w", err)
	}
	return folder, nil
}
