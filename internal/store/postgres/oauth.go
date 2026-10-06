package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// OAuth authorization server persistence.

var errOAuthRootOnly = errors.New("oauth replay handling must not run inside WithTx")

func (s *Store) CreateOAuthAuthRequest(ctx context.Context, req *models.OAuthAuthRequest) error {
	scopes, err := marshalTokenScopes(req.Scopes)
	if err != nil {
		return err
	}
	err = s.q.CreateOAuthAuthRequest(ctx, sqlc.CreateOAuthAuthRequestParams{
		ID:            string(req.ID),
		ClientID:      string(req.ClientID),
		RedirectUri:   req.RedirectURI,
		Resource:      req.Resource,
		Scopes:        scopes,
		OfflineAccess: req.OfflineAccess,
		CodeChallenge: req.CodeChallenge,
		State:         req.State,
		ExpiresAt:     ts(req.ExpiresAt),
		CreatedAt:     ts(req.CreatedAt),
	})
	if isUniqueViolation(err) {
		return models.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("creating oauth auth request: %w", err)
	}
	return nil
}

func (s *Store) GetOAuthAuthRequest(ctx context.Context, id models.OAuthAuthRequestID) (*models.OAuthAuthRequest, error) {
	row, err := s.q.GetOAuthAuthRequest(ctx, string(id))
	if notFound(err) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting oauth auth request: %w", err)
	}
	return authRequestFromRow(row), nil
}

func (s *Store) ApproveOAuthAuthRequest(ctx context.Context, id models.OAuthAuthRequestID, userID models.UserID, now time.Time) (*models.OAuthGrant, error) {
	var grant *models.OAuthGrant
	err := s.inTx(ctx, func(q sqlc.Querier) error {
		req, err := q.ClaimOAuthAuthRequestDecision(ctx, sqlc.ClaimOAuthAuthRequestDecisionParams{
			Now:    ts(now),
			UserID: int8Val(int64(userID)),
			ID:     string(id),
		})
		if notFound(err) {
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
			CreatedAt:     ts(now),
		})
		if err != nil {
			return fmt.Errorf("creating oauth grant: %w", err)
		}
		if err := q.SetOAuthAuthRequestGrant(ctx, sqlc.SetOAuthAuthRequestGrantParams{
			GrantID: int8Val(row.ID),
			ID:      req.ID,
		}); err != nil {
			return fmt.Errorf("linking oauth grant: %w", err)
		}
		grant = grantFromRow(row)
		return nil
	})
	return grant, err
}

func (s *Store) DenyOAuthAuthRequest(ctx context.Context, id models.OAuthAuthRequestID, userID models.UserID, now time.Time) error {
	_, err := s.q.ClaimOAuthAuthRequestDecision(ctx, sqlc.ClaimOAuthAuthRequestDecisionParams{
		Now:    ts(now),
		UserID: int8Val(int64(userID)),
		Denied: true,
		ID:     string(id),
	})
	if notFound(err) {
		return models.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("denying oauth auth request: %w", err)
	}
	return nil
}

func (s *Store) SaveOAuthAuthCode(ctx context.Context, id models.OAuthAuthRequestID, codeHash models.OAuthSecretHash, expiresAt time.Time) error {
	n, err := s.q.SaveOAuthAuthCode(ctx, sqlc.SaveOAuthAuthCodeParams{
		CodeHash:      text(codeHash.Hex()),
		CodeExpiresAt: ts(expiresAt),
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

func (s *Store) ConsumeAuthCode(ctx context.Context, codeHash models.OAuthSecretHash, now time.Time) (*models.OAuthAuthRequest, error) {
	if s.pool == nil {
		return nil, errOAuthRootOnly
	}
	hash := text(codeHash.Hex())
	row, err := s.q.ConsumeOAuthAuthCode(ctx, sqlc.ConsumeOAuthAuthCodeParams{Now: ts(now), CodeHash: hash})
	if err == nil {
		return authRequestFromRow(row), nil
	}
	if !notFound(err) {
		return nil, fmt.Errorf("consuming oauth auth code: %w", err)
	}

	state, err := s.q.GetOAuthAuthCodeState(ctx, hash)
	if notFound(err) {
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
	if _, err := s.q.RevokeOAuthGrant(ctx, sqlc.RevokeOAuthGrantParams{
		Now:    ts(now),
		Reason: text(string(models.OAuthRevokeCodeReplay)),
		ID:     state.GrantID.Int64,
	}); err != nil {
		return nil, fmt.Errorf("revoking oauth grant after code replay: %w", err)
	}
	return nil, models.ErrOAuthCodeReplay
}

func (s *Store) CreateOAuthDeviceAuthorization(ctx context.Context, d models.NewOAuthDeviceAuthorization) error {
	scopes, err := marshalTokenScopes(d.Scopes)
	if err != nil {
		return err
	}
	err = s.q.CreateOAuthDeviceAuthorization(ctx, sqlc.CreateOAuthDeviceAuthorizationParams{
		DeviceCodeHash: d.DeviceCodeHash.Hex(),
		UserCodeHash:   d.UserCodeHash.Hex(),
		ClientID:       string(d.ClientID),
		Resource:       d.Resource,
		Scopes:         scopes,
		OfflineAccess:  d.OfflineAccess,
		IntervalSecs:   int32(d.Interval / time.Second), //nolint:gosec // G115: poll interval in seconds, small bounded value
		ExpiresAt:      ts(d.ExpiresAt),
		CreatedAt:      ts(d.CreatedAt),
	})
	if isUniqueViolation(err) {
		return models.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("creating oauth device authorization: %w", err)
	}
	return nil
}

func (s *Store) GetPendingOAuthDeviceAuthorization(ctx context.Context, userCodeHash models.OAuthSecretHash, now time.Time) (*models.OAuthDeviceAuthorization, error) {
	row, err := s.q.GetPendingOAuthDeviceAuthorization(ctx, sqlc.GetPendingOAuthDeviceAuthorizationParams{
		UserCodeHash: userCodeHash.Hex(),
		Now:          ts(now),
	})
	if notFound(err) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting oauth device authorization: %w", err)
	}
	d := deviceFromRow(row)
	return &d, nil
}

func (s *Store) ApproveOAuthDeviceAuthorization(ctx context.Context, userCodeHash models.OAuthSecretHash, userID models.UserID, now time.Time) (*models.OAuthGrant, error) {
	var grant *models.OAuthGrant
	err := s.inTx(ctx, func(q sqlc.Querier) error {
		d, err := q.ApproveOAuthDeviceAuthorization(ctx, sqlc.ApproveOAuthDeviceAuthorizationParams{
			Now:          ts(now),
			UserID:       int8Val(int64(userID)),
			UserCodeHash: userCodeHash.Hex(),
		})
		if notFound(err) {
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
			CreatedAt:     ts(now),
		})
		if err != nil {
			return fmt.Errorf("creating oauth grant: %w", err)
		}
		if err := q.SetOAuthDeviceAuthorizationGrant(ctx, sqlc.SetOAuthDeviceAuthorizationGrantParams{
			GrantID:        int8Val(row.ID),
			DeviceCodeHash: d.DeviceCodeHash,
		}); err != nil {
			return fmt.Errorf("linking oauth grant: %w", err)
		}
		grant = grantFromRow(row)
		return nil
	})
	return grant, err
}

func (s *Store) DenyOAuthDeviceAuthorization(ctx context.Context, userCodeHash models.OAuthSecretHash, userID models.UserID, now time.Time) error {
	n, err := s.q.DenyOAuthDeviceAuthorization(ctx, sqlc.DenyOAuthDeviceAuthorizationParams{
		Now:          ts(now),
		UserID:       int8Val(int64(userID)),
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

func (s *Store) RecordDevicePoll(ctx context.Context, deviceCodeHash models.OAuthSecretHash, clientID models.OAuthClientID, now time.Time) (models.OAuthDevicePoll, error) {
	row, err := s.q.RecordOAuthDevicePoll(ctx, sqlc.RecordOAuthDevicePollParams{
		Now:            ts(now),
		DeviceCodeHash: deviceCodeHash.Hex(),
		ClientID:       string(clientID),
	})
	if notFound(err) {
		return models.OAuthDevicePoll{}, models.ErrNotFound
	}
	if err != nil {
		return models.OAuthDevicePoll{}, fmt.Errorf("recording oauth device poll: %w", err)
	}
	if !row.OnTime {
		return models.OAuthDevicePoll{Status: models.DevicePollSlowDown}, nil
	}
	d := deviceFromRow(sqlc.OauthDeviceAuthorization{
		DeviceCodeHash: row.DeviceCodeHash,
		UserCodeHash:   row.UserCodeHash,
		ClientID:       row.ClientID,
		Resource:       row.Resource,
		Scopes:         row.Scopes,
		OfflineAccess:  row.OfflineAccess,
		IntervalSecs:   row.IntervalSecs,
		LastPolledAt:   row.LastPolledAt,
		UserID:         row.UserID,
		GrantID:        row.GrantID,
		ApprovedAt:     row.ApprovedAt,
		DeniedAt:       row.DeniedAt,
		ConsumedAt:     row.ConsumedAt,
		ExpiresAt:      row.ExpiresAt,
		CreatedAt:      row.CreatedAt,
	})
	return models.OAuthDevicePoll{Status: devicePollStatus(d, now), Authorization: d}, nil
}

func (s *Store) ConsumeDeviceCode(ctx context.Context, deviceCodeHash models.OAuthSecretHash, clientID models.OAuthClientID, now time.Time) (*models.OAuthGrant, error) {
	grantID, err := s.q.ConsumeOAuthDeviceCode(ctx, sqlc.ConsumeOAuthDeviceCodeParams{
		Now:            ts(now),
		DeviceCodeHash: deviceCodeHash.Hex(),
		ClientID:       string(clientID),
	})
	if notFound(err) {
		return nil, models.ErrOAuthInvalidGrant
	}
	if err != nil {
		return nil, fmt.Errorf("consuming oauth device code: %w", err)
	}
	row, err := s.q.GetOAuthGrant(ctx, grantID.Int64)
	if err != nil {
		return nil, fmt.Errorf("getting oauth grant: %w", err)
	}
	return grantFromRow(row), nil
}

func (s *Store) IssueOAuthTokens(ctx context.Context, grantID models.OAuthGrantID, issue models.OAuthTokenIssue, now time.Time) error {
	return s.inTx(ctx, func(q sqlc.Querier) error {
		return issueOAuthTokens(ctx, q, int64(grantID), issue, now)
	})
}

func (s *Store) CheckRefreshToken(ctx context.Context, tokenHash models.OAuthSecretHash, now time.Time) (models.RefreshOutcome, error) {
	if s.pool == nil {
		return models.RefreshOutcome{}, errOAuthRootOnly
	}
	return classifyRefreshToken(ctx, s.q, tokenHash, now)
}

func (s *Store) RotateRefreshToken(ctx context.Context, oldHash models.OAuthSecretHash, issue models.OAuthTokenIssue, now time.Time) (models.RefreshOutcome, error) {
	if s.pool == nil {
		return models.RefreshOutcome{}, errOAuthRootOnly
	}
	if issue.RefreshHash == nil {
		return models.RefreshOutcome{}, errors.New("refresh rotation needs a new refresh token")
	}
	var out models.RefreshOutcome
	// A replay returns nil from the callback so the grant revocation commits.
	err := s.inTx(ctx, func(q sqlc.Querier) error {
		grantID, err := q.ConsumeOAuthRefreshToken(ctx, sqlc.ConsumeOAuthRefreshTokenParams{
			Now:            ts(now),
			ReplacedByHash: text(issue.RefreshHash.Hex()),
			TokenHash:      oldHash.Hex(),
		})
		if notFound(err) {
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

func (s *Store) AuthenticateOAuthAccessToken(ctx context.Context, idHash models.OAuthSecretHash, resource string, now time.Time) (*models.OAuthGrant, *models.User, error) {
	row, err := s.q.AuthenticateOAuthAccessToken(ctx, sqlc.AuthenticateOAuthAccessTokenParams{
		IDHash:   idHash.Hex(),
		Now:      ts(now),
		Resource: resource,
	})
	if notFound(err) {
		return nil, nil, models.ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("authenticating oauth access token: %w", err)
	}
	return grantFromRow(row.OauthGrant), userToModel(row.User), nil
}

func (s *Store) TouchOAuthGrant(ctx context.Context, id models.OAuthGrantID, now time.Time) error {
	if err := s.q.TouchOAuthGrant(ctx, sqlc.TouchOAuthGrantParams{LastUsedAt: ts(now), ID: int64(id)}); err != nil {
		return fmt.Errorf("touching oauth grant: %w", err)
	}
	return nil
}

func (s *Store) RevokeGrant(ctx context.Context, id models.OAuthGrantID, userID models.UserID, now time.Time) error {
	n, err := s.q.RevokeOAuthGrantForUser(ctx, sqlc.RevokeOAuthGrantForUserParams{
		Now:    ts(now),
		Reason: text(string(models.OAuthRevokeUser)),
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

func (s *Store) RevokeGrantByToken(ctx context.Context, tokenHash models.OAuthSecretHash, clientID models.OAuthClientID, now time.Time) error {
	if err := s.q.RevokeOAuthGrantByToken(ctx, sqlc.RevokeOAuthGrantByTokenParams{
		Now:       ts(now),
		Reason:    text(string(models.OAuthRevokeTokenEndpoint)),
		ClientID:  string(clientID),
		TokenHash: tokenHash.Hex(),
	}); err != nil {
		return fmt.Errorf("revoking oauth grant by token: %w", err)
	}
	return nil
}

func (s *Store) ListGrantsForUser(ctx context.Context, userID models.UserID) ([]*models.OAuthGrant, error) {
	rows, err := s.q.ListOAuthGrantsForUser(ctx, int64(userID))
	if err != nil {
		return nil, fmt.Errorf("listing oauth grants: %w", err)
	}
	grants := make([]*models.OAuthGrant, len(rows))
	for i := range rows {
		grants[i] = grantFromRow(rows[i])
	}
	return grants, nil
}

func (s *Store) DeleteExpiredOAuthRows(ctx context.Context, now time.Time) error {
	at := ts(now)
	return s.inTx(ctx, func(q sqlc.Querier) error {
		if err := q.DeleteExpiredOAuthAuthRequests(ctx, at); err != nil {
			return fmt.Errorf("deleting expired oauth auth requests: %w", err)
		}
		if err := q.DeleteExpiredOAuthDeviceAuthorizations(ctx, at); err != nil {
			return fmt.Errorf("deleting expired oauth device authorizations: %w", err)
		}
		if err := q.DeleteExpiredOAuthAccessTokens(ctx, at); err != nil {
			return fmt.Errorf("deleting expired oauth access tokens: %w", err)
		}
		if err := q.DeleteExpiredOAuthRefreshFamilies(ctx, at); err != nil {
			return fmt.Errorf("deleting expired oauth refresh tokens: %w", err)
		}
		return nil
	})
}

// inTx runs fn in one transaction. On a tx-scoped Store it joins the caller's
// transaction instead.
func (s *Store) inTx(ctx context.Context, fn func(q sqlc.Querier) error) error {
	if s.pool == nil {
		return fn(s.q)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(sqlc.New(tx)); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			s.log.Error("failed to roll back transaction", "error", rbErr, "cause", err)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func issueOAuthTokens(ctx context.Context, q sqlc.Querier, grantID int64, issue models.OAuthTokenIssue, now time.Time) error {
	n, err := q.InsertOAuthAccessToken(ctx, sqlc.InsertOAuthAccessTokenParams{
		IDHash:    issue.AccessIDHash.Hex(),
		ExpiresAt: ts(issue.AccessExpiresAt),
		CreatedAt: ts(now),
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
		ExpiresAt: ts(issue.RefreshExpiresAt),
		CreatedAt: ts(now),
		GrantID:   grantID,
	}); err != nil {
		return fmt.Errorf("inserting oauth refresh token: %w", err)
	}
	return nil
}

// classifyRefreshToken reads a refresh token's state. A consumed token is a
// replay: the grant is revoked through q, which commits with q's transaction.
func classifyRefreshToken(ctx context.Context, q sqlc.Querier, tokenHash models.OAuthSecretHash, now time.Time) (models.RefreshOutcome, error) {
	invalid := models.RefreshOutcome{Kind: models.RefreshInvalid}
	row, err := q.GetOAuthRefreshTokenState(ctx, tokenHash.Hex())
	if notFound(err) {
		return invalid, nil
	}
	if err != nil {
		return models.RefreshOutcome{}, fmt.Errorf("reading oauth refresh token: %w", err)
	}
	if row.ConsumedAt.Valid {
		if _, err := q.RevokeOAuthGrant(ctx, sqlc.RevokeOAuthGrantParams{
			Now:    ts(now),
			Reason: text(string(models.OAuthRevokeRefreshReplay)),
			ID:     row.OauthGrant.ID,
		}); err != nil {
			return models.RefreshOutcome{}, fmt.Errorf("revoking oauth grant after refresh replay: %w", err)
		}
		return models.RefreshOutcome{Kind: models.RefreshReplay}, nil
	}
	if !now.Before(row.ExpiresAt.Time) || row.OauthGrant.RevokedAt.Valid {
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
		OfflineAccess: row.OfflineAccess,
		CreatedAt:     row.CreatedAt.Time,
		LastUsedAt:    tsPtr(row.LastUsedAt),
		RevokedAt:     tsPtr(row.RevokedAt),
		RevokeReason:  models.OAuthRevokeReason(textStr(row.RevokeReason)),
	}
}

func authRequestFromRow(row sqlc.OauthAuthRequest) *models.OAuthAuthRequest {
	return &models.OAuthAuthRequest{
		ID:            models.OAuthAuthRequestID(row.ID),
		ClientID:      models.OAuthClientID(row.ClientID),
		RedirectURI:   row.RedirectUri,
		Resource:      row.Resource,
		Scopes:        unmarshalTokenScopes(row.Scopes),
		OfflineAccess: row.OfflineAccess,
		CodeChallenge: row.CodeChallenge,
		State:         row.State,
		UserID:        userIDPtr(row.UserID),
		GrantID:       grantIDPtr(row.GrantID),
		DecidedAt:     tsPtr(row.DecidedAt),
		Denied:        row.Denied,
		ExpiresAt:     row.ExpiresAt.Time,
		CreatedAt:     row.CreatedAt.Time,
	}
}

func deviceFromRow(row sqlc.OauthDeviceAuthorization) models.OAuthDeviceAuthorization {
	return models.OAuthDeviceAuthorization{
		ClientID:      models.OAuthClientID(row.ClientID),
		Resource:      row.Resource,
		Scopes:        unmarshalTokenScopes(row.Scopes),
		OfflineAccess: row.OfflineAccess,
		Interval:      time.Duration(row.IntervalSecs) * time.Second,
		UserID:        userIDPtr(row.UserID),
		GrantID:       grantIDPtr(row.GrantID),
		ApprovedAt:    tsPtr(row.ApprovedAt),
		DeniedAt:      tsPtr(row.DeniedAt),
		ExpiresAt:     row.ExpiresAt.Time,
		CreatedAt:     row.CreatedAt.Time,
	}
}

func grantIDPtr(v pgtype.Int8) *models.OAuthGrantID {
	if !v.Valid {
		return nil
	}
	id := models.OAuthGrantID(v.Int64)
	return &id
}
