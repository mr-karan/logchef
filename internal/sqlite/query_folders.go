package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// ListQueryFolders retrieves all query folders for a team with query counts.
func (db *DB) ListQueryFolders(ctx context.Context, teamID models.TeamID) ([]*models.QueryFolder, error) {
	rows, err := db.readQueries.ListQueryFolders(ctx, int64(teamID))
	if err != nil {
		return nil, fmt.Errorf("error listing query folders: %w", err)
	}

	var folders []*models.QueryFolder
	for _, row := range rows {
		folders = append(folders, queryFolderFromListRow(row))
	}
	return folders, nil
}

// CreateQueryFolder creates a team-level query folder.
func (db *DB) CreateQueryFolder(ctx context.Context, folder *models.QueryFolder) error {
	row, err := db.writeQueries.CreateQueryFolder(ctx, sqlc.CreateQueryFolderParams{
		TeamID:      int64(folder.TeamID),
		Name:        folder.Name,
		Description: nullString(folder.Description),
		Color:       folder.Color,
		SortOrder:   int64(folder.SortOrder),
		CreatedBy:   nullUserID(folder.CreatedBy),
	})
	if err != nil {
		return fmt.Errorf("error creating query folder: %w", err)
	}
	folder.ID = int(row.ID)
	folder.CreatedAt = row.CreatedAt
	folder.UpdatedAt = row.UpdatedAt
	return nil
}

// GetQueryFolder retrieves one folder scoped to a team.
func (db *DB) GetQueryFolder(ctx context.Context, teamID models.TeamID, folderID int) (*models.QueryFolder, error) {
	row, err := db.readQueries.GetQueryFolder(ctx, sqlc.GetQueryFolderParams{
		TeamID: int64(teamID),
		ID:     int64(folderID),
	})
	if err != nil {
		return nil, handleNotFoundError(err, fmt.Sprintf("getting query folder id %d", folderID))
	}
	return queryFolderFromGetRow(row), nil
}

// UpdateQueryFolder updates a folder's editable fields.
func (db *DB) UpdateQueryFolder(ctx context.Context, teamID models.TeamID, folderID int, name, description, color string) error {
	_, err := db.writeQueries.UpdateQueryFolder(ctx, sqlc.UpdateQueryFolderParams{
		Name:        name,
		Description: nullString(description),
		Color:       color,
		TeamID:      int64(teamID),
		ID:          int64(folderID),
	})
	if err != nil {
		return fmt.Errorf("error updating query folder: %w", err)
	}
	return nil
}

// DeleteQueryFolder deletes the folder and its memberships. Saved queries are preserved.
func (db *DB) DeleteQueryFolder(ctx context.Context, teamID models.TeamID, folderID int) error {
	_, err := db.writeQueries.DeleteQueryFolder(ctx, sqlc.DeleteQueryFolderParams{
		TeamID: int64(teamID),
		ID:     int64(folderID),
	})
	if err != nil {
		return fmt.Errorf("error deleting query folder: %w", err)
	}
	return nil
}

// ListQueriesByFolder retrieves all saved queries for a folder.
func (db *DB) ListQueriesByFolder(ctx context.Context, teamID models.TeamID, folderID int) ([]*models.SavedTeamQuery, error) {
	if err := db.ensureFolderBelongsToTeam(ctx, db.readQueries, teamID, folderID); err != nil {
		return nil, err
	}

	rows, err := db.readQueries.ListQueriesByFolder(ctx, sqlc.ListQueriesByFolderParams{
		TeamID:   int64(teamID),
		FolderID: int64(folderID),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing folder queries: %w", err)
	}

	var queries []*models.SavedTeamQuery
	for _, row := range rows {
		queries = append(queries, savedTeamQueryFromSQLC(row))
	}
	if err := db.AttachFoldersToQueries(ctx, queries); err != nil {
		return nil, err
	}
	return queries, nil
}

// SetQueryFolders replaces a saved query's folder memberships.
func (db *DB) SetQueryFolders(ctx context.Context, teamID models.TeamID, queryID int, folderIDs []int, addedBy *models.UserID) error {
	tx, err := db.BeginWriteTx(ctx)
	if err != nil {
		return fmt.Errorf("error starting query folder transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := db.WriteQueriesWithTx(tx)

	if err := db.ensureQueryBelongsToTeam(ctx, qtx, teamID, queryID); err != nil {
		return err
	}
	for _, folderID := range uniqueInts(folderIDs) {
		if err := db.ensureFolderBelongsToTeam(ctx, qtx, teamID, folderID); err != nil {
			return err
		}
	}

	if err := qtx.DeleteQueryFolderItemsByQuery(ctx, int64(queryID)); err != nil {
		return fmt.Errorf("error clearing query folder memberships: %w", err)
	}

	for _, folderID := range uniqueInts(folderIDs) {
		if err := qtx.CreateQueryFolderItem(ctx, sqlc.CreateQueryFolderItemParams{
			FolderID: int64(folderID),
			QueryID:  int64(queryID),
			AddedBy:  nullUserID(addedBy),
		}); err != nil {
			return fmt.Errorf("error adding query folder membership: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error committing query folder memberships: %w", err)
	}
	return nil
}

// AddQueriesToFolder adds many existing saved queries to a folder.
func (db *DB) AddQueriesToFolder(ctx context.Context, teamID models.TeamID, folderID int, queryIDs []int, addedBy *models.UserID) error {
	tx, err := db.BeginWriteTx(ctx)
	if err != nil {
		return fmt.Errorf("error starting add-to-folder transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := db.WriteQueriesWithTx(tx)

	if err := db.ensureFolderBelongsToTeam(ctx, qtx, teamID, folderID); err != nil {
		return err
	}

	for _, queryID := range uniqueInts(queryIDs) {
		if err := db.ensureQueryBelongsToTeam(ctx, qtx, teamID, queryID); err != nil {
			return err
		}
		if err := qtx.CreateQueryFolderItemIgnore(ctx, sqlc.CreateQueryFolderItemIgnoreParams{
			FolderID: int64(folderID),
			QueryID:  int64(queryID),
			AddedBy:  nullUserID(addedBy),
		}); err != nil {
			return fmt.Errorf("error adding query to folder: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error committing add-to-folder transaction: %w", err)
	}
	return nil
}

// RemoveQueryFromFolder removes one saved query from a folder.
func (db *DB) RemoveQueryFromFolder(ctx context.Context, teamID models.TeamID, folderID, queryID int) error {
	if err := db.ensureFolderBelongsToTeam(ctx, db.readQueries, teamID, folderID); err != nil {
		return err
	}
	if err := db.ensureQueryBelongsToTeam(ctx, db.readQueries, teamID, queryID); err != nil {
		return err
	}

	if err := db.writeQueries.RemoveQueryFromFolder(ctx, sqlc.RemoveQueryFromFolderParams{
		FolderID: int64(folderID),
		QueryID:  int64(queryID),
	}); err != nil {
		return fmt.Errorf("error removing query from folder: %w", err)
	}
	return nil
}

// AttachFoldersToQueries populates query folder metadata for a list of saved queries.
func (db *DB) AttachFoldersToQueries(ctx context.Context, queries []*models.SavedTeamQuery) error {
	if len(queries) == 0 {
		return nil
	}

	queryIDs := make([]int, 0, len(queries))
	queryByID := make(map[int]*models.SavedTeamQuery, len(queries))
	for _, query := range queries {
		queryIDs = append(queryIDs, query.ID)
		queryByID[query.ID] = query
		query.Folders = []models.QueryFolderSummary{}
	}

	sqlcQueryIDs := make([]int64, 0, len(queryIDs))
	for _, queryID := range queryIDs {
		sqlcQueryIDs = append(sqlcQueryIDs, int64(queryID))
	}

	rows, err := db.readQueries.ListQueryFoldersByQueryIDs(ctx, sqlcQueryIDs)
	if err != nil {
		return fmt.Errorf("error listing query folder metadata: %w", err)
	}

	for _, row := range rows {
		if query, ok := queryByID[int(row.QueryID)]; ok {
			query.Folders = append(query.Folders, models.QueryFolderSummary{
				ID:    int(row.ID),
				Name:  row.Name,
				Color: row.Color,
			})
		}
	}
	return nil
}

func (db *DB) ensureFolderBelongsToTeam(ctx context.Context, q sqlc.Querier, teamID models.TeamID, folderID int) error {
	exists, err := q.CountQueryFolderForTeam(ctx, sqlc.CountQueryFolderForTeamParams{
		TeamID: int64(teamID),
		ID:     int64(folderID),
	})
	if err != nil {
		return fmt.Errorf("error checking query folder ownership: %w", err)
	}
	if exists == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) ensureQueryBelongsToTeam(ctx context.Context, q sqlc.Querier, teamID models.TeamID, queryID int) error {
	exists, err := q.CountTeamQueryForTeam(ctx, sqlc.CountTeamQueryForTeamParams{
		TeamID: int64(teamID),
		ID:     int64(queryID),
	})
	if err != nil {
		return fmt.Errorf("error checking saved query ownership: %w", err)
	}
	if exists == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func nullUserID(userID *models.UserID) sql.NullInt64 {
	if userID == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*userID), Valid: true}
}

func uniqueInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	unique := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func queryFolderFromListRow(row sqlc.ListQueryFoldersRow) *models.QueryFolder {
	return &models.QueryFolder{
		ID:          int(row.ID),
		TeamID:      models.TeamID(row.TeamID),
		Name:        row.Name,
		Description: row.Description.String,
		Color:       row.Color,
		SortOrder:   int(row.SortOrder),
		CreatedBy:   userIDPtr(row.CreatedBy),
		QueryCount:  int(row.QueryCount),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func queryFolderFromGetRow(row sqlc.GetQueryFolderRow) *models.QueryFolder {
	return &models.QueryFolder{
		ID:          int(row.ID),
		TeamID:      models.TeamID(row.TeamID),
		Name:        row.Name,
		Description: row.Description.String,
		Color:       row.Color,
		SortOrder:   int(row.SortOrder),
		CreatedBy:   userIDPtr(row.CreatedBy),
		QueryCount:  int(row.QueryCount),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func savedTeamQueryFromSQLC(row sqlc.TeamQuery) *models.SavedTeamQuery {
	return &models.SavedTeamQuery{
		ID:           int(row.ID),
		TeamID:       models.TeamID(row.TeamID),
		SourceID:     models.SourceID(row.SourceID),
		Name:         row.Name,
		Description:  row.Description.String,
		QueryType:    models.SavedQueryType(row.QueryType),
		QueryContent: row.QueryContent,
		IsBookmarked: row.IsBookmarked,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func userIDPtr(value sql.NullInt64) *models.UserID {
	if !value.Valid {
		return nil
	}
	userID := models.UserID(value.Int64)
	return &userID
}
