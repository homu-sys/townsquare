package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrInvalid       = errors.New("invalid")
	ErrInviteUsed    = errors.New("invite already used")
	ErrInviteExpired = errors.New("invite expired")
)

type Store struct {
	db *sql.DB
}

const (
	RoleUser      = "user"
	RoleModerator = "moderator"
	RoleAdmin     = "admin"
	RoleOwner     = "owner"
)

type User struct {
	ID           string
	Username     string
	DisplayName  string
	Email        string
	PasswordHash string
	Bio          string
	Avatar       string
	Role         string
	Disabled     bool
	CreatedAt    string
	APITokenHash string
}

func (u *User) IsOwner() bool {
	return u != nil && u.Role == RoleOwner
}

func (u *User) IsAdmin() bool {
	return u != nil && (u.Role == RoleAdmin || u.Role == RoleOwner)
}

func (u *User) IsModerator() bool {
	return u != nil && (u.Role == RoleModerator || u.IsAdmin())
}

func (u *User) IsStaff() bool {
	return u.IsModerator()
}

type Post struct {
	ID                string
	AuthorID          string
	AuthorUsername    string
	AuthorDisplayName string
	AuthorRole        string
	Channel           string
	Title             string
	BodyMD            string
	BodyHTML          string
	CreatedAt         string
	UpdatedAt         string
	CommentCount      int
	New               bool
	NewComments       int
}

type Comment struct {
	ID                string
	PostID            string
	ParentID          string
	AuthorID          string
	AuthorUsername    string
	AuthorDisplayName string
	AuthorRole        string
	BodyMD            string
	BodyHTML          string
	CreatedAt         string
}

type Invite struct {
	Code              string
	CreatedBy         string
	CreatedByUsername string
	CreatedAt         string
	UsedBy            string
	UsedByUsername    string
	UsedAt            string
	ExpiresAt         string
}

type Session struct {
	ID        string
	UserID    string
	CSRFToken string
	CreatedAt string
	ExpiresAt string
}

type PasswordReset struct {
	TokenHash string
	UserID    string
	CreatedAt string
	ExpiresAt string
}

type DirectMessage struct {
	ID                   string
	SenderID             string
	RecipientID          string
	SenderUsername       string
	SenderDisplayName    string
	RecipientUsername    string
	RecipientDisplayName string
	BodyMD               string
	BodyHTML             string
	CreatedAt            string
	Blocked              bool
	BlockReason          string
}

type Conversation struct {
	UserID      string
	Username    string
	DisplayName string
	LastAt      string
}

type Reaction struct {
	Emoji   string
	Count   int
	Reacted bool
}

type TagCount struct {
	Tag   string
	Count int
}

type NewsItem struct {
	ID        string
	Body      string
	CreatedAt string
}

type RemotePost struct {
	Instance          string
	ID                string
	SiteName          string
	URL               string
	Title             string
	BodyMD            string
	AuthorUsername    string
	AuthorDisplayName string
	AuthorURL         string
	Channel           string
	CreatedAt         string
	UpdatedAt         string
	CommentCount      int
	FetchedAt         string
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY,
  username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
  display_name  TEXT NOT NULL,
  email         TEXT UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  bio           TEXT NOT NULL DEFAULT '',
  avatar        TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'user',
  disabled      INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL,
  api_token_hash TEXT UNIQUE
);

CREATE TABLE IF NOT EXISTS invites (
  code       TEXT PRIMARY KEY,
  created_by TEXT NOT NULL REFERENCES users(id),
  created_at TEXT NOT NULL,
  used_by    TEXT REFERENCES users(id),
  used_at    TEXT,
  expires_at TEXT
);

CREATE TABLE IF NOT EXISTS sessions (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS posts (
  id         TEXT PRIMARY KEY,
  author_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  body_md    TEXT NOT NULL,
  body_html  TEXT NOT NULL,
  channel    TEXT NOT NULL DEFAULT 'general',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS comments (
  id        TEXT PRIMARY KEY,
  post_id   TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  parent_id TEXT REFERENCES comments(id) ON DELETE CASCADE,
  author_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body_md   TEXT NOT NULL,
  body_html TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS direct_messages (
  id           TEXT PRIMARY KEY,
  sender_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  recipient_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body_md      TEXT NOT NULL,
  body_html    TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  blocked      INTEGER NOT NULL DEFAULT 0,
  block_reason TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS user_mutes (
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  muted_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  PRIMARY KEY(user_id, muted_id)
);

CREATE TABLE IF NOT EXISTS user_blocks (
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  blocked_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  PRIMARY KEY(user_id, blocked_id)
);

CREATE TABLE IF NOT EXISTS bookmarks (
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  post_id    TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  PRIMARY KEY(user_id, post_id)
);

CREATE TABLE IF NOT EXISTS user_visits (
  user_id      TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  last_seen_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS post_tags (
  post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  tag     TEXT NOT NULL,
  PRIMARY KEY(post_id, tag)
);

CREATE TABLE IF NOT EXISTS reactions (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  target_type TEXT NOT NULL,
  target_id   TEXT NOT NULL,
  emoji       TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  UNIQUE(user_id, target_type, target_id, emoji)
);

CREATE TABLE IF NOT EXISTS visitors (
  id         TEXT PRIMARY KEY,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS news (
  id         TEXT PRIMARY KEY,
  body       TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS remote_posts (
  instance            TEXT NOT NULL,
  id                  TEXT NOT NULL,
  site_name           TEXT NOT NULL,
  url                 TEXT NOT NULL,
  title               TEXT NOT NULL,
  body_md             TEXT NOT NULL,
  author_username     TEXT NOT NULL,
  author_display_name TEXT NOT NULL,
  author_url          TEXT NOT NULL,
  channel             TEXT NOT NULL,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  comment_count       INTEGER NOT NULL DEFAULT 0,
  fetched_at          TEXT NOT NULL,
  PRIMARY KEY(instance, id)
);

CREATE TABLE IF NOT EXISTS recovery_codes (
  code_hash  TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS password_resets (
  token_hash TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_comments_post ON comments(post_id, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_recovery_codes_user ON recovery_codes(user_id);
CREATE INDEX IF NOT EXISTS idx_password_resets_user ON password_resets(user_id);
CREATE INDEX IF NOT EXISTS idx_password_resets_expiry ON password_resets(expires_at);
CREATE INDEX IF NOT EXISTS idx_direct_messages_sender ON direct_messages(sender_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_direct_messages_recipient ON direct_messages(recipient_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_user_blocks_blocked ON user_blocks(blocked_id, user_id);
CREATE INDEX IF NOT EXISTS idx_bookmarks_user ON bookmarks(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_bookmarks_post ON bookmarks(post_id);
CREATE INDEX IF NOT EXISTS idx_direct_messages_blocked ON direct_messages(blocked, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reactions_target ON reactions(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_post_tags_tag ON post_tags(tag, post_id);
CREATE INDEX IF NOT EXISTS idx_news_created ON news(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_remote_posts_created ON remote_posts(created_at DESC);
`

func Open(path string) (*Store, error) {
	dsn := path
	if !strings.HasPrefix(dsn, "file:") {
		if dsn == ":memory:" {
			dsn = "file::memory:?cache=shared"
		} else {
			dsn = "file:" + dsn
		}
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrateOwnerRole(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate owner role: %w", err)
	}
	if err := migrateOptionalEmail(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate email column: %w", err)
	}
	if err := migrateAvatar(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate avatar column: %w", err)
	}
	if err := migratePostChannel(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate post channel column: %w", err)
	}
	if err := migrateCommentParent(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate comment parent column: %w", err)
	}
	if _, err := db.Exec(`UPDATE meta SET value = '0'
		WHERE key = 'hits' AND NOT EXISTS (SELECT 1 FROM visitors)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("reset page view counter: %w", err)
	}
	return &Store{db: db}, nil
}

func migrateOptionalEmail(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return err
	}
	var emailNotNull bool
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "email" {
			emailNotNull = notNull != 0
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !emailNotNull {
		return nil
	}

	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer db.Exec(`PRAGMA foreign_keys=ON`)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TABLE users_new (
		id             TEXT PRIMARY KEY,
		username       TEXT NOT NULL UNIQUE COLLATE NOCASE,
		display_name   TEXT NOT NULL,
		email          TEXT UNIQUE COLLATE NOCASE,
		password_hash  TEXT NOT NULL,
		bio            TEXT NOT NULL DEFAULT '',
		role           TEXT NOT NULL DEFAULT 'user',
		disabled       INTEGER NOT NULL DEFAULT 0,
		created_at     TEXT NOT NULL,
		api_token_hash TEXT UNIQUE
	)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO users_new
		(id, username, display_name, email, password_hash, bio, role, disabled, created_at, api_token_hash)
		SELECT id, username, display_name, NULLIF(email, ''), password_hash, bio, role, disabled,
			created_at, api_token_hash FROM users`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE users`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE users_new RENAME TO users`); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateAvatar(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return err
	}
	hasAvatar := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "avatar" {
			hasAvatar = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if hasAvatar {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE users ADD COLUMN avatar TEXT NOT NULL DEFAULT ''`)
	return err
}

func migratePostChannel(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(posts)`)
	if err != nil {
		return err
	}
	hasChannel := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "channel" {
			hasChannel = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !hasChannel {
		if _, err := db.Exec(`ALTER TABLE posts ADD COLUMN channel TEXT NOT NULL DEFAULT 'general'`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_posts_channel ON posts(channel, created_at DESC)`)
	return err
}

func migrateOwnerRole(db *sql.DB) error {
	var owners int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'owner'`).Scan(&owners); err != nil {
		return err
	}
	if owners > 0 {
		return nil
	}
	var id string
	err := db.QueryRow(`SELECT id FROM users WHERE role = 'admin'
		ORDER BY created_at ASC, id ASC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE users SET role = 'owner' WHERE id = ?`, id)
	return err
}

func migrateCommentParent(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(comments)`)
	if err != nil {
		return err
	}
	hasParent := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "parent_id" {
			hasParent = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !hasParent {
		if _, err := db.Exec(`ALTER TABLE comments
			ADD COLUMN parent_id TEXT REFERENCES comments(id) ON DELETE CASCADE`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_comments_parent ON comments(parent_id)`)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func Now() string { return time.Now().UTC().Format(time.RFC3339) }

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func NewCode() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func insertUser(e execer, u *User) error {
	_, err := e.Exec(`INSERT INTO users
		(id, username, display_name, email, password_hash, bio, avatar, role, disabled, created_at, api_token_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, nullIfEmpty(strings.TrimSpace(u.Email)), u.PasswordHash, u.Bio, u.Avatar, u.Role,
		boolInt(u.Disabled), u.CreatedAt, nullIfEmpty(u.APITokenHash))
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Store) CreateUser(u *User) error {
	if u.ID == "" {
		u.ID = NewID()
	}
	if u.CreatedAt == "" {
		u.CreatedAt = Now()
	}
	if u.Role == "" {
		u.Role = "user"
	}
	return insertUser(s.db, u)
}

func (s *Store) CreateFirstUser(u *User) error {
	if u.ID == "" {
		u.ID = NewID()
	}
	if u.CreatedAt == "" {
		u.CreatedAt = Now()
	}
	if u.Role == "" {
		u.Role = RoleOwner
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrConflict
	}
	if err := insertUser(tx, u); err != nil {
		return err
	}
	return tx.Commit()
}

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var disabled int
	var email, token sql.NullString
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &email, &u.PasswordHash,
		&u.Bio, &u.Avatar, &u.Role, &disabled, &u.CreatedAt, &token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Email = email.String
	u.Disabled = disabled != 0
	u.APITokenHash = token.String
	return &u, nil
}

func (s *Store) GetUserByID(id string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT id, username, display_name, email, password_hash, bio, avatar,
		role, disabled, created_at, api_token_hash FROM users WHERE id = ?`, id))
}

func (s *Store) GetUserByUsername(username string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT id, username, display_name, email, password_hash, bio, avatar,
		role, disabled, created_at, api_token_hash FROM users WHERE username = ?`, username))
}

func (s *Store) GetUserByLogin(login string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT id, username, display_name, email, password_hash, bio, avatar,
		role, disabled, created_at, api_token_hash FROM users
		WHERE username = ? OR email = ?`, login, login))
}

func (s *Store) GetUserByAPITokenHash(hash string) (*User, error) {
	if hash == "" {
		return nil, ErrNotFound
	}
	return scanUser(s.db.QueryRow(`SELECT id, username, display_name, email, password_hash, bio, avatar,
		role, disabled, created_at, api_token_hash FROM users WHERE api_token_hash = ?`, hash))
}

func (s *Store) ListUsers(limit, offset int) ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, display_name, email, password_hash, bio, avatar,
		role, disabled, created_at, api_token_hash FROM users
		ORDER BY username COLLATE NOCASE ASC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) ListActiveUsers(limit, offset int) ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, display_name, email, password_hash, bio, avatar,
		role, disabled, created_at, api_token_hash FROM users
		WHERE disabled = 0
		ORDER BY username COLLATE NOCASE ASC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) MuteUser(userID, targetID string) error {
	if userID == "" || targetID == "" || userID == targetID {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO user_mutes(user_id, muted_id, created_at)
		VALUES (?, ?, ?) ON CONFLICT(user_id, muted_id) DO NOTHING`, userID, targetID, Now())
	return err
}

func (s *Store) UnmuteUser(userID, targetID string) error {
	_, err := s.db.Exec(`DELETE FROM user_mutes WHERE user_id = ? AND muted_id = ?`, userID, targetID)
	return err
}

func (s *Store) BlockUser(userID, targetID string) error {
	if userID == "" || targetID == "" || userID == targetID {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO user_blocks(user_id, blocked_id, created_at)
		VALUES (?, ?, ?) ON CONFLICT(user_id, blocked_id) DO NOTHING`, userID, targetID, Now()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM user_mutes WHERE user_id = ? AND muted_id = ?`, userID, targetID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UnblockUser(userID, targetID string) error {
	_, err := s.db.Exec(`DELETE FROM user_blocks WHERE user_id = ? AND blocked_id = ?`, userID, targetID)
	return err
}

func (s *Store) IsMuted(userID, targetID string) (bool, error) {
	return s.relationshipExists(`SELECT 1 FROM user_mutes WHERE user_id = ? AND muted_id = ?`, userID, targetID)
}

func (s *Store) IsBlocked(userID, targetID string) (bool, error) {
	return s.relationshipExists(`SELECT 1 FROM user_blocks WHERE user_id = ? AND blocked_id = ?`, userID, targetID)
}

func (s *Store) IsBlockedEither(userA, userB string) (bool, error) {
	if userA == "" || userB == "" {
		return false, nil
	}
	return s.relationshipExists(`SELECT 1 FROM user_blocks
		WHERE (user_id = ? AND blocked_id = ?) OR (user_id = ? AND blocked_id = ?)`,
		userA, userB, userB, userA)
}

func (s *Store) relationshipExists(query string, args ...any) (bool, error) {
	var one int
	err := s.db.QueryRow(query, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ListMutedUsers(userID string) ([]User, error) {
	return s.listRelationshipUsers(`SELECT u.id, u.username, u.display_name, u.email, u.password_hash,
		u.bio, u.avatar, u.role, u.disabled, u.created_at, u.api_token_hash
		FROM users u JOIN user_mutes m ON m.muted_id = u.id
		WHERE m.user_id = ? ORDER BY u.username COLLATE NOCASE ASC`, userID)
}

func (s *Store) ListBlockedUsers(userID string) ([]User, error) {
	return s.listRelationshipUsers(`SELECT u.id, u.username, u.display_name, u.email, u.password_hash,
		u.bio, u.avatar, u.role, u.disabled, u.created_at, u.api_token_hash
		FROM users u JOIN user_blocks b ON b.blocked_id = u.id
		WHERE b.user_id = ? ORDER BY u.username COLLATE NOCASE ASC`, userID)
}

func (s *Store) listRelationshipUsers(query, userID string) ([]User, error) {
	rows, err := s.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateProfile(id, displayName, bio string) error {
	res, err := s.db.Exec(`UPDATE users SET display_name = ?, bio = ? WHERE id = ?`, displayName, bio, id)
	return oneRow(res, err)
}

func (s *Store) SetAvatar(id, avatar string) error {
	res, err := s.db.Exec(`UPDATE users SET avatar = ? WHERE id = ?`, avatar, id)
	return oneRow(res, err)
}

func (s *Store) UpdatePassword(id, hash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	if err := oneRow(res, err); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM password_resets WHERE user_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetAPIToken(id, hash string) error {
	res, err := s.db.Exec(`UPDATE users SET api_token_hash = ? WHERE id = ?`, nullIfEmpty(hash), id)
	return oneRow(res, err)
}

func (s *Store) SetUserDisabled(id string, disabled bool) error {
	res, err := s.db.Exec(`UPDATE users SET disabled = ? WHERE id = ?`, boolInt(disabled), id)
	return oneRow(res, err)
}

func (s *Store) SetUserRole(id, role string) error {
	switch role {
	case RoleUser, RoleModerator, RoleAdmin:
	default:
		return ErrInvalid
	}
	res, err := s.db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	return oneRow(res, err)
}

func oneRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateInvite(code, createdBy, expiresAt string) error {
	_, err := s.db.Exec(`INSERT INTO invites(code, created_by, created_at, expires_at)
		VALUES (?, ?, ?, ?)`, code, createdBy, Now(), nullIfEmpty(expiresAt))
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

const inviteSelect = `SELECT i.code, i.created_by, c.username, i.created_at,
	COALESCE(i.used_by, ''), COALESCE(u.username, ''), COALESCE(i.used_at, ''), COALESCE(i.expires_at, '')
	FROM invites i
	JOIN users c ON c.id = i.created_by
	LEFT JOIN users u ON u.id = i.used_by`

func scanInvite(row interface{ Scan(...any) error }) (*Invite, error) {
	var i Invite
	err := row.Scan(&i.Code, &i.CreatedBy, &i.CreatedByUsername, &i.CreatedAt,
		&i.UsedBy, &i.UsedByUsername, &i.UsedAt, &i.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (s *Store) GetInvite(code string) (*Invite, error) {
	return scanInvite(s.db.QueryRow(inviteSelect+` WHERE i.code = ?`, code))
}

func (s *Store) ListInvites() ([]Invite, error) {
	rows, err := s.db.Query(inviteSelect + ` ORDER BY i.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		i, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

func (s *Store) DeleteInvite(code string) error {
	res, err := s.db.Exec(`DELETE FROM invites WHERE code = ? AND used_by IS NULL`, code)
	return oneRow(res, err)
}

const maxNewsLength = 280

func cleanNewsBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > maxNewsLength {
		return "", ErrInvalid
	}
	return body, nil
}

func (s *Store) CreateNews(body string) (*NewsItem, error) {
	body, err := cleanNewsBody(body)
	if err != nil {
		return nil, err
	}
	item := &NewsItem{ID: NewID(), Body: body, CreatedAt: Now()}
	if _, err := s.db.Exec(`INSERT INTO news(id, body, created_at) VALUES (?, ?, ?)`,
		item.ID, item.Body, item.CreatedAt); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Store) GetNews(id string) (*NewsItem, error) {
	var item NewsItem
	err := s.db.QueryRow(`SELECT id, body, created_at FROM news WHERE id = ?`, id).
		Scan(&item.ID, &item.Body, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) ListNews() ([]NewsItem, error) {
	rows, err := s.db.Query(`SELECT id, body, created_at FROM news
		ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NewsItem
	for rows.Next() {
		var item NewsItem
		if err := rows.Scan(&item.ID, &item.Body, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpdateNews(id, body string) error {
	body, err := cleanNewsBody(body)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE news SET body = ? WHERE id = ?`, body, id)
	return oneRow(res, err)
}

func (s *Store) DeleteNews(id string) error {
	res, err := s.db.Exec(`DELETE FROM news WHERE id = ?`, id)
	return oneRow(res, err)
}

func (s *Store) ReplaceRemotePosts(instance string, posts []RemotePost) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM remote_posts WHERE instance = ?`, instance); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO remote_posts
		(instance, id, site_name, url, title, body_md, author_username, author_display_name,
		 author_url, channel, created_at, updated_at, comment_count, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	fetchedAt := Now()
	for i := range posts {
		p := &posts[i]
		if _, err := stmt.Exec(instance, p.ID, p.SiteName, p.URL, p.Title, p.BodyMD,
			p.AuthorUsername, p.AuthorDisplayName, p.AuthorURL, p.Channel,
			p.CreatedAt, p.UpdatedAt, p.CommentCount, fetchedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListRemotePosts(limit int) ([]RemotePost, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT instance, id, site_name, url, title, body_md,
		author_username, author_display_name, author_url, channel, created_at, updated_at,
		comment_count, fetched_at
		FROM remote_posts ORDER BY created_at DESC, instance ASC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RemotePost
	for rows.Next() {
		var p RemotePost
		if err := rows.Scan(&p.Instance, &p.ID, &p.SiteName, &p.URL, &p.Title, &p.BodyMD,
			&p.AuthorUsername, &p.AuthorDisplayName, &p.AuthorURL, &p.Channel,
			&p.CreatedAt, &p.UpdatedAt, &p.CommentCount, &p.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) RegisterWithInvite(code string, u *User) error {
	if u.ID == "" {
		u.ID = NewID()
	}
	if u.CreatedAt == "" {
		u.CreatedAt = Now()
	}
	if u.Role == "" {
		u.Role = "user"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var usedBy, expiresAt sql.NullString
	err = tx.QueryRow(`SELECT used_by, expires_at FROM invites WHERE code = ?`, code).Scan(&usedBy, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if usedBy.Valid && usedBy.String != "" {
		return ErrInviteUsed
	}
	if expiresAt.Valid && expiresAt.String != "" {
		t, err := time.Parse(time.RFC3339, expiresAt.String)
		if err == nil && time.Now().UTC().After(t) {
			return ErrInviteExpired
		}
	}
	if err := insertUser(tx, u); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE invites SET used_by = ?, used_at = ? WHERE code = ? AND used_by IS NULL`,
		u.ID, Now(), code)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return tx.Commit()
}

func (s *Store) CreateSession(sess *Session) error {
	_, err := s.db.Exec(`INSERT INTO sessions(id, user_id, csrf_token, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`, sess.ID, sess.UserID, sess.CSRFToken, sess.CreatedAt, sess.ExpiresAt)
	return err
}

func (s *Store) GetSession(id string) (*Session, error) {
	var sess Session
	err := s.db.QueryRow(`SELECT id, user_id, csrf_token, created_at, expires_at FROM sessions
		WHERE id = ? AND expires_at > ?`, id, Now()).Scan(
		&sess.ID, &sess.UserID, &sess.CSRFToken, &sess.CreatedAt, &sess.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteOtherSessions(userID, keepID string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND id <> ?`, userID, keepID)
	return err
}

func (s *Store) CleanupSessions() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, Now())
	return err
}

func (s *Store) ReplaceRecoveryCodes(userID string, codeHashes []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	now := Now()
	for _, hash := range codeHashes {
		if _, err := tx.Exec(`INSERT INTO recovery_codes(code_hash, user_id, created_at)
			VALUES (?, ?, ?)`, hash, userID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CountRecoveryCodes(userID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM recovery_codes WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

func (s *Store) ConsumeRecoveryCode(userID, codeHash, passwordHash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`DELETE FROM recovery_codes WHERE user_id = ? AND code_hash = ?`, userID, codeHash)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	res, err = tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	if err := oneRow(res, err); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreatePasswordReset(tokenHash, userID, expiresAt string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM password_resets WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO password_resets(token_hash, user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)`, tokenHash, userID, Now(), expiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetPasswordReset(tokenHash string) (*PasswordReset, error) {
	var pr PasswordReset
	err := s.db.QueryRow(`SELECT token_hash, user_id, created_at, expires_at
		FROM password_resets WHERE token_hash = ? AND expires_at > ?`, tokenHash, Now()).Scan(
		&pr.TokenHash, &pr.UserID, &pr.CreatedAt, &pr.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &pr, nil
}

func (s *Store) ConsumePasswordReset(tokenHash, passwordHash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var userID string
	err = tx.QueryRow(`SELECT user_id FROM password_resets
		WHERE token_hash = ? AND expires_at > ?`, tokenHash, Now()).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	if err := oneRow(res, err); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM password_resets WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CleanupPasswordResets() error {
	_, err := s.db.Exec(`DELETE FROM password_resets WHERE expires_at <= ?`, Now())
	return err
}

func (s *Store) CreatePost(p *Post) error {
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = Now()
	}
	if p.UpdatedAt == "" {
		p.UpdatedAt = p.CreatedAt
	}
	if p.Channel == "" {
		p.Channel = "general"
	}
	_, err := s.db.Exec(`INSERT INTO posts(id, author_id, title, body_md, body_html, channel, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, p.ID, p.AuthorID, p.Title, p.BodyMD, p.BodyHTML, p.Channel, p.CreatedAt, p.UpdatedAt)
	return err
}

const postSelect = `SELECT p.id, p.author_id, u.username, u.display_name, u.role, p.channel,
	p.title, p.body_md, p.body_html, p.created_at, p.updated_at,
	(SELECT COUNT(*) FROM comments c WHERE c.post_id = p.id) AS comment_count
	FROM posts p JOIN users u ON u.id = p.author_id`

const visiblePostAuthor = `p.author_id NOT IN (SELECT muted_id FROM user_mutes WHERE user_id = ?)
	AND p.author_id NOT IN (SELECT blocked_id FROM user_blocks WHERE user_id = ?)`

func scanPost(row interface{ Scan(...any) error }) (*Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.AuthorID, &p.AuthorUsername, &p.AuthorDisplayName, &p.AuthorRole, &p.Channel,
		&p.Title, &p.BodyMD, &p.BodyHTML, &p.CreatedAt, &p.UpdatedAt, &p.CommentCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) GetPost(id string) (*Post, error) {
	return scanPost(s.db.QueryRow(postSelect+` WHERE p.id = ?`, id))
}

func (s *Store) RandomPost() (*Post, error) {
	return scanPost(s.db.QueryRow(postSelect + ` WHERE p.channel <> 'nsfw' ORDER BY RANDOM() LIMIT 1`))
}

func (s *Store) ListPosts(limit, offset int) ([]Post, error) {
	return s.listPosts(` WHERE p.channel <> 'nsfw'`, limit, offset)
}

func (s *Store) ListPostsFor(viewerID string, limit, offset int) ([]Post, error) {
	if viewerID == "" {
		return s.ListPosts(limit, offset)
	}
	return s.listPosts(` WHERE p.channel <> 'nsfw' AND `+visiblePostAuthor, limit, offset, viewerID, viewerID)
}

func (s *Store) ListAllPosts(limit, offset int) ([]Post, error) {
	return s.listPosts(``, limit, offset)
}

func (s *Store) listPosts(where string, limit, offset int, args ...any) ([]Post, error) {
	queryArgs := append(args, limit, offset)
	rows, err := s.db.Query(postSelect+where+` ORDER BY p.created_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) ListUserPosts(userID string, limit, offset int) ([]Post, error) {
	rows, err := s.db.Query(postSelect+` WHERE p.author_id = ? AND p.channel <> 'nsfw' ORDER BY p.created_at DESC LIMIT ? OFFSET ?`,
		userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) ListBookmarks(userID string, includeNSFW bool, limit, offset int) ([]Post, error) {
	where := ` JOIN bookmarks b ON b.post_id = p.id
		WHERE b.user_id = ?
			AND p.author_id NOT IN (SELECT blocked_id FROM user_blocks WHERE user_id = ?)`
	args := []any{userID, userID}
	if !includeNSFW {
		where += ` AND p.channel <> 'nsfw'`
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(postSelect+where+`
		ORDER BY b.created_at DESC, p.created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) BookmarkPost(userID, postID string) error {
	if userID == "" || postID == "" {
		return ErrInvalid
	}
	_, err := s.db.Exec(`INSERT INTO bookmarks(user_id, post_id, created_at)
		VALUES (?, ?, ?) ON CONFLICT(user_id, post_id) DO NOTHING`, userID, postID, Now())
	return err
}

func (s *Store) UnbookmarkPost(userID, postID string) error {
	_, err := s.db.Exec(`DELETE FROM bookmarks WHERE user_id = ? AND post_id = ?`, userID, postID)
	return err
}

func (s *Store) IsBookmarked(userID, postID string) (bool, error) {
	if userID == "" || postID == "" {
		return false, nil
	}
	return s.relationshipExists(`SELECT 1 FROM bookmarks WHERE user_id = ? AND post_id = ?`, userID, postID)
}

func (s *Store) UpdatePost(id, title, bodyMD, bodyHTML, channel string) error {
	if channel == "" {
		channel = "general"
	}
	res, err := s.db.Exec(`UPDATE posts SET title = ?, body_md = ?, body_html = ?, channel = ?, updated_at = ? WHERE id = ?`,
		title, bodyMD, bodyHTML, channel, Now(), id)
	return oneRow(res, err)
}

func (s *Store) DeletePost(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM reactions WHERE target_type = 'comment'
		AND target_id IN (SELECT id FROM comments WHERE post_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM reactions WHERE target_type = 'post' AND target_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM posts WHERE id = ?`, id)
	if err := oneRow(res, err); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateComment(c *Comment) error {
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.CreatedAt == "" {
		c.CreatedAt = Now()
	}
	_, err := s.db.Exec(`INSERT INTO comments(id, post_id, parent_id, author_id, body_md, body_html, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, c.ID, c.PostID, nullIfEmpty(c.ParentID), c.AuthorID,
		c.BodyMD, c.BodyHTML, c.CreatedAt)
	return err
}

const commentSelect = `SELECT c.id, c.post_id, c.parent_id, c.author_id, u.username, u.display_name, u.role,
	c.body_md, c.body_html, c.created_at
	FROM comments c JOIN users u ON u.id = c.author_id`

const visibleCommentAuthor = `c.author_id NOT IN (SELECT muted_id FROM user_mutes WHERE user_id = ?)
	AND c.author_id NOT IN (SELECT blocked_id FROM user_blocks WHERE user_id = ?)`

func scanComment(row interface{ Scan(...any) error }) (*Comment, error) {
	var c Comment
	var parentID sql.NullString
	err := row.Scan(&c.ID, &c.PostID, &parentID, &c.AuthorID, &c.AuthorUsername, &c.AuthorDisplayName,
		&c.AuthorRole, &c.BodyMD, &c.BodyHTML, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.ParentID = parentID.String
	return &c, nil
}

func (s *Store) ListComments(postID string) ([]Comment, error) {
	return s.ListCommentsFor(postID, "")
}

func (s *Store) ListCommentsFor(postID, viewerID string) ([]Comment, error) {
	where := ` WHERE c.post_id = ?`
	args := []any{postID}
	if viewerID != "" {
		where += ` AND ` + visibleCommentAuthor
		args = append(args, viewerID, viewerID)
	}
	rows, err := s.db.Query(commentSelect+where+` ORDER BY c.created_at ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) CountNewComments(postIDs []string, viewerID, since string) (map[string]int, error) {
	out := make(map[string]int)
	if len(postIDs) == 0 || viewerID == "" {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(postIDs)), ",")
	args := make([]any, 0, len(postIDs)+2)
	for _, id := range postIDs {
		args = append(args, id)
	}
	where := ` WHERE post_id IN (` + placeholders + `) AND author_id <> ?`
	args = append(args, viewerID)
	if since != "" {
		where += ` AND created_at > ?`
		args = append(args, since)
	}
	rows, err := s.db.Query(`SELECT post_id, COUNT(*) FROM comments`+where+` GROUP BY post_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var postID string
		var count int
		if err := rows.Scan(&postID, &count); err != nil {
			return nil, err
		}
		out[postID] = count
	}
	return out, rows.Err()
}

func (s *Store) GetComment(id string) (*Comment, error) {
	return scanComment(s.db.QueryRow(commentSelect+` WHERE c.id = ?`, id))
}

func (s *Store) DeleteComment(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const subtree = `WITH RECURSIVE subtree(id) AS (
		SELECT id FROM comments WHERE id = ?
		UNION
		SELECT c.id FROM comments c JOIN subtree s ON c.parent_id = s.id
	)
	`
	if _, err := tx.Exec(subtree+
		`DELETE FROM reactions WHERE target_type = 'comment' AND target_id IN (SELECT id FROM subtree)`,
		id); err != nil {
		return err
	}
	res, err := tx.Exec(subtree+`DELETE FROM comments WHERE id IN (SELECT id FROM subtree)`, id)
	if err := oneRow(res, err); err != nil {
		return err
	}
	return tx.Commit()
}

const directMessageSelect = `SELECT m.id, m.sender_id, m.recipient_id,
	su.username, su.display_name, ru.username, ru.display_name,
	m.body_md, m.body_html, m.created_at, m.blocked, m.block_reason
	FROM direct_messages m
	JOIN users su ON su.id = m.sender_id
	JOIN users ru ON ru.id = m.recipient_id`

func scanDirectMessage(row interface{ Scan(...any) error }) (*DirectMessage, error) {
	var m DirectMessage
	var blocked int
	err := row.Scan(&m.ID, &m.SenderID, &m.RecipientID,
		&m.SenderUsername, &m.SenderDisplayName, &m.RecipientUsername, &m.RecipientDisplayName,
		&m.BodyMD, &m.BodyHTML, &m.CreatedAt, &blocked, &m.BlockReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Blocked = blocked != 0
	return &m, nil
}

func (s *Store) CreateDirectMessage(m *DirectMessage) error {
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.CreatedAt == "" {
		m.CreatedAt = Now()
	}
	_, err := s.db.Exec(`INSERT INTO direct_messages
		(id, sender_id, recipient_id, body_md, body_html, created_at, blocked, block_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.SenderID, m.RecipientID, m.BodyMD, m.BodyHTML, m.CreatedAt,
		boolInt(m.Blocked), m.BlockReason)
	return err
}

func (s *Store) GetDirectMessage(id string) (*DirectMessage, error) {
	return scanDirectMessage(s.db.QueryRow(directMessageSelect+` WHERE m.id = ?`, id))
}

func (s *Store) ListDirectMessages(userA, userB string) ([]DirectMessage, error) {
	const limit = 200
	rows, err := s.db.Query(directMessageSelect+`
		WHERE m.blocked = 0 AND
			((m.sender_id = ? AND m.recipient_id = ?) OR (m.sender_id = ? AND m.recipient_id = ?))
		ORDER BY m.created_at DESC LIMIT ?`, userA, userB, userB, userA, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DirectMessage
	for rows.Next() {
		m, err := scanDirectMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (s *Store) ListConversations(userID string) ([]Conversation, error) {
	rows, err := s.db.Query(`SELECT u.id, u.username, u.display_name, MAX(m.created_at) AS last_at
		FROM direct_messages m
		JOIN users u ON u.id = CASE WHEN m.sender_id = ? THEN m.recipient_id ELSE m.sender_id END
		WHERE m.blocked = 0 AND (m.sender_id = ? OR m.recipient_id = ?)
			AND u.id NOT IN (SELECT blocked_id FROM user_blocks WHERE user_id = ?)
			AND u.id NOT IN (SELECT user_id FROM user_blocks WHERE blocked_id = ?)
		GROUP BY u.id, u.username, u.display_name
		ORDER BY last_at DESC`, userID, userID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.UserID, &c.Username, &c.DisplayName, &c.LastAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ListBlockedDirectMessages(limit, offset int) ([]DirectMessage, error) {
	rows, err := s.db.Query(directMessageSelect+`
		WHERE m.blocked = 1 ORDER BY m.created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DirectMessage
	for rows.Next() {
		m, err := scanDirectMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteBlockedDirectMessage(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM reactions WHERE target_type = 'dm' AND target_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM direct_messages WHERE id = ? AND blocked = 1`, id)
	if err := oneRow(res, err); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ToggleReaction(userID, targetType, targetID, emoji string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM reactions
		WHERE user_id = ? AND target_type = ? AND target_id = ? AND emoji = ?`,
		userID, targetType, targetID, emoji)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := tx.Exec(`INSERT INTO reactions(id, user_id, target_type, target_id, emoji, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, NewID(), userID, targetType, targetID, emoji, Now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListReactions(targetType, targetID, viewerID string) ([]Reaction, error) {
	rows, err := s.db.Query(`SELECT emoji, COUNT(*), MAX(CASE WHEN user_id = ? THEN 1 ELSE 0 END)
		FROM reactions WHERE target_type = ? AND target_id = ?
		GROUP BY emoji ORDER BY MIN(created_at)`, viewerID, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reaction
	for rows.Next() {
		var r Reaction
		var reacted int
		if err := rows.Scan(&r.Emoji, &r.Count, &reacted); err != nil {
			return nil, err
		}
		r.Reacted = reacted != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SetPostTags(postID string, tags []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM post_tags WHERE post_id = ?`, postID); err != nil {
		return err
	}
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO post_tags(post_id, tag) VALUES (?, ?)`, postID, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListPostTags(postID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT tag FROM post_tags WHERE post_id = ? ORDER BY tag`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

func (s *Store) ListPostsByTag(tag string, limit, offset int) ([]Post, error) {
	return s.ListPostsByTagFor("", tag, limit, offset)
}

func (s *Store) ListPostsByTagFor(viewerID, tag string, limit, offset int) ([]Post, error) {
	where := ` JOIN post_tags t ON t.post_id = p.id
		WHERE t.tag = ? AND p.channel <> 'nsfw'`
	args := []any{tag}
	if viewerID != "" {
		where += ` AND ` + visiblePostAuthor
		args = append(args, viewerID, viewerID)
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(postSelect+where+` ORDER BY p.created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) ListTags(limit int) ([]TagCount, error) {
	rows, err := s.db.Query(`SELECT t.tag, COUNT(*) FROM post_tags t
		JOIN posts p ON p.id = t.post_id
		WHERE p.channel <> 'nsfw'
		GROUP BY t.tag ORDER BY COUNT(*) DESC, t.tag ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagCount
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) ListPostsByChannel(channel string, limit, offset int) ([]Post, error) {
	return s.ListPostsByChannelFor("", channel, limit, offset)
}

func (s *Store) ListPostsByChannelFor(viewerID, channel string, limit, offset int) ([]Post, error) {
	where := ` WHERE p.channel = ?`
	args := []any{channel}
	if viewerID != "" {
		where += ` AND ` + visiblePostAuthor
		args = append(args, viewerID, viewerID)
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(postSelect+where+` ORDER BY p.created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) CountPostsByChannel(channel string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE channel = ?`, channel).Scan(&n)
	return n, err
}

func visitorID(ip string) string {
	sum := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(sum[:])
}

func (s *Store) RecordVisitor(ip string) error {
	_, err := s.db.Exec(`INSERT INTO visitors(id, created_at) VALUES (?, ?)
		ON CONFLICT(id) DO NOTHING`, visitorID(ip), Now())
	return err
}

func (s *Store) IsRecordedVisitor(ip string) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM visitors WHERE id = ?`, visitorID(ip)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) LockdownEnabled() (bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'lockdown'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "1", nil
}

func (s *Store) SetLockdown(enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES ('lockdown', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, value)
	return err
}

func (s *Store) StartVisit(userID string) (string, error) {
	if userID == "" {
		return "", ErrInvalid
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var previous string
	err = tx.QueryRow(`SELECT last_seen_at FROM user_visits WHERE user_id = ?`, userID).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		previous = ""
	} else if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO user_visits(user_id, last_seen_at) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET last_seen_at = excluded.last_seen_at`, userID, Now()); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return previous, nil
}

func (s *Store) GetLastSeen(userID string) (string, error) {
	if userID == "" {
		return "", ErrInvalid
	}
	var at string
	err := s.db.QueryRow(`SELECT last_seen_at FROM user_visits WHERE user_id = ?`, userID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return at, nil
}

func (s *Store) Visit(ip string) (int64, error) {
	visitorID := visitorID(ip)

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var one int
	err = tx.QueryRow(`SELECT 1 FROM visitors WHERE id = ?`, visitorID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.Exec(`INSERT INTO visitors(id, created_at) VALUES (?, ?)`, visitorID, Now()); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES ('hits', '1')
		ON CONFLICT(key) DO UPDATE SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT)`); err != nil {
		return 0, err
	}

	var raw string
	err = tx.QueryRow(`SELECT value FROM meta WHERE key = 'hits'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		raw = "0"
	} else if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	n, _ := strconv.ParseInt(raw, 10, 64)
	return n, nil
}

func (s *Store) CountVisitors() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM visitors`).Scan(&n)
	return n, err
}

func (s *Store) GetHits() (int64, error) {
	var raw string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'hits'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, _ := strconv.ParseInt(raw, 10, 64)
	return n, nil
}

func (s *Store) Stats() (users, posts, comments int, err error) {
	err = s.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM users),
		(SELECT COUNT(*) FROM posts),
		(SELECT COUNT(*) FROM comments)`).Scan(&users, &posts, &comments)
	return
}

func (s *Store) CountActiveAdmins() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users
		WHERE role IN ('admin', 'owner') AND disabled = 0`).Scan(&n)
	return n, err
}

func (s *Store) CountActiveAdminsExcluding(id string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users
		WHERE role IN ('admin', 'owner') AND disabled = 0 AND id <> ?`, id).Scan(&n)
	return n, err
}
