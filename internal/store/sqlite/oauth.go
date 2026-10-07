package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// OAuth authorization server persistence. Times are stored as Unix
// milliseconds so SQL can compare them; see 000033_oauth.up.sql.

var errOAuthRootOnly = errors.New("oauth replay handling must not run inside WithTx")

func (db *DB) CreateOAuthAuthRequest(ctx context.Context, req *models.OAuthAuthRequest) error {
	scopes, err := marshalTokenScopes(req.Scopes)
	if err != nil {
		return err
	}
	err = db.writeQueries.CreateOAuthAuthRequest(ctx, sqlc.CreateOAuthAuthRequestParams{
		ID:            string(req.ID),
		ClientID:      string(req.ClientID),
		RedirectUri:   req.RedirectURI,
		Resource:      req.Resource,
		Scopes:        scopes,
		OfflineAccess: boolToInt(req.OfflineAccess),
		CodeChallenge: req.CodeChallenge,
		State:         req.State,
		ExpiresAt:     req.ExpiresAt.UnixMilli(),
		CreatedAt:     req.CreatedAt.UnixMilli(),
	})
	if isUniqueConstraintSQLiteError(err, "", "") {
		return models.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("creating oauth auth request: %w", err)
	}
	return nil
}

func (db *DB) GetOAuthAuthRequest(ctx context.Context, id models.OAuthAuthRequestID) (*models.OAuthAuthRequest, error) {
	row, err := db.readQueries.GetOAuthAuthRequest(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting oauth auth request: %w", err)
	}
	return authRequestFromRow(row), nil
}

func (db *DB) ApproveOAuthAuthRequest(ctx context.Context, id models.OAuthAuthRequestID, userID models.UserID, now time.Time) (*models.OAuthGrant, error) {
	var grant *models.OAuthGrant
	err := db.inWriteTx(ctx, func(q *sqlc.Queries) error {
		req, err := q.ClaimOAuthAuthRequestDecision(ctx, sqlc.ClaimOAuthAuthRequestDecisionParams{
			Now:    nullMillis(now),
			UserID: sql.NullInt64{Int64: int64(userID), Valid: true},
			ID:     string(id),
		})
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("claiming oauth auth request: %w", err)
		}
		row, err := q.CreateOAuthGrant(ctx, sqlc.CreateOAuthGrantParams{
			UserID:        int64(userID),
			ClientID:      req.ClientID,
			Resource:      req.Resource,
			Scopes:        req.Scopes,
			OfflineAccess: req.OfflineAccess,
			CreatedAt:     now.UnixMilli(),
		})
		if err != nil {
			return fmt.Errorf("creating oauth grant: %w", err)
		}
		if err := q.SetOAuthAuthRequestGrant(ctx, sqlc.SetOAuthAuthRequestGrantParams{
			GrantID: sql.NullInt64{Int64: row.ID, Valid: true},
			ID:      req.ID,
		}); err != nil {
			return fmt.Errorf("linking oauth grant: %w", err)
		}
		grant = grantFromRow(row)
		return nil
	})
	return grant, err
}

func (db *DB) DenyOAuthAuthRequest(ctx context.Context, id models.OAuthAuthRequestID, userID models.UserID, now time.Time) error {
	_, err := db.writeQueries.ClaimOAuthAuthRequestDecision(ctx, sqlc.ClaimOAuthAuthRequestDecisionParams{
		Now:    nullMillis(now),
		UserID: sql.NullInt64{Int64: int64(userID), Valid: true},
		Denied: 1,
		ID:     string(id),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("denying oauth auth request: %w", err)
	}
	return nil
}

func (db *DB) SaveOAuthAuthCode(ctx context.Context, id models.OAuthAuthRequestID, codeHash models.OAuthSecretHash, expiresAt time.Time) error {
	n, err := db.writeQueries.SaveOAuthAuthCode(ctx, sqlc.SaveOAuthAuthCodeParams{
		CodeHash:      sql.NullString{String: codeHash.Hex(), Valid: true},
		CodeExpiresAt: nullMillis(expiresAt),
		ID:            string(id),
	})
	if err != nil {
		return fmt.Errorf("saving oauth auth code: %w", err)
	}
	if n == 0 {
		return models.ErrNotFound
	}
	return nil
}

func (db *DB) ConsumeAuthCode(ctx context.Context, codeHash models.OAuthSecretHash, now time.Time) (*models.OAuthAuthRequest, error) {
	if db.inTx {
		return nil, errOAuthRootOnly
	}
	hash := sql.NullString{String: codeHash.Hex(), Valid: true}
	row, err := db.writeQueries.ConsumeOAuthAuthCode(ctx, sqlc.ConsumeOAuthAuthCodeParams{Now: nullMillis(now), CodeHash: hash})
	if err == nil {
		return authRequestFromRow(row), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("consuming oauth auth code: %w", err)
	}

	state, err := db.writeQueries.GetOAuthAuthCodeState(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrOAuthInvalidGrant
	}
	if err != nil {
		return nil, fmt.Errorf("reading oauth auth code state: %w", err)
	}
	if !state.CodeConsumedAt.Valid || !state.GrantID.Valid {
		return nil, models.ErrOAuthInvalidGrant
	}
	// The consumption above already committed; this revocation commits on
	// its own, so the caller's failure path cannot roll it back.
	if _, err := db.writeQueries.RevokeOAuthGrant(ctx, sqlc.RevokeOAuthGrantParams{
		Now:    nullMillis(now),
		Reason: sql.NullString{String: string(models.OAuthRevokeCodeReplay), Valid: true},
		ID:     state.GrantID.Int64,
	}); err != nil {
		return nil, fmt.Errorf("revoking oauth grant after code replay: %w", err)
	}
	return nil, models.ErrOAuthCodeReplay
}

func (db *DB) CreateOAuthDeviceAuthorization(ctx context.Context, d models.NewOAuthDeviceAuthorization) error {
	scopes, err := marshalTokenScopes(d.Scopes)
	if err != nil {
		return err
	}
	err = db.writeQueries.CreateOAuthDeviceAuthorization(ctx, sqlc.CreateOAuthDeviceAuthorizationParams{
		DeviceCodeHash: d.DeviceCodeHash.Hex(),
		UserCodeHash:   d.UserCodeHash.Hex(),
		ClientID:       string(d.ClientID),
		Resource:       d.Resource,
		Scopes:         scopes,
		OfflineAccess:  boolToInt(d.OfflineAccess),
		IntervalSecs:   int64(d.Interval / time.Second),
		ExpiresAt:      d.ExpiresAt.UnixMilli(),
		CreatedAt:      d.CreatedAt.UnixMilli(),
	})
	if isUniqueConstraintSQLiteError(err, "", "") {
		return models.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("creating oauth device authorization: %w", err)
	}
	return nil
}

func (db *DB) GetPendingOAuthDeviceAuthorization(ctx context.Context, userCodeHash models.OAuthSecretHash, now time.Time) (*models.OAuthDeviceAuthorization, error) {
	row, err := db.readQueries.GetPendingOAuthDeviceAuthorization(ctx, sqlc.GetPendingOAuthDeviceAuthorizationParams{
		UserCodeHash: userCodeHash.Hex(),
		Now:          now.UnixMilli(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting oauth device authorization: %w", err)
	}
	d := deviceFromRow(row)
	return &d, nil
}

func (db *DB) ApproveOAuthDeviceAuthorization(ctx context.Context, userCodeHash models.OAuthSecretHash, userID models.UserID, now time.Time) (*models.OAuthGrant, error) {
	var grant *models.OAuthGrant
	err := db.inWriteTx(ctx, func(q *sqlc.Queries) error {
		d, err := q.ApproveOAuthDeviceAuthorization(ctx, sqlc.ApproveOAuthDeviceAuthorizationParams{
			Now:          nullMillis(now),
			UserID:       sql.NullInt64{Int64: int64(userID), Valid: true},
			UserCodeHash: userCodeHash.Hex(),
		})
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("approving oauth device authorization: %w", err)
		}
		row, err := q.CreateOAuthGrant(ctx, sqlc.CreateOAuthGrantParams{
			UserID:        int64(userID),
			ClientID:      d.ClientID,
			Resource:      d.Resource,
			Scopes:        d.Scopes,
			OfflineAccess: d.OfflineAccess,
			CreatedAt:     now.UnixMilli(),
		})
		if err != nil {
			return fmt.Errorf("creating oauth grant: %w", err)
		}
		if err := q.SetOAuthDeviceAuthorizationGrant(ctx, sqlc.SetOAuthDeviceAuthorizationGrantParams{
			GrantID:        sql.NullInt64{Int64: row.ID, Valid: true},
			DeviceCodeHash: d.DeviceCodeHash,
		}); err != nil {
			return fmt.Errorf("linking oauth grant: %w", err)
		}
		grant = grantFromRow(row)
		return nil
	})
	return grant, err
}

func (db *DB) DenyOAuthDeviceAuthorization(ctx context.Context, userCodeHash models.OAuthSecretHash, userID models.UserID, now time.Time) error {
	n, err := db.writeQueries.DenyOAuthDeviceAuthorization(ctx, sqlc.DenyOAuthDeviceAuthorizationParams{
		Now:          nullMillis(now),
		UserID:       sql.NullInt64{Int64: int64(userID), Valid: true},
		UserCodeHash: userCodeHash.Hex(),
	})
	if err != nil {
		return fmt.Errorf("denying oauth device authorization: %w", err)
	}
	if n == 0 {
		return models.ErrNotFound
	}
	return nil
}

func (db *DB) RecordDevicePoll(ctx context.Context, deviceCodeHash models.OAuthSecretHash, clientID models.OAuthClientID, now time.Time) (models.OAuthDevicePoll, error) {
	row, err := db.writeQueries.RecordOAuthDevicePoll(ctx, sqlc.RecordOAuthDevicePollParams{
		Now:            now.UnixMilli(),
		DeviceCodeHash: deviceCodeHash.Hex(),
		ClientID:       string(clientID),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return models.OAuthDevicePoll{}, models.ErrNotFound
	}
	if err != nil {
		return models.OAuthDevicePoll{}, fmt.Errorf("recording oauth device poll: %w", err)
	}
	// Column12 is the query's on_time column; sqlc's SQLite generator drops
	// RETURNING aliases.
	if row.Column12 == 0 {
		return models.OAuthDevicePoll{Status: models.DevicePollSlowDown}, nil
	}
	d := deviceFromRow(sqlc.OauthDeviceAuthorization{
		ClientID:      row.ClientID,
		Resource:      row.Resource,
		Scopes:        row.Scopes,
		OfflineAccess: row.OfflineAccess,
		IntervalSecs:  row.IntervalSecs,
		UserID:        row.UserID,
		GrantID:       row.GrantID,
		ApprovedAt:    row.ApprovedAt,
		DeniedAt:      row.DeniedAt,
		ExpiresAt:     row.ExpiresAt,
		CreatedAt:     row.CreatedAt,
	})
	return models.OAuthDevicePoll{Status: devicePollStatus(d, now), Authorization: d}, nil
}

func (db *DB) ConsumeDeviceCode(ctx context.Context, deviceCodeHash models.OAuthSecretHash, clientID models.OAuthClientID, now time.Time) (*models.OAuthGrant, error) {
	grantID, err := db.writeQueries.ConsumeOAuthDeviceCode(ctx, sqlc.ConsumeOAuthDeviceCodeParams{
		Now:            nullMillis(now),
		DeviceCodeHash: deviceCodeHash.Hex(),
		ClientID:       string(clientID),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrOAuthInvalidGrant
	}
	if err != nil {
		return nil, fmt.Errorf("consuming oauth device code: %w", err)
	}
	row, err := db.writeQueries.GetOAuthGrant(ctx, grantID.Int64)
	if err != nil {
		return nil, fmt.Errorf("getting oauth grant: %w", err)
	}
	return grantFromRow(row), nil
}

func (db *DB) IssueOAuthTokens(ctx context.Context, grantID models.OAuthGrantID, issue models.OAuthTokenIssue, now time.Time) error {
	return db.inWriteTx(ctx, func(q *sqlc.Queries) error {
		return issueOAuthTokens(ctx, q, int64(grantID), issue, now)
	})
}

func (db *DB) CheckRefreshToken(ctx context.Context, tokenHash models.OAuthSecretHash, now time.Time) (models.RefreshOutcome, error) {
	if db.inTx {
		return models.RefreshOutcome{}, errOAuthRootOnly
	}
	return classifyRefreshToken(ctx, db.writeQueries, tokenHash, now)
}

func (db *DB) RotateRefreshToken(ctx context.Context, oldHash models.OAuthSecretHash, issue models.OAuthTokenIssue, now time.Time) (models.RefreshOutcome, error) {
	if db.inTx {
		return models.RefreshOutcome{}, errOAuthRootOnly
	}
	if issue.RefreshHash == nil {
		return models.RefreshOutcome{}, errors.New("refresh rotation needs a new refresh token")
	}
	var out models.RefreshOutcome
	// A replay returns nil from the callback so the grant revocation commits.
	err := db.inWriteTx(ctx, func(q *sqlc.Queries) error {
		grantID, err := q.ConsumeOAuthRefreshToken(ctx, sqlc.ConsumeOAuthRefreshTokenParams{
			Now:            nullMillis(now),
			ReplacedByHash: sql.NullString{String: issue.RefreshHash.Hex(), Valid: true},
			TokenHash:      oldHash.Hex(),
		})
		if errors.Is(err, sql.ErrNoRows) {
			out, err = classifyRefreshToken(ctx, q, oldHash, now)
			if out.Kind != models.RefreshReplay {
				out = models.RefreshOutcome{Kind: models.RefreshInvalid}
			}
			return err
		}
		if err != nil {
			return fmt.Errorf("consuming oauth refresh token: %w", err)
		}
		if err := issueOAuthTokens(ctx, q, grantID, issue, now); err != nil {
			return err
		}
		row, err := q.GetOAuthGrant(ctx, grantID)
		if err != nil {
			return fmt.Errorf("getting oauth grant: %w", err)
		}
		out = models.RefreshOutcome{Kind: models.RefreshOK, Grant: *grantFromRow(row)}
		return nil
	})
	if err != nil {
		return models.RefreshOutcome{}, err
	}
	return out, nil
}

func (db *DB) AuthenticateOAuthAccessToken(ctx context.Context, idHash models.OAuthSecretHash, resource string, now time.Time) (*models.OAuthGrant, *models.User, error) {
	row, err := db.readQueries.AuthenticateOAuthAccessToken(ctx, sqlc.AuthenticateOAuthAccessTokenParams{
		IDHash:   idHash.Hex(),
		Now:      now.UnixMilli(),
		Resource: resource,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, models.ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("authenticating oauth access token: %w", err)
	}
	return grantFromRow(row.OauthGrant), mapUserRowToModel(row.User), nil
}

func (db *DB) TouchOAuthGrant(ctx context.Context, id models.OAuthGrantID, now time.Time) error {
	if err := db.writeQueries.TouchOAuthGrant(ctx, sqlc.TouchOAuthGrantParams{
		LastUsedAt: nullMillis(now),
		ID:         int64(id),
	}); err != nil {
		return fmt.Errorf("touching oauth grant: %w", err)
	}
	return nil
}

func (db *DB) RevokeGrant(ctx context.Context, id models.OAuthGrantID, userID models.UserID, now time.Time) error {
	n, err := db.writeQueries.RevokeOAuthGrantForUser(ctx, sqlc.RevokeOAuthGrantForUserParams{
		Now:    nullMillis(now),
		Reason: sql.NullString{String: string(models.OAuthRevokeUser), Valid: true},
		ID:     int64(id),
		UserID: int64(userID),
	})
	if err != nil {
		return fmt.Errorf("revoking oauth grant: %w", err)
	}
	if n == 0 {
		return models.ErrNotFound
	}
	return nil
}

func (db *DB) RevokeGrantByToken(ctx context.Context, tokenHash models.OAuthSecretHash, clientID models.OAuthClientID, now time.Time) error {
	if err := db.writeQueries.RevokeOAuthGrantByToken(ctx, sqlc.RevokeOAuthGrantByTokenParams{
		Now:       nullMillis(now),
		Reason:    sql.NullString{String: string(models.OAuthRevokeTokenEndpoint), Valid: true},
		ClientID:  string(clientID),
		TokenHash: tokenHash.Hex(),
	}); err != nil {
		return fmt.Errorf("revoking oauth grant by token: %w", err)
	}
	return nil
}

func (db *DB) ListGrantsForUser(ctx context.Context, userID models.UserID) ([]*models.OAuthGrant, error) {
	rows, err := db.readQueries.ListOAuthGrantsForUser(ctx, int64(userID))
	if err != nil {
		return nil, fmt.Errorf("listing oauth grants: %w", err)
	}
	grants := make([]*models.OAuthGrant, len(rows))
	for i := range rows {
		grants[i] = grantFromRow(rows[i])
	}
	return grants, nil
}

func (db *DB) DeleteExpiredOAuthRows(ctx context.Context, now time.Time) error {
	ms := now.UnixMilli()
	return db.inWriteTx(ctx, func(q *sqlc.Queries) error {
		if err := q.DeleteExpiredOAuthAuthRequests(ctx, ms); err != nil {
			return fmt.Errorf("deleting expired oauth auth requests: %w", err)
		}
		if err := q.DeleteExpiredOAuthDeviceAuthorizations(ctx, ms); err != nil {
			return fmt.Errorf("deleting expired oauth device authorizations: %w", err)
		}
		if err := q.DeleteExpiredOAuthAccessTokens(ctx, ms); err != nil {
			return fmt.Errorf("deleting expired oauth access tokens: %w", err)
		}
		if err := q.DeleteExpiredOAuthRefreshFamilies(ctx, ms); err != nil {
			return fmt.Errorf("deleting expired oauth refresh tokens: %w", err)
		}
		return nil
	})
}

// inWriteTx runs fn in one write transaction. On a tx-scoped handle it joins
// the caller's transaction instead.
func (db *DB) inWriteTx(ctx context.Context, fn func(q *sqlc.Queries) error) error {
	if db.inTx {
		return fn(db.writeQueries)
	}
	tx, err := db.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(db.writeQueries.WithTx(tx)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			db.log.Error("failed to roll back transaction", "error", rbErr, "cause", err)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func issueOAuthTokens(ctx context.Context, q *sqlc.Queries, grantID int64, issue models.OAuthTokenIssue, now time.Time) error {
	n, err := q.InsertOAuthAccessToken(ctx, sqlc.InsertOAuthAccessTokenParams{
		IDHash:    issue.AccessIDHash.Hex(),
		ExpiresAt: issue.AccessExpiresAt.UnixMilli(),
		CreatedAt: now.UnixMilli(),
		GrantID:   grantID,
	})
	if err != nil {
		return fmt.Errorf("inserting oauth access token: %w", err)
	}
	if n == 0 {
		return models.ErrOAuthInvalidGrant
	}
	if issue.RefreshHash == nil {
		return nil
	}
	if _, err := q.InsertOAuthRefreshToken(ctx, sqlc.InsertOAuthRefreshTokenParams{
		TokenHash: issue.RefreshHash.Hex(),
		ExpiresAt: issue.RefreshExpiresAt.UnixMilli(),
		CreatedAt: now.UnixMilli(),
		GrantID:   grantID,
	}); err != nil {
		return fmt.Errorf("inserting oauth refresh token: %w", err)
	}
	return nil
}

// classifyRefreshToken reads a refresh token's state. A consumed token is a
// replay: the grant is revoked through q, which commits with q's transaction.
func classifyRefreshToken(ctx context.Context, q *sqlc.Queries, tokenHash models.OAuthSecretHash, now time.Time) (models.RefreshOutcome, error) {
	invalid := models.RefreshOutcome{Kind: models.RefreshInvalid}
	row, err := q.GetOAuthRefreshTokenState(ctx, tokenHash.Hex())
	if errors.Is(err, sql.ErrNoRows) {
		return invalid, nil
	}
	if err != nil {
		return models.RefreshOutcome{}, fmt.Errorf("reading oauth refresh token: %w", err)
	}
	if row.ConsumedAt.Valid {
		if _, err := q.RevokeOAuthGrant(ctx, sqlc.RevokeOAuthGrantParams{
			Now:    nullMillis(now),
			Reason: sql.NullString{String: string(models.OAuthRevokeRefreshReplay), Valid: true},
			ID:     row.OauthGrant.ID,
		}); err != nil {
			return models.RefreshOutcome{}, fmt.Errorf("revoking oauth grant after refresh replay: %w", err)
		}
		return models.RefreshOutcome{Kind: models.RefreshReplay}, nil
	}
	if row.ExpiresAt <= now.UnixMilli() || row.OauthGrant.RevokedAt.Valid {
		return invalid, nil
	}
	return models.RefreshOutcome{Kind: models.RefreshOK, Grant: *grantFromRow(row.OauthGrant)}, nil
}

func devicePollStatus(d models.OAuthDeviceAuthorization, now time.Time) models.OAuthDevicePollStatus {
	switch {
	case !now.Before(d.ExpiresAt):
		return models.DevicePollExpired
	case d.DeniedAt != nil:
		return models.DevicePollDenied
	case d.ApprovedAt != nil:
		return models.DevicePollApproved
	default:
		return models.DevicePollPending
	}
}

func grantFromRow(row sqlc.OauthGrant) *models.OAuthGrant {
	return &models.OAuthGrant{
		ID:            models.OAuthGrantID(row.ID),
		UserID:        models.UserID(row.UserID),
		ClientID:      models.OAuthClientID(row.ClientID),
		Resource:      row.Resource,
		Scopes:        unmarshalTokenScopes(row.Scopes),
		OfflineAccess: row.OfflineAccess == 1,
		CreatedAt:     fromMillis(row.CreatedAt),
		LastUsedAt:    fromNullMillis(row.LastUsedAt),
		RevokedAt:     fromNullMillis(row.RevokedAt),
		RevokeReason:  models.OAuthRevokeReason(row.RevokeReason.String),
	}
}

func authRequestFromRow(row sqlc.OauthAuthRequest) *models.OAuthAuthRequest {
	req := &models.OAuthAuthRequest{
		ID:            models.OAuthAuthRequestID(row.ID),
		ClientID:      models.OAuthClientID(row.ClientID),
		RedirectURI:   row.RedirectUri,
		Resource:      row.Resource,
		Scopes:        unmarshalTokenScopes(row.Scopes),
		OfflineAccess: row.OfflineAccess == 1,
		CodeChallenge: row.CodeChallenge,
		State:         row.State,
		DecidedAt:     fromNullMillis(row.DecidedAt),
		Denied:        row.Denied == 1,
		ExpiresAt:     fromMillis(row.ExpiresAt),
		CreatedAt:     fromMillis(row.CreatedAt),
	}
	if row.UserID.Valid {
		id := models.UserID(row.UserID.Int64)
		req.UserID = &id
	}
	if row.GrantID.Valid {
		id := models.OAuthGrantID(row.GrantID.Int64)
		req.GrantID = &id
	}
	return req
}

func deviceFromRow(row sqlc.OauthDeviceAuthorization) models.OAuthDeviceAuthorization {
	d := models.OAuthDeviceAuthorization{
		ClientID:      models.OAuthClientID(row.ClientID),
		Resource:      row.Resource,
		Scopes:        unmarshalTokenScopes(row.Scopes),
		OfflineAccess: row.OfflineAccess == 1,
		Interval:      time.Duration(row.IntervalSecs) * time.Second,
		ApprovedAt:    fromNullMillis(row.ApprovedAt),
		DeniedAt:      fromNullMillis(row.DeniedAt),
		ExpiresAt:     fromMillis(row.ExpiresAt),
		CreatedAt:     fromMillis(row.CreatedAt),
	}
	if row.UserID.Valid {
		id := models.UserID(row.UserID.Int64)
		d.UserID = &id
	}
	if row.GrantID.Valid {
		id := models.OAuthGrantID(row.GrantID.Int64)
		d.GrantID = &id
	}
	return d
}

func nullMillis(t time.Time) sql.NullInt64 {
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}
}

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func fromNullMillis(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMillis(v.Int64)
	return &t
}
