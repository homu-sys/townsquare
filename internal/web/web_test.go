package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"townsquare/internal/auth"
	"townsquare/internal/config"
	"townsquare/internal/moderation"
	"townsquare/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		Addr:          ":0",
		DBPath:        filepath.Join(dir, "test.db"),
		BaseURL:       "http://example.test",
		SiteName:      "Test Town",
		Tagline:       "a test instance",
		SecureCookies: false,
		AllowSetup:    true,
		SessionTTL:    time.Hour,
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv, err := New(cfg, st)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv, st, cfg.BaseURL
}

func newTestClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func httpGet(t *testing.T, c *http.Client, target string) *http.Response {
	t.Helper()
	resp, err := c.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	return resp
}

func csrfFor(t *testing.T, c *http.Client, base string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	for _, cookie := range c.Jar.Cookies(u) {
		if cookie.Name == auth.CSRFCookie {
			return cookie.Value
		}
	}
	t.Fatalf("no CSRF cookie for %s", base)
	return ""
}

func httpPostForm(t *testing.T, c *http.Client, base, target string, values url.Values) *http.Response {
	t.Helper()
	values.Set("_csrf", csrfFor(t, c, base))
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	return resp
}

func loginClient(t *testing.T, base, username, password string) *http.Client {
	t.Helper()
	c := newTestClient(t)
	resp := httpGet(t, c, base+"/login")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = httpPostForm(t, c, base, base+"/login", url.Values{
		"login":    {username},
		"password": {password},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	return c
}

func httpPostMultipart(t *testing.T, c *http.Client, base, target string, fields map[string]string, fileField, fileName string, fileData []byte) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("_csrf", csrfFor(t, c, base)); err != nil {
		t.Fatalf("write csrf field: %v", err)
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if fileField != "" {
		fw, err := mw.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatalf("create file field: %v", err)
		}
		if _, err := fw.Write(fileData); err != nil {
			t.Fatalf("write file field: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, target, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	return resp
}

func bodyOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func wantStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d (body: %s)", resp.StatusCode, want, bodyOf(t, resp))
	}
}

func TestSetupCreatePostCommentAndAPI(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := newTestClient(t)

	resp := httpGet(t, c, ts.URL+"/")
	wantStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/setup" {
		t.Fatalf("home redirect = %q, want /setup", loc)
	}
	resp.Body.Close()

	resp = httpGet(t, c, ts.URL+"/setup")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/setup", url.Values{
		"username":     {"admin"},
		"display_name": {"The Admin"},
		"email":        {"admin@example.test"},
		"password":     {"correct horse battery"},
		"confirm":      {"correct horse battery"},
	})
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "recovery codes") {
		t.Fatalf("setup did not show recovery codes: %s", body)
	}

	u, err := st.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if u.Role != "admin" {
		t.Fatalf("role = %q, want admin", u.Role)
	}

	resp = httpGet(t, c, ts.URL+"/")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "Test Town") || !strings.Contains(body, "Random post") {
		t.Fatalf("home page missing site name or random button: %s", body)
	}

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/new", url.Values{
		"title": {"Hello, town"},
		"body":  {"This is **my** first post."},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	loc := resp.Header.Get("Location")
	resp.Body.Close()
	if !strings.HasPrefix(loc, "/posts/") {
		t.Fatalf("post redirect = %q", loc)
	}
	postID := path.Base(loc)

	resp = httpGet(t, c, ts.URL+loc)
	wantStatus(t, resp, http.StatusOK)
	body := bodyOf(t, resp)
	if !strings.Contains(body, "Hello, town") || !strings.Contains(body, "<strong>my</strong>") {
		t.Fatalf("post page missing content: %s", body)
	}

	resp = httpGet(t, c, ts.URL+"/random")
	wantStatus(t, resp, http.StatusSeeOther)
	if loc := resp.Header.Get("Location"); loc != "/posts/"+postID {
		t.Fatalf("random redirect = %q, want /posts/%s", loc, postID)
	}
	resp.Body.Close()

	resp = httpGet(t, c, ts.URL+"/feed.xml")
	wantStatus(t, resp, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); ct != "application/rss+xml; charset=utf-8" {
		t.Fatalf("feed content type = %q", ct)
	}
	if resp.Header.Get("Set-Cookie") != "" {
		t.Fatalf("feed should not set cookies")
	}
	feedBody := bodyOf(t, resp)
	if !strings.Contains(feedBody, "<title>Hello, town</title>") ||
		!strings.Contains(feedBody, "http://example.test/posts/"+postID) {
		t.Fatalf("feed missing post: %s", feedBody)
	}

	resp = httpGet(t, c, ts.URL+"/users/admin/feed.xml")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "Test Town :: The Admin") {
		t.Fatalf("user feed missing title: %s", body)
	}

	resp = httpGet(t, c, ts.URL+"/rss.xml")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = httpPostForm(t, c, ts.URL, ts.URL+loc+"/comments", url.Values{
		"body": {"Nice first post."},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	resp = httpGet(t, c, ts.URL+"/api/v1/posts/"+postID)
	wantStatus(t, resp, http.StatusOK)
	var out struct {
		Post apiPost `json:"post"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode post: %v", err)
	}
	resp.Body.Close()
	if out.Post.Title != "Hello, town" || out.Post.CommentCount != 1 || len(out.Post.Comments) != 1 {
		t.Fatalf("unexpected API post: %+v", out.Post)
	}
}

func TestAPIRequiresTokenAndCreatesPost(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	u := &store.User{
		ID:           store.NewID(),
		Username:     "api_user",
		DisplayName:  "API User",
		Email:        "api@example.test",
		PasswordHash: "not-used",
		Role:         "admin",
		CreatedAt:    store.Now(),
	}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	payload := `{"title":"From the API","body":"Hello from **curl**."}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/posts", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unauthenticated post: %v", err)
	}
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()

	token := "test-api-token"
	if err := st.SetAPIToken(u.ID, auth.HashToken(token)); err != nil {
		t.Fatalf("set token: %v", err)
	}
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/v1/posts", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated post: %v", err)
	}
	wantStatus(t, resp, http.StatusCreated)
	var created struct {
		Post apiPost `json:"post"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created post: %v", err)
	}
	resp.Body.Close()
	if created.Post.ID == "" || created.Post.Title != "From the API" {
		t.Fatalf("unexpected created post: %+v", created.Post)
	}
	if created.Post.Channel != "general" {
		t.Fatalf("API post channel = %q, want general", created.Post.Channel)
	}

	commentReq := func(payload string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/posts/"+created.Post.ID+"/comments", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("API comment: %v", err)
		}
		return resp
	}

	resp = commentReq(`{"body":"root comment"}`)
	wantStatus(t, resp, http.StatusCreated)
	var createdComment struct {
		Comment apiComment `json:"comment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createdComment); err != nil {
		t.Fatalf("decode created comment: %v", err)
	}
	resp.Body.Close()
	if createdComment.Comment.ID == "" || createdComment.Comment.ParentID != "" {
		t.Fatalf("unexpected created comment: %+v", createdComment.Comment)
	}

	resp = commentReq(`{"body":"reply","parent_id":"` + createdComment.Comment.ID + `"}`)
	wantStatus(t, resp, http.StatusCreated)
	var createdReply struct {
		Comment apiComment `json:"comment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createdReply); err != nil {
		t.Fatalf("decode created reply: %v", err)
	}
	resp.Body.Close()
	if createdReply.Comment.ParentID != createdComment.Comment.ID {
		t.Fatalf("API reply parent = %q, want %q", createdReply.Comment.ParentID, createdComment.Comment.ID)
	}

	resp = commentReq(`{"body":"orphan","parent_id":"missing"}`)
	wantStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()

	for _, tc := range []struct {
		name    string
		payload string
		status  int
		channel string
	}{
		{"nsfw", `{"title":"API nsfw","body":"hidden","channel":"nsfw"}`, http.StatusBadRequest, ""},
		{"invalid", `{"title":"API bad","body":"bad","channel":"nope"}`, http.StatusBadRequest, ""},
	} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/posts", strings.NewReader(tc.payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s API post: %v", tc.name, err)
		}
		wantStatus(t, resp, tc.status)
		if tc.status == http.StatusCreated {
			var out struct {
				Post apiPost `json:"post"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatalf("decode %s post: %v", tc.name, err)
			}
			if out.Post.Channel != tc.channel {
				t.Fatalf("%s post channel = %q, want %q", tc.name, out.Post.Channel, tc.channel)
			}
		}
		resp.Body.Close()
	}

	resp = httpGet(t, http.DefaultClient, ts.URL+"/api/v1/posts")
	wantStatus(t, resp, http.StatusOK)
	var list struct {
		Posts []apiPost `json:"posts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	resp.Body.Close()
	if len(list.Posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(list.Posts))
	}
}

func TestInviteRegistration(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := newTestClient(t)

	admin := &store.User{
		ID:           store.NewID(),
		Username:     "admin",
		DisplayName:  "Admin",
		Email:        "admin@example.test",
		PasswordHash: "not-used",
		Role:         "admin",
		CreatedAt:    store.Now(),
	}
	if err := st.CreateUser(admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := st.CreateInvite("abc123", admin.ID, ""); err != nil {
		t.Fatalf("create invite: %v", err)
	}

	resp := httpGet(t, c, ts.URL+"/register")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/register", url.Values{
		"invite":       {"abc123"},
		"username":     {"friend"},
		"display_name": {"A Friend"},
		"email":        {"friend@example.test"},
		"password":     {"another password"},
		"confirm":      {"another password"},
	})
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "recovery codes") {
		t.Fatalf("registration did not show recovery codes: %s", body)
	}

	friend, err := st.GetUserByUsername("friend")
	if err != nil {
		t.Fatalf("get friend: %v", err)
	}
	if friend.Role != "user" {
		t.Fatalf("friend role = %q, want user", friend.Role)
	}

	inv, err := st.GetInvite("abc123")
	if err != nil {
		t.Fatalf("get invite: %v", err)
	}
	if inv.UsedBy != friend.ID {
		t.Fatalf("invite used_by = %q, want %q", inv.UsedBy, friend.ID)
	}

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/register", url.Values{
		"invite":       {"abc123"},
		"username":     {"second"},
		"display_name": {"Second Person"},
		"email":        {"second@example.test"},
		"password":     {"another password"},
		"confirm":      {"another password"},
	})
	wantStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
}

func TestCSRFRejectsBadToken(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := newTestClient(t)

	admin := &store.User{
		ID:           store.NewID(),
		Username:     "admin",
		DisplayName:  "Admin",
		Email:        "admin@example.test",
		PasswordHash: "not-used",
		Role:         "admin",
		CreatedAt:    store.Now(),
	}
	if err := st.CreateUser(admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	resp := httpGet(t, c, ts.URL+"/login")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/login", strings.NewReader("login=admin&password=x&_csrf=wrong"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("post login: %v", err)
	}
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
}

func TestFavicon(t *testing.T) {
	srv, _, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := httpGet(t, http.DefaultClient, ts.URL+"/favicon.ico")
	wantStatus(t, resp, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); ct != "image/x-icon" {
		t.Fatalf("favicon content type = %q", ct)
	}
	if body := bodyOf(t, resp); !strings.HasPrefix(body, "\x00\x00\x01\x00") {
		t.Fatalf("favicon is not an ICO file")
	}

	resp = httpGet(t, http.DefaultClient, ts.URL+"/static/favicon.svg")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "<svg") {
		t.Fatalf("favicon.svg is not an SVG file")
	}
}

func extractMatch(t *testing.T, re *regexp.Regexp, body string) string {
	t.Helper()
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no match for %s in body: %s", re, body)
	}
	if len(m) > 1 {
		return m[1]
	}
	return m[0]
}

func setupAdmin(t *testing.T, c *http.Client, base string) string {
	t.Helper()
	resp := httpGet(t, c, base+"/setup")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = httpPostForm(t, c, base, base+"/setup", url.Values{
		"username":     {"admin"},
		"display_name": {"The Admin"},
		"email":        {"admin@example.test"},
		"password":     {"correct horse battery"},
		"confirm":      {"correct horse battery"},
	})
	wantStatus(t, resp, http.StatusOK)
	return bodyOf(t, resp)
}

func TestRecoveryCodeResetsPassword(t *testing.T) {
	srv, _, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	admin := newTestClient(t)

	body := setupAdmin(t, admin, ts.URL)
	code := extractMatch(t, regexp.MustCompile(`[A-Z2-7]{4}(?:-[A-Z2-7]{4}){3}`), body)

	c := newTestClient(t)
	resp := httpGet(t, c, ts.URL+"/recover")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/recover", url.Values{
		"login":    {"admin"},
		"code":     {strings.ToLower(code)},
		"password": {"a brand new password"},
		"confirm":  {"a brand new password"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	login := newTestClient(t)
	resp = httpGet(t, login, ts.URL+"/login")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = httpPostForm(t, login, ts.URL, ts.URL+"/login", url.Values{
		"login":    {"admin"},
		"password": {"a brand new password"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	retry := newTestClient(t)
	resp = httpGet(t, retry, ts.URL+"/recover")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = httpPostForm(t, retry, ts.URL, ts.URL+"/recover", url.Values{
		"login":    {"admin"},
		"code":     {code},
		"password": {"another password"},
		"confirm":  {"another password"},
	})
	wantStatus(t, resp, http.StatusUnauthorized)
	resp.Body.Close()
}

func TestAuthRateLimit(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := newTestClient(t)

	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u := &store.User{
		ID:           store.NewID(),
		Username:     "admin",
		DisplayName:  "Admin",
		Email:        "admin@example.test",
		PasswordHash: hash,
		Role:         "admin",
		CreatedAt:    store.Now(),
	}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	resp := httpGet(t, c, ts.URL+"/login")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	for i := 0; i < 10; i++ {
		resp = httpPostForm(t, c, ts.URL, ts.URL+"/login", url.Values{
			"login":    {"admin"},
			"password": {"wrong password"},
		})
		wantStatus(t, resp, http.StatusUnauthorized)
		resp.Body.Close()
	}
	resp = httpPostForm(t, c, ts.URL, ts.URL+"/login", url.Values{
		"login":    {"admin"},
		"password": {"wrong password"},
	})
	wantStatus(t, resp, http.StatusTooManyRequests)
	if resp.Header.Get("Retry-After") == "" {
		t.Fatalf("missing Retry-After header")
	}
	resp.Body.Close()
}

func TestSecurityHeaders(t *testing.T) {
	srv, _, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := httpGet(t, http.DefaultClient, ts.URL+"/about")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	if got := resp.Header.Get("Permissions-Policy"); !strings.Contains(got, "geolocation=()") {
		t.Fatalf("missing permissions policy: %q", got)
	}
	if got := resp.Header.Get("Cross-Origin-Opener-Policy"); got != "same-origin" {
		t.Fatalf("COOP = %q", got)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "object-src 'none'") {
		t.Fatalf("weak CSP: %q", csp)
	}
	if strings.Contains(csp, "http: https:") {
		t.Fatalf("remote images should be blocked by default: %q", csp)
	}
}

func TestClientIPUsesRightmostForwardedFor(t *testing.T) {
	s := &Server{cfg: config.Config{TrustProxy: true}}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.2")
	if got := s.clientIP(r); got != "198.51.100.2" {
		t.Fatalf("clientIP = %q, want rightmost proxy-provided address", got)
	}
}

func TestOptionalEmail(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	c := newTestClient(t)
	resp := httpGet(t, c, ts.URL+"/setup")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/setup", url.Values{
		"username":     {"noemail"},
		"display_name": {"No Email"},
		"password":     {"correct horse battery"},
		"confirm":      {"correct horse battery"},
	})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	u, err := st.GetUserByUsername("noemail")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u.Email != "" {
		t.Fatalf("email = %q, want empty", u.Email)
	}

	second := &store.User{Username: "second", DisplayName: "Second", PasswordHash: "hash"}
	if err := st.CreateUser(second); err != nil {
		t.Fatalf("create second user without email: %v", err)
	}

	c2 := newTestClient(t)
	resp = httpGet(t, c2, ts.URL+"/login")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = httpPostForm(t, c2, ts.URL, ts.URL+"/login", url.Values{
		"login":    {"noemail"},
		"password": {"correct horse battery"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
}

func TestRegistrationOpenOption(t *testing.T) {
	srv, st, _ := newTestServer(t)
	admin := &store.User{Username: "admin", DisplayName: "Admin", PasswordHash: "hash", Role: "admin"}
	if err := st.CreateUser(admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := newTestClient(t)

	resp := httpGet(t, c, ts.URL+"/register")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "Registration is invite-only") {
		t.Fatalf("register page missing invite-only notice")
	}

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/register", url.Values{
		"username": {"closed"},
		"password": {"correct horse battery"},
		"confirm":  {"correct horse battery"},
	})
	wantStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()

	srv.cfg.OpenRegistration = true
	resp = httpGet(t, c, ts.URL+"/register")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "Registration is open") {
		t.Fatalf("register page missing open notice")
	}

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/register", url.Values{
		"username": {"openuser"},
		"password": {"correct horse battery"},
		"confirm":  {"correct horse battery"},
	})
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	u, err := st.GetUserByUsername("openuser")
	if err != nil {
		t.Fatalf("get open-registration user: %v", err)
	}
	if u.Role != "user" {
		t.Fatalf("role = %q, want user", u.Role)
	}
}

func TestDirectMessagesAndModeration(t *testing.T) {
	srv, st, _ := newTestServer(t)
	srv.cfg.DMBlocklist = []string{".onion", "magnet:"}
	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	alice := &store.User{Username: "alice", DisplayName: "Alice", PasswordHash: hash, Role: "admin"}
	bob := &store.User{Username: "bob", DisplayName: "Bob", PasswordHash: hash, Role: "user"}
	for _, u := range []*store.User{alice, bob} {
		if err := st.CreateUser(u); err != nil {
			t.Fatalf("create %s: %v", u.Username, err)
		}
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	c := newTestClient(t)
	resp := httpGet(t, c, ts.URL+"/login")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = httpPostForm(t, c, ts.URL, ts.URL+"/login", url.Values{
		"login":    {"alice"},
		"password": {"correct horse battery"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	resp = httpGet(t, c, ts.URL+"/messages/bob")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/messages/bob", url.Values{"body": {"Hello Bob."}})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	resp = httpGet(t, c, ts.URL+"/messages/bob")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "Hello Bob.") {
		t.Fatalf("thread missing delivered message: %s", body)
	}

	resp = httpGet(t, c, ts.URL+"/messages")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "bob") {
		t.Fatalf("inbox missing conversation: %s", body)
	}

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/messages/bob", url.Values{
		"body": {"Visit abc.onion or magnet:?xt=urn:btih:123 now."},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	resp = httpGet(t, c, ts.URL+"/messages/bob")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); strings.Contains(body, "abc.onion") {
		t.Fatalf("blocked message was delivered: %s", body)
	}

	blocked, err := st.ListBlockedDirectMessages(10, 0)
	if err != nil {
		t.Fatalf("list blocked messages: %v", err)
	}
	if len(blocked) != 1 || blocked[0].BlockReason != ".onion, magnet:" {
		t.Fatalf("blocked messages = %+v", blocked)
	}

	resp = httpGet(t, c, ts.URL+"/admin/dms")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "abc.onion") || !strings.Contains(body, ".onion, magnet:") {
		t.Fatalf("admin queue missing blocked message: %s", body)
	}

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/admin/dms/"+blocked[0].ID+"/delete", url.Values{})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	blocked, err = st.ListBlockedDirectMessages(10, 0)
	if err != nil {
		t.Fatalf("list blocked messages after delete: %v", err)
	}
	if len(blocked) != 0 {
		t.Fatalf("blocked messages after delete = %+v", blocked)
	}
}

func TestAvatarUploadIsSanitized(t *testing.T) {
	srv, st, _ := newTestServer(t)
	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u := &store.User{Username: "alice", DisplayName: "Alice", PasswordHash: hash, Role: "user"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := loginClient(t, ts.URL, "alice", "correct horse battery")

	resp := httpPostMultipart(t, c, ts.URL, ts.URL+"/settings/avatar",
		map[string]string{"action": "upload"}, "avatar", "bad.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	fresh, err := st.GetUserByID(u.ID)
	if err != nil {
		t.Fatalf("get user after bad upload: %v", err)
	}
	if fresh.Avatar != "" {
		t.Fatal("malicious image was accepted")
	}

	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	img.SetRGBA(1, 0, color.RGBA{G: 255, A: 255})
	img.SetRGBA(2, 0, color.RGBA{B: 255, A: 255})
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	resp = httpPostMultipart(t, c, ts.URL, ts.URL+"/settings/avatar",
		map[string]string{"action": "upload"}, "avatar", "me.png", pngData.Bytes())
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	fresh, err = st.GetUserByID(u.ID)
	if err != nil {
		t.Fatalf("get user after upload: %v", err)
	}
	if fresh.Avatar == "" {
		t.Fatal("uploaded avatar was not stored")
	}
	raw, err := base64.StdEncoding.DecodeString(fresh.Avatar)
	if err != nil {
		t.Fatalf("avatar is not base64: %v", err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode stored avatar: %v", err)
	}
	if format != "png" {
		t.Fatalf("stored avatar format = %q, want png", format)
	}
	if b := decoded.Bounds(); b.Dx() != 128 || b.Dy() != 128 {
		t.Fatalf("stored avatar size = %dx%d, want 128x128", b.Dx(), b.Dy())
	}

	resp = httpGet(t, c, ts.URL+"/users/alice")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "data:image/png;base64,"+fresh.Avatar) {
		t.Fatalf("profile does not show the avatar: %s", body)
	}

	resp = httpPostMultipart(t, c, ts.URL, ts.URL+"/settings/avatar",
		map[string]string{"action": "remove"}, "", "", nil)
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	fresh, err = st.GetUserByID(u.ID)
	if err != nil {
		t.Fatalf("get user after removal: %v", err)
	}
	if fresh.Avatar != "" {
		t.Fatal("avatar was not removed")
	}
}

func TestHashtagsAndReactions(t *testing.T) {
	srv, st, _ := newTestServer(t)
	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	alice := &store.User{Username: "alice", DisplayName: "Alice", PasswordHash: hash, Role: "admin"}
	bob := &store.User{Username: "bob", DisplayName: "Bob", PasswordHash: hash, Role: "user"}
	charlie := &store.User{Username: "charlie", DisplayName: "Charlie", PasswordHash: hash, Role: "user"}
	for _, u := range []*store.User{alice, bob, charlie} {
		if err := st.CreateUser(u); err != nil {
			t.Fatalf("create %s: %v", u.Username, err)
		}
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	aliceClient := loginClient(t, ts.URL, "alice", "correct horse battery")

	resp := httpPostForm(t, aliceClient, ts.URL, ts.URL+"/new", url.Values{
		"title": {"Hello world"},
		"body":  {"A #Go post about #web-dev."},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	location := resp.Header.Get("Location")
	resp.Body.Close()
	postID := strings.TrimPrefix(location, "/posts/")
	if postID == "" || postID == location {
		t.Fatalf("new post location = %q", location)
	}
	tags, err := st.ListPostTags(postID)
	if err != nil {
		t.Fatalf("list post tags: %v", err)
	}
	if len(tags) != 2 || tags[0] != "go" || tags[1] != "web-dev" {
		t.Fatalf("post tags = %#v", tags)
	}

	resp = httpGet(t, aliceClient, ts.URL+"/tags")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "#go") || !strings.Contains(body, "#web-dev") {
		t.Fatalf("hashtag index missing tags: %s", body)
	}
	resp = httpGet(t, aliceClient, ts.URL+"/tags/go")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "Hello world") {
		t.Fatalf("hashtag page missing post: %s", body)
	}
	resp = httpGet(t, aliceClient, ts.URL+"/posts/"+postID)
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, `href="/tags/go"`) {
		t.Fatalf("post page missing hashtag link: %s", body)
	}

	reaction := url.Values{
		"target_type": {"post"},
		"target_id":   {postID},
		"emoji":       {"👍"},
		"next":        {"/posts/" + postID},
	}
	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/reactions", reaction)
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	items, err := st.ListReactions("post", postID, alice.ID)
	if err != nil {
		t.Fatalf("list post reactions: %v", err)
	}
	if len(items) != 1 || items[0].Emoji != "👍" || items[0].Count != 1 || !items[0].Reacted {
		t.Fatalf("post reactions = %+v", items)
	}
	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/reactions", reaction)
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	items, err = st.ListReactions("post", postID, alice.ID)
	if err != nil {
		t.Fatalf("list post reactions after toggle: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("post reactions after toggle = %+v", items)
	}

	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/posts/"+postID+"/comments", url.Values{
		"body": {"Nice #Go post."},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	comments, err := st.ListComments(postID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments after create = %d, err = %v", len(comments), err)
	}
	commentID := comments[0].ID
	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/reactions", url.Values{
		"target_type": {"comment"},
		"target_id":   {commentID},
		"emoji":       {"🎉"},
		"next":        {"/posts/" + postID + "#comment-" + commentID},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	items, err = st.ListReactions("comment", commentID, alice.ID)
	if err != nil || len(items) != 1 || items[0].Count != 1 {
		t.Fatalf("comment reactions = %+v, err = %v", items, err)
	}

	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/messages/bob", url.Values{"body": {"Hello Bob"}})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	messages, err := st.ListDirectMessages(alice.ID, bob.ID)
	if err != nil || len(messages) != 1 {
		t.Fatalf("direct messages = %d, err = %v", len(messages), err)
	}
	dmID := messages[0].ID
	bobClient := loginClient(t, ts.URL, "bob", "correct horse battery")
	resp = httpPostForm(t, bobClient, ts.URL, ts.URL+"/reactions", url.Values{
		"target_type": {"dm"},
		"target_id":   {dmID},
		"emoji":       {"❤️"},
		"next":        {"/messages/alice"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	items, err = st.ListReactions("dm", dmID, bob.ID)
	if err != nil || len(items) != 1 || items[0].Count != 1 || !items[0].Reacted {
		t.Fatalf("dm reactions = %+v, err = %v", items, err)
	}

	charlieClient := loginClient(t, ts.URL, "charlie", "correct horse battery")
	resp = httpPostForm(t, charlieClient, ts.URL, ts.URL+"/reactions", url.Values{
		"target_type": {"dm"},
		"target_id":   {dmID},
		"emoji":       {"😂"},
		"next":        {"/messages/alice"},
	})
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()

	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/posts/"+postID+"/edit", url.Values{
		"title": {"Hello world"},
		"body":  {"Now #Rust and #linux."},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	posts, err := st.ListPostsByTag("go", 10, 0)
	if err != nil || len(posts) != 0 {
		t.Fatalf("posts tagged go after edit = %d, err = %v", len(posts), err)
	}
	posts, err = st.ListPostsByTag("rust", 10, 0)
	if err != nil || len(posts) != 1 {
		t.Fatalf("posts tagged rust after edit = %d, err = %v", len(posts), err)
	}
}

func TestCommentReplies(t *testing.T) {
	srv, st, _ := newTestServer(t)
	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	alice := &store.User{Username: "alice", DisplayName: "Alice", PasswordHash: hash, Role: "admin"}
	bob := &store.User{Username: "bob", DisplayName: "Bob", PasswordHash: hash, Role: "user"}
	for _, u := range []*store.User{alice, bob} {
		if err := st.CreateUser(u); err != nil {
			t.Fatalf("create %s: %v", u.Username, err)
		}
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	aliceClient := loginClient(t, ts.URL, "alice", "correct horse battery")

	resp := httpPostForm(t, aliceClient, ts.URL, ts.URL+"/new", url.Values{
		"title": {"Thread test"},
		"body":  {"A post for replies."},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	postID := strings.TrimPrefix(resp.Header.Get("Location"), "/posts/")
	resp.Body.Close()

	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/posts/"+postID+"/comments", url.Values{
		"body": {"Root comment."},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	comments, err := st.ListComments(postID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments after root = %d, err = %v", len(comments), err)
	}
	rootID := comments[0].ID

	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/posts/"+postID+"/comments", url.Values{
		"body":      {"First reply."},
		"parent_id": {rootID},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	location := resp.Header.Get("Location")
	resp.Body.Close()
	comments, err = st.ListComments(postID)
	if err != nil || len(comments) != 2 {
		t.Fatalf("comments after reply = %d, err = %v", len(comments), err)
	}
	var reply store.Comment
	for _, c := range comments {
		if c.ParentID == rootID {
			reply = c
		}
	}
	if reply.ID == "" {
		t.Fatalf("reply has no parent: %+v", comments)
	}
	if !strings.HasSuffix(location, "#comment-"+reply.ID) {
		t.Fatalf("reply redirect = %q, want anchor for %s", location, reply.ID)
	}

	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/posts/"+postID+"/comments", url.Values{
		"body":      {"Nested reply."},
		"parent_id": {reply.ID},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	comments, err = st.ListComments(postID)
	if err != nil || len(comments) != 3 {
		t.Fatalf("comments after nested reply = %d, err = %v", len(comments), err)
	}

	resp = httpGet(t, aliceClient, ts.URL+"/posts/"+postID)
	wantStatus(t, resp, http.StatusOK)
	body := bodyOf(t, resp)
	for _, want := range []string{
		"in reply to",
		`style="margin-left: 1em"`,
		`style="margin-left: 2em"`,
		"?reply=" + rootID,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("post page missing %q: %s", want, body)
		}
	}

	resp = httpGet(t, aliceClient, ts.URL+"/posts/"+postID+"?reply="+rootID)
	wantStatus(t, resp, http.StatusOK)
	body = bodyOf(t, resp)
	for _, want := range []string{
		`name="parent_id" value="` + rootID + `"`,
		"Replying to",
		"Post reply",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("reply form missing %q: %s", want, body)
		}
	}

	other := &store.Post{AuthorID: alice.ID, Channel: "general", Title: "Other", BodyMD: "other", BodyHTML: "<p>other</p>"}
	if err := st.CreatePost(other); err != nil {
		t.Fatalf("create other post: %v", err)
	}
	otherComment := &store.Comment{PostID: other.ID, AuthorID: alice.ID, BodyMD: "elsewhere", BodyHTML: "<p>elsewhere</p>"}
	if err := st.CreateComment(otherComment); err != nil {
		t.Fatalf("create other comment: %v", err)
	}
	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/posts/"+postID+"/comments", url.Values{
		"body":      {"Wrong thread."},
		"parent_id": {otherComment.ID},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	comments, err = st.ListComments(postID)
	if err != nil || len(comments) != 3 {
		t.Fatalf("cross-post reply created a comment: %d, err = %v", len(comments), err)
	}

	resp = httpPostForm(t, aliceClient, ts.URL, ts.URL+"/comments/"+rootID+"/delete", url.Values{})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	comments, err = st.ListComments(postID)
	if err != nil || len(comments) != 0 {
		t.Fatalf("comments after deleting root = %d, err = %v", len(comments), err)
	}
}

func TestVisitorCounterCountsUniqueIPs(t *testing.T) {
	srv, st, _ := newTestServer(t)
	srv.cfg.TrustProxy = true
	if err := st.CreateUser(&store.User{
		Username:     "admin",
		DisplayName:  "Admin",
		PasswordHash: "not-used",
		Role:         "admin",
	}); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	c := newTestClient(t)
	getAs := func(ip string) string {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("X-Forwarded-For", ip)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("GET as %s: %v", ip, err)
		}
		wantStatus(t, resp, http.StatusOK)
		return bodyOf(t, resp)
	}

	for range 2 {
		if body := getAs("198.51.100.1"); !strings.Contains(body, `You are visitor <span class="digits">1</span>`) {
			t.Fatalf("page refresh from same IP changed visitor number: %s", body)
		}
	}
	hits, err := st.GetHits()
	if err != nil {
		t.Fatalf("get hits: %v", err)
	}
	if hits != 1 {
		t.Fatalf("hits after same-IP refresh = %d, want 1", hits)
	}

	if body := getAs("203.0.113.9"); !strings.Contains(body, `You are visitor <span class="digits">2</span>`) {
		t.Fatalf("second IP not counted: %s", body)
	}
}

func TestOpenGraphMetadata(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	u := &store.User{
		ID:           store.NewID(),
		Username:     "writer",
		DisplayName:  "A Writer",
		Email:        "writer@example.test",
		PasswordHash: "not-used",
		Role:         "user",
		Bio:          "I write things.",
		CreatedAt:    store.Now(),
	}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	p := &store.Post{
		AuthorID: u.ID,
		Title:    "A post about towns",
		BodyMD:   "Hello there.",
		BodyHTML: "<p>Hello <strong>there</strong>.</p>",
	}
	if err := st.CreatePost(p); err != nil {
		t.Fatalf("create post: %v", err)
	}

	resp := httpGet(t, http.DefaultClient, ts.URL+"/posts/"+p.ID)
	wantStatus(t, resp, http.StatusOK)
	body := bodyOf(t, resp)
	for _, want := range []string{
		`<meta property="og:title" content="A post about towns">`,
		`<meta property="og:description" content="Hello there.">`,
		`<meta property="og:type" content="article">`,
		`<meta property="og:image" content="http://example.test/static/og.png">`,
		`<meta property="og:url" content="http://example.test/posts/` + p.ID + `">`,
		`<meta name="twitter:card" content="summary_large_image">`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("post page missing %q", want)
		}
	}

	resp = httpGet(t, http.DefaultClient, ts.URL+"/users/writer")
	wantStatus(t, resp, http.StatusOK)
	body = bodyOf(t, resp)
	if !strings.Contains(body, `<meta property="og:type" content="profile">`) ||
		!strings.Contains(body, `<meta property="og:description" content="I write things.">`) {
		t.Fatalf("profile page missing OG profile data")
	}

	resp = httpGet(t, http.DefaultClient, ts.URL+"/static/og.png")
	wantStatus(t, resp, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Fatalf("og.png content type = %q", ct)
	}
	if body := bodyOf(t, resp); !strings.HasPrefix(body, "\x89PNG") {
		t.Fatalf("og.png is not a PNG file")
	}
}

func TestAdminPasswordResetLink(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	admin := newTestClient(t)
	setupAdmin(t, admin, ts.URL)

	hash, err := auth.HashPassword("old password here")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	target := &store.User{
		ID:           store.NewID(),
		Username:     "target",
		DisplayName:  "Target User",
		Email:        "target@example.test",
		PasswordHash: hash,
		Role:         "user",
		CreatedAt:    store.Now(),
	}
	if err := st.CreateUser(target); err != nil {
		t.Fatalf("create target: %v", err)
	}

	resp := httpPostForm(t, admin, ts.URL, ts.URL+"/admin/users/"+target.ID+"/reset", url.Values{})
	wantStatus(t, resp, http.StatusOK)
	body := bodyOf(t, resp)
	token := extractMatch(t, regexp.MustCompile(`/reset\?token=([A-Za-z0-9_-]+)`), body)

	resetClient := newTestClient(t)
	resp = httpGet(t, resetClient, ts.URL+"/reset?token="+token)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = httpPostForm(t, resetClient, ts.URL, ts.URL+"/reset", url.Values{
		"token":    {token},
		"password": {"reset password"},
		"confirm":  {"reset password"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	login := newTestClient(t)
	resp = httpGet(t, login, ts.URL+"/login")
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = httpPostForm(t, login, ts.URL, ts.URL+"/login", url.Values{
		"login":    {"target"},
		"password": {"reset password"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	resp = httpGet(t, resetClient, ts.URL+"/reset?token="+token)
	wantStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()
}

type blockingAvatarChecker struct{}

func (blockingAvatarChecker) Check(image.Image) (moderation.Verdict, error) {
	return moderation.Verdict{NSFL: 0.01, NSFW: 0.99, SFW: 0}, nil
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func TestAvatarModerationRejectsUnsafeImage(t *testing.T) {
	srv, st, _ := newTestServer(t)
	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u := &store.User{Username: "alice", DisplayName: "Alice", PasswordHash: hash, Role: "user"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := loginClient(t, ts.URL, "alice", "correct horse battery")

	srv.avatarChecker = blockingAvatarChecker{}
	resp := httpPostMultipart(t, c, ts.URL, ts.URL+"/settings/avatar",
		map[string]string{"action": "upload"}, "avatar", "me.png", testPNG(t))
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	fresh, err := st.GetUserByID(u.ID)
	if err != nil {
		t.Fatalf("get user after blocked upload: %v", err)
	}
	if fresh.Avatar != "" {
		t.Fatal("avatar flagged by moderation was stored")
	}

	srv.avatarChecker = nil
	resp = httpPostMultipart(t, c, ts.URL, ts.URL+"/settings/avatar",
		map[string]string{"action": "upload"}, "avatar", "me.png", testPNG(t))
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	fresh, err = st.GetUserByID(u.ID)
	if err != nil {
		t.Fatalf("get user after allowed upload: %v", err)
	}
	if fresh.Avatar == "" {
		t.Fatal("safe avatar was not stored")
	}
}

func TestSuggestionAndShameChannels(t *testing.T) {
	srv, st, _ := newTestServer(t)
	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	alice := &store.User{Username: "alice", DisplayName: "Alice", PasswordHash: hash, Role: "user"}
	if err := st.CreateUser(alice); err != nil {
		t.Fatalf("create user: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := loginClient(t, ts.URL, "alice", "correct horse battery")

	for _, tc := range []struct {
		slug  string
		title string
	}{
		{"suggestions", "A good idea"},
		{"hall-of-shame", "A regrettable post"},
	} {
		resp := httpPostForm(t, c, ts.URL, ts.URL+"/new", url.Values{
			"title":   {tc.title},
			"body":    {"channel test"},
			"channel": {tc.slug},
		})
		wantStatus(t, resp, http.StatusSeeOther)
		resp.Body.Close()

		resp = httpGet(t, c, ts.URL+"/c/"+tc.slug)
		wantStatus(t, resp, http.StatusOK)
		if body := bodyOf(t, resp); !strings.Contains(body, tc.title) {
			t.Fatalf("channel %s missing %q: %s", tc.slug, tc.title, body)
		}
	}

	resp := httpGet(t, c, ts.URL+"/channels")
	wantStatus(t, resp, http.StatusOK)
	body := bodyOf(t, resp)
	for _, want := range []string{"Suggestions", "Hall of Shame", "/c/suggestions", "/c/hall-of-shame"} {
		if !strings.Contains(body, want) {
			t.Fatalf("channels page missing %q: %s", want, body)
		}
	}
}

func TestChannelsAndNSFWHiding(t *testing.T) {
	srv, st, _ := newTestServer(t)
	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	alice := &store.User{Username: "alice", DisplayName: "Alice", PasswordHash: hash, Role: "user"}
	if err := st.CreateUser(alice); err != nil {
		t.Fatalf("create user: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := loginClient(t, ts.URL, "alice", "correct horse battery")

	resp := httpPostForm(t, c, ts.URL, ts.URL+"/new", url.Values{
		"title":   {"General hello"},
		"body":    {"hello #public"},
		"channel": {"general"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	generalID := path.Base(resp.Header.Get("Location"))

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/new", url.Values{
		"title":   {"A meme"},
		"body":    {"memes only"},
		"channel": {"memes"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	memesID := path.Base(resp.Header.Get("Location"))

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/new", url.Values{
		"title":   {"Secret adult"},
		"body":    {"graphic #secret"},
		"channel": {"nsfw"},
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/nsfw/confirm", url.Values{"next": {"/c/nsfw"}})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	resp = httpPostForm(t, c, ts.URL, ts.URL+"/new", url.Values{
		"title":   {"Secret adult"},
		"body":    {"graphic #secret"},
		"channel": {"nsfw"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	nsfwID := path.Base(resp.Header.Get("Location"))
	if nsfwID == generalID || nsfwID == memesID {
		t.Fatalf("unexpected post ids: general=%s memes=%s nsfw=%s", generalID, memesID, nsfwID)
	}

	anon := newTestClient(t)
	resp = httpGet(t, anon, ts.URL+"/")
	body := bodyOf(t, resp)
	wantStatus(t, resp, http.StatusOK)
	if strings.Contains(body, "Secret adult") {
		t.Fatal("NSFW post leaked onto the front page")
	}
	if !strings.Contains(body, "General hello") {
		t.Fatal("general post missing from the front page")
	}

	resp = httpGet(t, anon, ts.URL+"/c/memes")
	body = bodyOf(t, resp)
	wantStatus(t, resp, http.StatusOK)
	if !strings.Contains(body, "A meme") || strings.Contains(body, "General hello") {
		t.Fatalf("memes channel showed the wrong posts: %s", body)
	}

	resp = httpGet(t, anon, ts.URL+"/c/nsfw")
	body = bodyOf(t, resp)
	wantStatus(t, resp, http.StatusOK)
	if !strings.Contains(body, "18+ warning") || strings.Contains(body, "Secret adult") {
		t.Fatalf("NSFW channel did not show the warning: %s", body)
	}

	resp = httpGet(t, anon, ts.URL+"/posts/"+nsfwID)
	body = bodyOf(t, resp)
	wantStatus(t, resp, http.StatusOK)
	if !strings.Contains(body, "18+ warning") || strings.Contains(body, "Secret adult") {
		t.Fatalf("NSFW post did not show the warning: %s", body)
	}

	resp = httpGet(t, anon, ts.URL+"/tags")
	body = bodyOf(t, resp)
	wantStatus(t, resp, http.StatusOK)
	if strings.Contains(body, "secret") {
		t.Fatal("NSFW hashtag leaked into the tag list")
	}
	resp = httpGet(t, anon, ts.URL+"/tags/secret")
	body = bodyOf(t, resp)
	wantStatus(t, resp, http.StatusOK)
	if strings.Contains(body, "Secret adult") {
		t.Fatal("NSFW post leaked into a tag page")
	}

	resp = httpGet(t, anon, ts.URL+"/api/v1/posts")
	body = bodyOf(t, resp)
	wantStatus(t, resp, http.StatusOK)
	if strings.Contains(body, "Secret adult") {
		t.Fatal("NSFW post leaked through the API")
	}
	resp = httpGet(t, anon, ts.URL+"/api/v1/posts/"+nsfwID)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
	resp = httpGet(t, anon, ts.URL+"/api/v1/posts/"+nsfwID+"/comments")
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()

	resp = httpGet(t, anon, ts.URL+"/feed.xml")
	body = bodyOf(t, resp)
	wantStatus(t, resp, http.StatusOK)
	if strings.Contains(body, "Secret adult") {
		t.Fatal("NSFW post leaked through RSS")
	}

	for i := 0; i < 10; i++ {
		resp = httpGet(t, anon, ts.URL+"/random")
		wantStatus(t, resp, http.StatusSeeOther)
		if resp.Header.Get("Location") == "/posts/"+nsfwID {
			t.Fatal("random post selected an NSFW post")
		}
		resp.Body.Close()
	}

	resp = httpPostForm(t, anon, ts.URL, ts.URL+"/nsfw/confirm", url.Values{"next": {"/posts/" + nsfwID}})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()
	resp = httpGet(t, anon, ts.URL+"/posts/"+nsfwID)
	body = bodyOf(t, resp)
	wantStatus(t, resp, http.StatusOK)
	if !strings.Contains(body, "Secret adult") {
		t.Fatal("confirmed adult post did not render")
	}
	resp = httpGet(t, anon, ts.URL+"/api/v1/posts/"+nsfwID)
	wantStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}

func TestAdminNewsTicker(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	admin := newTestClient(t)
	setupAdmin(t, admin, ts.URL)

	resp := httpGet(t, admin, ts.URL+"/")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "a test instance") {
		t.Fatalf("home should fall back to tagline: %s", body)
	}

	resp = httpPostForm(t, admin, ts.URL, ts.URL+"/admin/news", url.Values{
		"body": {"Town hall on Friday"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	resp = httpGet(t, admin, ts.URL+"/admin/news")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "Town hall on Friday") {
		t.Fatalf("admin news page missing line: %s", body)
	}

	resp = httpGet(t, admin, ts.URL+"/")
	wantStatus(t, resp, http.StatusOK)
	if body := bodyOf(t, resp); !strings.Contains(body, "Town hall on Friday") {
		t.Fatalf("home page missing news line: %s", body)
	}

	news, err := st.ListNews()
	if err != nil || len(news) != 1 {
		t.Fatalf("list news = %+v, err = %v", news, err)
	}

	resp = httpPostForm(t, admin, ts.URL, ts.URL+"/admin/news/"+news[0].ID+"/edit", url.Values{
		"body": {"Town hall moved to Saturday"},
	})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	resp = httpGet(t, admin, ts.URL+"/")
	wantStatus(t, resp, http.StatusOK)
	body := bodyOf(t, resp)
	if !strings.Contains(body, "Town hall moved to Saturday") || strings.Contains(body, "Town hall on Friday") {
		t.Fatalf("home page did not show updated news: %s", body)
	}

	resp = httpPostForm(t, admin, ts.URL, ts.URL+"/admin/news/"+news[0].ID+"/delete", url.Values{})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	resp = httpGet(t, admin, ts.URL+"/")
	wantStatus(t, resp, http.StatusOK)
	body = bodyOf(t, resp)
	if strings.Contains(body, "Town hall moved to Saturday") || !strings.Contains(body, "a test instance") {
		t.Fatalf("home page should fall back to tagline: %s", body)
	}

	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	reader := &store.User{
		ID:           store.NewID(),
		Username:     "reader",
		DisplayName:  "Reader",
		Email:        "reader@example.test",
		PasswordHash: hash,
		Role:         "user",
		CreatedAt:    store.Now(),
	}
	if err := st.CreateUser(reader); err != nil {
		t.Fatalf("create reader: %v", err)
	}
	user := loginClient(t, ts.URL, "reader", "correct horse battery")
	resp = httpPostForm(t, user, ts.URL, ts.URL+"/admin/news", url.Values{
		"body": {"sneaky news"},
	})
	wantStatus(t, resp, http.StatusForbidden)
	resp.Body.Close()
}

func newProxyTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		Addr:          ":0",
		DBPath:        filepath.Join(dir, "test.db"),
		BaseURL:       "http://example.test",
		SiteName:      "Test Town",
		Tagline:       "a test instance",
		SecureCookies: false,
		AllowSetup:    true,
		SessionTTL:    time.Hour,
		TrustProxy:    true,
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv, err := New(cfg, st)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return srv, st
}

func ipGet(t *testing.T, c *http.Client, target, ip string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Forwarded-For", ip)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s as %s: %v", target, ip, err)
	}
	return resp
}

func ipPostForm(t *testing.T, c *http.Client, base, target, ip string, values url.Values) *http.Response {
	t.Helper()
	values.Set("_csrf", csrfFor(t, c, base))
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", ip)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST %s as %s: %v", target, ip, err)
	}
	return resp
}

func TestAttackModeManual(t *testing.T) {
	srv, st := newProxyTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	admin := newTestClient(t)
	setupAdmin(t, admin, ts.URL)

	recorded := "198.51.100.7"
	resp := ipGet(t, newTestClient(t), ts.URL+"/", recorded)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = httpPostForm(t, admin, ts.URL, ts.URL+"/admin/lockdown", url.Values{})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	if !srv.lockdown.Load() {
		t.Fatalf("attack mode did not turn on")
	}
	if on, err := st.LockdownEnabled(); err != nil || !on {
		t.Fatalf("stored lockdown = %v, err = %v", on, err)
	}

	resp = ipGet(t, newTestClient(t), ts.URL+"/", recorded)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	stranger := "203.0.113.9"
	resp = ipGet(t, newTestClient(t), ts.URL+"/", stranger)
	wantStatus(t, resp, http.StatusServiceUnavailable)
	if body := bodyOf(t, resp); !strings.Contains(body, "This website is under attack.") {
		t.Fatalf("attack page missing message: %s", body)
	}

	resp = ipGet(t, admin, ts.URL+"/", stranger)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = ipGet(t, newTestClient(t), ts.URL+"/healthz", stranger)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = ipGet(t, newTestClient(t), ts.URL+"/static/style.css", stranger)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
	resp = ipGet(t, newTestClient(t), ts.URL+"/api/v1/site", "203.0.113.11")
	wantStatus(t, resp, http.StatusServiceUnavailable)
	resp.Body.Close()

	resp = httpPostForm(t, admin, ts.URL, ts.URL+"/admin/lockdown/disable", url.Values{})
	wantStatus(t, resp, http.StatusSeeOther)
	resp.Body.Close()

	resp = ipGet(t, newTestClient(t), ts.URL+"/", stranger)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}

func TestAttackModeAutomatic(t *testing.T) {
	old := autoLockdownThreshold
	autoLockdownThreshold = 3
	defer func() { autoLockdownThreshold = old }()

	srv, _ := newProxyTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	admin := newTestClient(t)
	setupAdmin(t, admin, ts.URL)

	recorded := "198.51.100.20"
	resp := ipGet(t, newTestClient(t), ts.URL+"/", recorded)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	attacker := "203.0.113.50"
	c := newTestClient(t)
	resp = ipGet(t, c, ts.URL+"/login", attacker)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	for i := 0; i < 20 && !srv.lockdown.Load(); i++ {
		resp = ipPostForm(t, c, ts.URL, ts.URL+"/login", attacker, url.Values{
			"login":    {"attacker"},
			"password": {"wrong password"},
		})
		resp.Body.Close()
	}
	if !srv.lockdown.Load() {
		t.Fatalf("attack mode did not turn on automatically")
	}

	resp = ipGet(t, newTestClient(t), ts.URL+"/", attacker)
	wantStatus(t, resp, http.StatusServiceUnavailable)
	resp.Body.Close()

	resp = ipGet(t, newTestClient(t), ts.URL+"/", recorded)
	wantStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	resp = ipGet(t, newTestClient(t), ts.URL+"/", "203.0.113.99")
	wantStatus(t, resp, http.StatusServiceUnavailable)
	resp.Body.Close()

	if err := srv.disableLockdown(); err != nil {
		t.Fatalf("disable lockdown: %v", err)
	}
}
