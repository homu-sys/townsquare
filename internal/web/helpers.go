package web

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"townsquare/internal/auth"
	"townsquare/internal/store"
)

type StatsView struct {
	Users        int
	Posts        int
	Comments     int
	ActiveAdmins int
	Hits         int64
}

type CommentView struct {
	store.Comment
	Depth              int
	Indent             int
	ReplyToUsername    string
	ReplyToDisplayName string
	CanDelete          bool
	Reactions          ReactionView
}

type ReplyTarget struct {
	ID                string
	AuthorUsername    string
	AuthorDisplayName string
}

type PageData struct {
	SiteName string
	Tagline  string
	BaseURL  string
	Version  string
	Path     string
	Heading  string

	Description    string
	OGTitle        string
	OGType         string
	OGImage        string
	CanonicalURL   string
	NoIndex        bool
	InviteRequired bool

	User     *store.User
	LoggedIn bool
	IsAdmin  bool

	CSRFToken string
	Flash     string
	Error     string
	Hits      int64
	Lockdown  bool

	Posts         []store.Post
	Post          *store.Post
	Comments      []CommentView
	ReplyTo       *ReplyTarget
	Profile       *store.User
	Peer          *store.User
	Users         []store.User
	Invites       []store.Invite
	News          []store.NewsItem
	Messages      []MessageView
	Conversations []store.Conversation
	BlockedDMs    []store.DirectMessage
	NewInvite     *store.Invite
	Stats         *StatsView
	Token         string

	RecoveryCodes     []string
	RecoveryCodeCount int
	ResetLink         string
	ResetTarget       *store.User
	ResetToken        string
	ResetExpires      string

	PostReactions      ReactionView
	PostTags           []string
	Tags               []store.TagCount
	Tag                string
	Channels           []ChannelInfo
	Channel            string
	ChannelName        string
	ChannelDescription string
	Adult              bool
	AdultNext          string

	Form       map[string]string
	FormAction string
	CanEdit    bool
	CanDelete  bool

	Prev    string
	Next    string
	PageNum int
	Total   int
}

func (s *Server) baseURL(r *http.Request) string {
	if s.cfg.BaseURL != "" {
		return s.cfg.BaseURL
	}
	return requestBaseURL(r, s.cfg.TrustProxy)
}

func requestBaseURL(r *http.Request, trustProxy bool) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if trustProxy {
		if proto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); proto == "http" || proto == "https" {
			scheme = proto
		}
		if forwardedHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); validHost(forwardedHost) {
			host = forwardedHost
		}
	}
	if !validHost(host) {
		host = "localhost"
	}
	return scheme + "://" + host
}

func firstForwardedValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func validHost(host string) bool {
	if host == "" || len(host) > 255 {
		return false
	}
	for _, c := range host {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == ':', c == '[', c == ']':
		default:
			return false
		}
	}
	return true
}

func (s *Server) newPage(r *http.Request) PageData {
	u := userFrom(r)
	return PageData{
		SiteName:       s.cfg.SiteName,
		Tagline:        s.cfg.Tagline,
		BaseURL:        s.baseURL(r),
		Version:        Version,
		Path:           r.URL.Path,
		User:           u,
		LoggedIn:       u != nil,
		IsAdmin:        u != nil && u.Role == "admin",
		CSRFToken:      csrfFrom(r),
		NoIndex:        sensitivePath(r.URL.Path),
		InviteRequired: !s.cfg.OpenRegistration,
		Adult:          s.adultOK(r),
		Form:           map[string]string{},
	}
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, page PageData) {
	if page.SiteName == "" {
		page.SiteName = s.cfg.SiteName
	}
	if page.Tagline == "" {
		page.Tagline = s.cfg.Tagline
	}
	if page.BaseURL == "" {
		page.BaseURL = s.baseURL(r)
	}
	if page.Version == "" {
		page.Version = Version
	}
	if page.Path == "" {
		page.Path = r.URL.Path
	}
	if page.Description == "" {
		page.Description = page.Tagline
	}
	if page.OGTitle == "" {
		if page.Heading != "" {
			page.OGTitle = page.Heading
		} else {
			page.OGTitle = page.SiteName
		}
	}
	if page.OGType == "" {
		page.OGType = "website"
	}
	if page.OGImage == "" {
		page.OGImage = page.BaseURL + "/static/og.png"
	}
	if page.CanonicalURL == "" {
		page.CanonicalURL = page.BaseURL + r.URL.Path
	}
	if page.User == nil {
		page.User = userFrom(r)
	}
	page.LoggedIn = page.User != nil
	page.IsAdmin = page.LoggedIn && page.User.Role == "admin"
	if page.CSRFToken == "" {
		page.CSRFToken = csrfFrom(r)
	}
	if page.Form == nil {
		page.Form = map[string]string{}
	}
	if page.Flash == "" {
		page.Flash = s.popFlash(w, r)
	}
	if page.Hits == 0 {
		page.Hits, _ = s.store.GetHits()
	}

	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, name, page); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	page := s.newPage(r)
	page.Heading = http.StatusText(status)
	page.Error = msg
	page.NoIndex = true
	s.render(w, r, status, "error.html", page)
}

func redirect(w http.ResponseWriter, r *http.Request, path string) {
	if path == "" {
		path = "/"
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

func (s *Server) setFlash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "ts_flash",
		Value:    url.QueryEscape(msg),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60,
	})
}

func (s *Server) popFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie("ts_flash")
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "ts_flash",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	v, err := url.QueryUnescape(c.Value)
	if err != nil {
		return ""
	}
	return v
}

func (s *Server) checkCSRF(w http.ResponseWriter, r *http.Request) bool {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Could not read the form.")
		return false
	}
	got := r.PostFormValue("_csrf")
	want := csrfFrom(r)
	if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		s.renderError(w, r, http.StatusForbidden, "Invalid or expired form token. Please go back and try again.")
		return false
	}
	return true
}

func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	u := userFrom(r)
	if u == nil {
		redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()))
		return nil, false
	}
	return u, true
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return nil, false
	}
	if u.Role != "admin" {
		s.renderError(w, r, http.StatusForbidden, "You do not have permission to view that page.")
		return nil, false
	}
	return u, true
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User) (*store.Session, error) {
	sess, err := auth.NewSession(u.ID, s.cfg.SessionTTL)
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateSession(sess); err != nil {
		return nil, err
	}
	http.SetCookie(w, auth.SessionCookieValue(sess.ID, s.cfg.SessionTTL, s.cfg.SecureCookies))
	http.SetCookie(w, auth.CSRFCookieValue(sess.CSRFToken, s.cfg.SecureCookies))
	return sess, nil
}

func pageParam(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

func pageURL(base string, page int) string {
	if page <= 1 {
		return base
	}
	return fmt.Sprintf("%s?page=%d", base, page)
}

func trimPage[T any](items []T, limit int) ([]T, bool) {
	if len(items) > limit {
		return items[:limit], true
	}
	return items, false
}

func safeNext(v string) string {
	if v == "" || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.HasPrefix(v, "/\\") {
		return "/"
	}
	return v
}

func apiLimitOffset(r *http.Request) (int, int) {
	limit := 20
	offset := 0
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = n
	}
	if limit > 100 {
		limit = 100
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n > 0 {
		offset = n
	}
	return limit, offset
}

func sensitivePath(p string) bool {
	switch p {
	case "/login", "/register", "/setup", "/reset", "/recover":
		return true
	}
	return strings.HasPrefix(p, "/admin") || strings.HasPrefix(p, "/settings") ||
		strings.HasPrefix(p, "/messages") || strings.HasPrefix(p, "/api/") ||
		strings.HasPrefix(p, "/c/nsfw")
}

var (
	blockTagRE = regexp.MustCompile(`(?i)</?(p|div|br|li|ul|ol|h[1-6]|blockquote|pre|tr|td|th)[^>]*>`)
	htmlTagRE  = regexp.MustCompile(`<[^>]*>`)
)

func truncateText(v string, max int) string {
	v = strings.Join(strings.Fields(v), " ")
	if utf8.RuneCountInString(v) <= max {
		return v
	}
	return strings.TrimSpace(string([]rune(v)[:max-1])) + "…"
}

func summarizeHTML(v string, max int) string {
	v = blockTagRE.ReplaceAllString(v, " ")
	v = htmlTagRE.ReplaceAllString(v, "")
	return truncateText(html.UnescapeString(v), max)
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"safeHTML": func(v string) template.HTML {
			return template.HTML(v)
		},
		"nl2br": func(v string) template.HTML {
			return template.HTML(strings.ReplaceAll(html.EscapeString(v), "\n", "<br>\n"))
		},
		"fdate": func(v string) string {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return v
			}
			return t.Local().Format("Jan 2, 2006 15:04")
		},
		"fday": func(v string) string {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return v
			}
			return t.Local().Format("Jan 2, 2006")
		},
		"inc": func(n int) int { return n + 1 },
		"dec": func(n int) int { return n - 1 },
		"add": func(a, b int) int { return a + b },
		"plural": func(n int, one, many string) string {
			if n == 1 {
				return one
			}
			return many
		},
		"hasprefix":    strings.HasPrefix,
		"emojiPalette": func() []string { return emojiPalette },
	}
}
