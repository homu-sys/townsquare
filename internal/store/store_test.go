package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenMigratesOptionalEmail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE users (
		id             TEXT PRIMARY KEY,
		username       TEXT NOT NULL UNIQUE COLLATE NOCASE,
		display_name   TEXT NOT NULL,
		email          TEXT NOT NULL UNIQUE COLLATE NOCASE,
		password_hash  TEXT NOT NULL,
		bio            TEXT NOT NULL DEFAULT '',
		role           TEXT NOT NULL DEFAULT 'user',
		disabled       INTEGER NOT NULL DEFAULT 0,
		created_at     TEXT NOT NULL,
		api_token_hash TEXT UNIQUE
	)`); err != nil {
		t.Fatalf("create old users table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users
		(id, username, display_name, email, password_hash, created_at)
		VALUES ('u1', 'olduser', 'Old User', 'old@example.test', 'hash', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert old user: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatalf("create old meta table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO meta(key, value) VALUES ('hits', '999')`); err != nil {
		t.Fatalf("insert old hit count: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	defer st.Close()

	hits, err := st.GetHits()
	if err != nil {
		t.Fatalf("get migrated hits: %v", err)
	}
	if hits != 0 {
		t.Fatalf("inflated hit counter not reset: %d", hits)
	}

	old, err := st.GetUserByLogin("old@example.test")
	if err != nil {
		t.Fatalf("get old user: %v", err)
	}
	if old.Email != "old@example.test" {
		t.Fatalf("old email = %q", old.Email)
	}
	if old.Avatar != "" {
		t.Fatalf("old avatar = %q, want empty", old.Avatar)
	}

	for _, username := range []string{"first", "second"} {
		u := &User{Username: username, DisplayName: username, PasswordHash: "hash"}
		if err := st.CreateUser(u); err != nil {
			t.Fatalf("create user %q without email: %v", username, err)
		}
	}
	u, err := st.GetUserByLogin("second")
	if err != nil {
		t.Fatalf("get user by username: %v", err)
	}
	if u.Email != "" {
		t.Fatalf("optional email = %q", u.Email)
	}

	m := &DirectMessage{SenderID: old.ID, RecipientID: u.ID, BodyMD: "hi", BodyHTML: "<p>hi</p>"}
	if err := st.CreateDirectMessage(m); err != nil {
		t.Fatalf("create direct message after migration: %v", err)
	}
	messages, err := st.ListDirectMessages(old.ID, u.ID)
	if err != nil {
		t.Fatalf("list direct messages after migration: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("direct messages after migration = %+v", messages)
	}
}

func TestOpenMigratesPostChannel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-posts.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE users (
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
	);
	CREATE TABLE posts (
		id         TEXT PRIMARY KEY,
		author_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title      TEXT NOT NULL,
		body_md    TEXT NOT NULL,
		body_html  TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	INSERT INTO users(id, username, display_name, password_hash, created_at)
		VALUES ('u1', 'writer', 'Writer', 'hash', '2026-01-01T00:00:00Z');
	INSERT INTO posts(id, author_id, title, body_md, body_html, created_at, updated_at)
		VALUES ('p1', 'u1', 'Old post', 'body', '<p>body</p>', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');`); err != nil {
		t.Fatalf("create old posts table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	defer st.Close()

	p, err := st.GetPost("p1")
	if err != nil {
		t.Fatalf("get old post: %v", err)
	}
	if p.Channel != "general" {
		t.Fatalf("migrated post channel = %q, want general", p.Channel)
	}
	posts, err := st.ListPosts(10, 0)
	if err != nil || len(posts) != 1 {
		t.Fatalf("list migrated posts = %d, err = %v", len(posts), err)
	}
	if _, err := st.DB().Exec(`INSERT INTO posts
		(id, author_id, title, body_md, body_html, channel, created_at, updated_at)
		VALUES ('p2', 'u1', 'Hidden', 'body', '<p>body</p>', 'nsfw', '2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("insert nsfw post: %v", err)
	}
	posts, err = st.ListPosts(10, 0)
	if err != nil || len(posts) != 1 || posts[0].ID != "p1" {
		t.Fatalf("safe list after nsfw insert = %+v, err = %v", posts, err)
	}
	posts, err = st.ListAllPosts(10, 0)
	if err != nil || len(posts) != 2 {
		t.Fatalf("all posts after nsfw insert = %d, err = %v", len(posts), err)
	}
}

func TestNewsCRUD(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := st.CreateNews("   "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank news err = %v, want ErrInvalid", err)
	}
	if _, err := st.CreateNews(strings.Repeat("a", maxNewsLength+1)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("long news err = %v, want ErrInvalid", err)
	}

	first, err := st.CreateNews("  Town hall on Friday  ")
	if err != nil {
		t.Fatalf("create news: %v", err)
	}
	if first.ID == "" || first.Body != "Town hall on Friday" || first.CreatedAt == "" {
		t.Fatalf("unexpected news item: %+v", first)
	}
	if _, err := st.CreateNews("Server maintenance at midnight"); err != nil {
		t.Fatalf("create second news: %v", err)
	}

	list, err := st.ListNews()
	if err != nil || len(list) != 2 {
		t.Fatalf("list news = %+v, err = %v", list, err)
	}
	bodies := map[string]bool{}
	for _, item := range list {
		bodies[item.Body] = true
	}
	if !bodies["Town hall on Friday"] || !bodies["Server maintenance at midnight"] {
		t.Fatalf("unexpected news bodies: %+v", list)
	}

	if err := st.UpdateNews(first.ID, "Town hall moved to Saturday"); err != nil {
		t.Fatalf("update news: %v", err)
	}
	got, err := st.GetNews(first.ID)
	if err != nil || got.Body != "Town hall moved to Saturday" {
		t.Fatalf("get updated news = %+v, err = %v", got, err)
	}
	if err := st.UpdateNews("missing", "anything"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing news err = %v, want ErrNotFound", err)
	}

	if err := st.DeleteNews(first.ID); err != nil {
		t.Fatalf("delete news: %v", err)
	}
	if err := st.DeleteNews(first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing news err = %v, want ErrNotFound", err)
	}
	list, err = st.ListNews()
	if err != nil || len(list) != 1 || list[0].Body != "Server maintenance at midnight" {
		t.Fatalf("list after delete = %+v, err = %v", list, err)
	}
}

func TestOpenMigratesCommentParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-comments.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE users (
		id             TEXT PRIMARY KEY,
		username       TEXT NOT NULL UNIQUE COLLATE NOCASE,
		display_name   TEXT NOT NULL,
		email          TEXT UNIQUE COLLATE NOCASE,
		password_hash  TEXT NOT NULL,
		bio            TEXT NOT NULL DEFAULT '',
		avatar         TEXT NOT NULL DEFAULT '',
		role           TEXT NOT NULL DEFAULT 'user',
		disabled       INTEGER NOT NULL DEFAULT 0,
		created_at     TEXT NOT NULL,
		api_token_hash TEXT UNIQUE
	);
	CREATE TABLE posts (
		id         TEXT PRIMARY KEY,
		author_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title      TEXT NOT NULL,
		body_md    TEXT NOT NULL,
		body_html  TEXT NOT NULL,
		channel    TEXT NOT NULL DEFAULT 'general',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE comments (
		id         TEXT PRIMARY KEY,
		post_id    TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
		author_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		body_md    TEXT NOT NULL,
		body_html  TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	INSERT INTO users(id, username, display_name, password_hash, created_at)
		VALUES ('u1', 'writer', 'Writer', 'hash', '2026-01-01T00:00:00Z');
	INSERT INTO posts(id, author_id, title, body_md, body_html, created_at, updated_at)
		VALUES ('p1', 'u1', 'Old post', 'body', '<p>body</p>', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
	INSERT INTO comments(id, post_id, author_id, body_md, body_html, created_at)
		VALUES ('c1', 'p1', 'u1', 'old', '<p>old</p>', '2026-01-01T00:00:00Z');`); err != nil {
		t.Fatalf("create old comments table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	defer st.Close()

	c, err := st.GetComment("c1")
	if err != nil {
		t.Fatalf("get old comment: %v", err)
	}
	if c.ParentID != "" {
		t.Fatalf("old comment parent = %q, want empty", c.ParentID)
	}
	reply := &Comment{PostID: "p1", ParentID: "c1", AuthorID: "u1", BodyMD: "reply", BodyHTML: "<p>reply</p>"}
	if err := st.CreateComment(reply); err != nil {
		t.Fatalf("create reply: %v", err)
	}
	got, err := st.GetComment(reply.ID)
	if err != nil {
		t.Fatalf("get reply: %v", err)
	}
	if got.ParentID != "c1" {
		t.Fatalf("reply parent = %q, want c1", got.ParentID)
	}
}

func TestCommentThreads(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	u := &User{ID: NewID(), Username: "writer", DisplayName: "Writer", PasswordHash: "hash", CreatedAt: Now()}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	p := &Post{AuthorID: u.ID, Channel: "general", Title: "Thread", BodyMD: "body", BodyHTML: "<p>body</p>"}
	if err := st.CreatePost(p); err != nil {
		t.Fatalf("create post: %v", err)
	}

	root := &Comment{PostID: p.ID, AuthorID: u.ID, BodyMD: "root", BodyHTML: "<p>root</p>"}
	if err := st.CreateComment(root); err != nil {
		t.Fatalf("create root: %v", err)
	}
	reply := &Comment{PostID: p.ID, ParentID: root.ID, AuthorID: u.ID, BodyMD: "reply", BodyHTML: "<p>reply</p>"}
	if err := st.CreateComment(reply); err != nil {
		t.Fatalf("create reply: %v", err)
	}

	comments, err := st.ListComments(p.ID)
	if err != nil || len(comments) != 2 {
		t.Fatalf("comments = %d, err = %v", len(comments), err)
	}
	if comments[1].ParentID != root.ID {
		t.Fatalf("reply parent = %q, want %q", comments[1].ParentID, root.ID)
	}

	if _, err := st.DB().Exec(`INSERT INTO reactions(id, user_id, target_type, target_id, emoji, created_at)
		VALUES ('r1', ?, 'comment', ?, 'x', ?)`, u.ID, reply.ID, Now()); err != nil {
		t.Fatalf("insert reply reaction: %v", err)
	}
	if err := st.DeleteComment(root.ID); err != nil {
		t.Fatalf("delete root: %v", err)
	}
	comments, err = st.ListComments(p.ID)
	if err != nil || len(comments) != 0 {
		t.Fatalf("comments after deleting root = %d, err = %v", len(comments), err)
	}
	var reactions int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM reactions WHERE target_type = 'comment'`).Scan(&reactions); err != nil {
		t.Fatalf("count reactions: %v", err)
	}
	if reactions != 0 {
		t.Fatalf("reply reactions left behind: %d", reactions)
	}
}

func TestLockdownAndRecordedVisitors(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if on, err := st.LockdownEnabled(); err != nil || on {
		t.Fatalf("initial lockdown = %v, err = %v", on, err)
	}
	if seen, err := st.IsRecordedVisitor("198.51.100.1"); err != nil || seen {
		t.Fatalf("initial recorded = %v, err = %v", seen, err)
	}

	if err := st.RecordVisitor("198.51.100.1"); err != nil {
		t.Fatalf("record visitor: %v", err)
	}
	if err := st.RecordVisitor("198.51.100.1"); err != nil {
		t.Fatalf("record visitor again: %v", err)
	}
	if seen, err := st.IsRecordedVisitor("198.51.100.1"); err != nil || !seen {
		t.Fatalf("recorded = %v, err = %v", seen, err)
	}
	if seen, err := st.IsRecordedVisitor("198.51.100.2"); err != nil || seen {
		t.Fatalf("unknown recorded = %v, err = %v", seen, err)
	}

	if err := st.SetLockdown(true); err != nil {
		t.Fatalf("enable lockdown: %v", err)
	}
	if on, err := st.LockdownEnabled(); err != nil || !on {
		t.Fatalf("lockdown = %v, err = %v", on, err)
	}
	if err := st.SetLockdown(false); err != nil {
		t.Fatalf("disable lockdown: %v", err)
	}
	if on, err := st.LockdownEnabled(); err != nil || on {
		t.Fatalf("lockdown after disable = %v, err = %v", on, err)
	}
}

func TestOpenMigratesOwnerRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	admin := &User{Username: "admin", DisplayName: "Admin", PasswordHash: "hash", Role: RoleAdmin}
	if err := st.CreateUser(admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer st.Close()
	u, err := st.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if u.Role != RoleOwner {
		t.Fatalf("role = %q, want owner", u.Role)
	}
}
