package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nees/zorail/internal/model"
	"github.com/nees/zorail/internal/storage"
)

// isUnique reports whether err is a SQLite UNIQUE-constraint violation.
func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func csvJoin(xs []string) string { return strings.Join(xs, ",") }

func csvSplit(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- Users ---

func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (?,?,?,?)`,
		u.ID, strings.ToLower(u.Email), u.PasswordHash, u.CreatedAt.Unix())
	if isUnique(err) {
		return storage.ErrConflict
	}
	return err
}

func (s *Store) scanUser(row *sql.Row) (*model.User, error) {
	u := &model.User{}
	var created int64
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	return u, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = ?`, strings.ToLower(email)))
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id = ?`, id))
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// --- Instance settings ---

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}

// --- API keys ---

func (s *Store) CreateAPIKey(ctx context.Context, k *model.APIKey) error {
	scopes := make([]string, len(k.Scopes))
	for i, sc := range k.Scopes {
		scopes[i] = string(sc)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, user_id, name, key_hash, scopes, inbox_prefix, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		k.ID, k.UserID, k.Name, k.KeyHash, csvJoin(scopes), k.InboxPrefix, k.CreatedAt.Unix())
	if isUnique(err) {
		return storage.ErrConflict
	}
	return err
}

func scanKey(sc func(...any) error) (*model.APIKey, error) {
	k := &model.APIKey{}
	var scopes string
	var created int64
	var lastUsed sql.NullInt64
	if err := sc(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &scopes, &k.InboxPrefix, &created, &lastUsed); err != nil {
		return nil, err
	}
	for _, p := range csvSplit(scopes) {
		k.Scopes = append(k.Scopes, model.Scope(p))
	}
	k.CreatedAt = time.Unix(created, 0).UTC()
	if lastUsed.Valid {
		k.LastUsedAt = time.Unix(lastUsed.Int64, 0).UTC()
	}
	return k, nil
}

const keyCols = `id, user_id, name, key_hash, scopes, inbox_prefix, created_at, last_used_at`

func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*model.APIKey, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+keyCols+` FROM api_keys WHERE key_hash = ?`, hash)
	k, err := scanKey(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// best-effort last_used bump; ignore failure
	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, time.Now().Unix(), k.ID)
	return k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]*model.APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+keyCols+` FROM api_keys WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.APIKey
	for rows.Next() {
		k, err := scanKey(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIKey(ctx context.Context, id, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// --- Addresses ---

func (s *Store) UpsertAddress(ctx context.Context, a *model.Address) error {
	var expires any
	if a.ExpiresAt != nil {
		expires = a.ExpiresAt.Unix()
	}
	fe := 0
	if a.ForwardEnabled {
		fe = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO addresses (address, type, owner_user_id, expires_at, forward_to, forward_enabled, created_at)
VALUES (?,?,?,?,?,?,?)
ON CONFLICT(address) DO UPDATE SET
	type=excluded.type,
	owner_user_id=excluded.owner_user_id,
	expires_at=excluded.expires_at,
	forward_to=excluded.forward_to,
	forward_enabled=excluded.forward_enabled`,
		strings.ToLower(a.Address), string(a.Type), nullStr(a.OwnerUserID), expires,
		csvJoin(a.ForwardTo), fe, a.CreatedAt.Unix())
	return err
}

func scanAddress(sc func(...any) error) (*model.Address, error) {
	a := &model.Address{}
	var typ, owner, fwd string
	var expires sql.NullInt64
	var fe int
	var created int64
	if err := sc(&a.Address, &typ, &owner, &expires, &fwd, &fe, &created); err != nil {
		return nil, err
	}
	a.Type = model.AddressType(typ)
	a.OwnerUserID = owner
	if expires.Valid {
		t := time.Unix(expires.Int64, 0).UTC()
		a.ExpiresAt = &t
	}
	a.ForwardTo = csvSplit(fwd)
	a.ForwardEnabled = fe != 0
	a.CreatedAt = time.Unix(created, 0).UTC()
	return a, nil
}

const addrCols = `address, type, COALESCE(owner_user_id,''), expires_at, forward_to, forward_enabled, created_at`

func (s *Store) GetAddress(ctx context.Context, address string) (*model.Address, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+addrCols+` FROM addresses WHERE address = ?`, strings.ToLower(address))
	a, err := scanAddress(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	return a, err
}

func (s *Store) ListAddresses(ctx context.Context, userID string) ([]*model.Address, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+addrCols+` FROM addresses WHERE owner_user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Address
	for rows.Next() {
		a, err := scanAddress(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAddress(ctx context.Context, address, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM addresses WHERE address = ? AND owner_user_id = ?`, strings.ToLower(address), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// --- Forwarding ---

func (s *Store) EnqueueForward(ctx context.Context, j *model.ForwardJob) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO forward_jobs (id, message_id, src_address, dest, raw, attempts, next_attempt_at, status, last_error, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.MessageID, j.SrcAddress, j.Dest, j.Raw, j.Attempts,
		j.NextAttemptAt.Unix(), string(j.Status), j.LastError, j.CreatedAt.Unix())
	return err
}

// ClaimForwardJobs atomically marks due pending jobs as in-flight (attempts++)
// and returns them, so a crashed worker's jobs become due again after backoff.
func (s *Store) ClaimForwardJobs(ctx context.Context, now time.Time, limit int) ([]*model.ForwardJob, error) {
	if limit <= 0 {
		limit = 10
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
SELECT id, message_id, src_address, dest, raw, attempts, next_attempt_at, status, last_error, created_at
FROM forward_jobs
WHERE status = 'pending' AND next_attempt_at <= ?
ORDER BY next_attempt_at ASC
LIMIT ?`, now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	var out []*model.ForwardJob
	for rows.Next() {
		j := &model.ForwardJob{}
		var status string
		var next, created int64
		var lastErr sql.NullString
		if err := rows.Scan(&j.ID, &j.MessageID, &j.SrcAddress, &j.Dest, &j.Raw, &j.Attempts, &next, &status, &lastErr, &created); err != nil {
			_ = rows.Close()
			return nil, err
		}
		j.Status = model.ForwardStatus(status)
		j.NextAttemptAt = time.Unix(next, 0).UTC()
		j.CreatedAt = time.Unix(created, 0).UTC()
		j.LastError = lastErr.String
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	// Push next_attempt_at out so a concurrent claim won't re-grab these while
	// we work; the worker resets it via MarkForward* on completion.
	lease := now.Add(2 * time.Minute).Unix()
	for _, j := range out {
		if _, err := tx.ExecContext(ctx, `UPDATE forward_jobs SET attempts = attempts + 1, next_attempt_at = ? WHERE id = ?`, lease, j.ID); err != nil {
			return nil, err
		}
		j.Attempts++
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) MarkForwardSent(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE forward_jobs SET status='sent', last_error='' WHERE id = ?`, id)
	return err
}

func (s *Store) MarkForwardRetry(ctx context.Context, id string, attempts int, next time.Time, lastErr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE forward_jobs SET status='pending', attempts=?, next_attempt_at=?, last_error=? WHERE id = ?`,
		attempts, next.Unix(), lastErr, id)
	return err
}

func (s *Store) MarkForwardFailed(ctx context.Context, id, lastErr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE forward_jobs SET status='failed', last_error=? WHERE id = ?`, lastErr, id)
	return err
}

// --- Mailbox verification ---

func (s *Store) CreateVerification(ctx context.Context, v *model.MailboxVerification) error {
	var verified any
	if v.VerifiedAt != nil {
		verified = v.VerifiedAt.Unix()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO mailbox_verifications (dest, user_id, token, verified_at, created_at)
VALUES (?,?,?,?,?)
ON CONFLICT(dest) DO UPDATE SET token=excluded.token, verified_at=NULL, created_at=excluded.created_at`,
		strings.ToLower(v.Dest), v.UserID, v.Token, verified, v.CreatedAt.Unix())
	return err
}

func (s *Store) GetVerificationByToken(ctx context.Context, token string) (*model.MailboxVerification, error) {
	v := &model.MailboxVerification{}
	var verified sql.NullInt64
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT dest, user_id, token, verified_at, created_at FROM mailbox_verifications WHERE token = ?`, token).
		Scan(&v.Dest, &v.UserID, &v.Token, &verified, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if verified.Valid {
		t := time.Unix(verified.Int64, 0).UTC()
		v.VerifiedAt = &t
	}
	v.CreatedAt = time.Unix(created, 0).UTC()
	return v, nil
}

func (s *Store) MarkVerified(ctx context.Context, dest string, when time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE mailbox_verifications SET verified_at = ? WHERE dest = ?`, when.Unix(), strings.ToLower(dest))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) IsVerified(ctx context.Context, dest string) (bool, error) {
	var verified sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT verified_at FROM mailbox_verifications WHERE dest = ?`, strings.ToLower(dest)).Scan(&verified)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return verified.Valid, nil
}

// --- Retention ---

// ExpireMessages deletes messages older than cutoff whose inbox is not a
// reserved/forward address (those are exempt from sweeping).
func (s *Store) ExpireMessages(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM messages
WHERE received_at < ?
  AND inbox NOT IN (
		SELECT address FROM addresses WHERE type IN ('reserved','forward')
  )`, cutoff.Unix())
	if err != nil {
		return 0, fmt.Errorf("expire messages: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
