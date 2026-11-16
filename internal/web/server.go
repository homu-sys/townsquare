package web

import (
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"townsquare/internal/config"
	"townsquare/internal/moderation"
	"townsquare/internal/store"
)

const Version = "2.0"

type Server struct {
	cfg             config.Config
	store           *store.Store
	mux             *http.ServeMux
	templates       *template.Template
	authLimiter     *rateLimiter
	loginLimiter    *rateLimiter
	apiLimiter      *rateLimiter
	dmLimiter       *rateLimiter
	reactionLimiter *rateLimiter
	avatarChecker   moderation.Checker
	lockdown        atomic.Bool
	flood           *floodGuard
}

func New(cfg config.Config, st *store.Store) (*Server, error) {
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("").Funcs(templateFuncs()).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:             cfg,
		store:           st,
		templates:       tmpl,
		authLimiter:     newRateLimiter(10, time.Minute),
		loginLimiter:    newRateLimiter(10, 15*time.Minute),
		apiLimiter:      newRateLimiter(60, time.Minute),
		dmLimiter:       newRateLimiter(30, time.Minute),
		reactionLimiter: newRateLimiter(120, time.Minute),
		flood:           newFloodGuard(),
	}
	locked, err := st.LockdownEnabled()
	if err != nil {
		log.Printf("warning: could not read attack mode: %v", err)
	}
	s.lockdown.Store(locked)
	if cfg.ImageModeration {
		checker, err := moderation.New()
		if err != nil {
			log.Printf("warning: avatar image moderation disabled: %v", err)
		} else {
			s.avatarChecker = checker
		}
	}
	s.routes(staticFS)
	return s, nil
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = s.trackVisitor(h)
	h = s.attackMode(h)
	h = s.visitMarkers(h)
	h = s.loadSession(h)
	h = s.securityHeaders(h)
	h = s.limitRequestBody(h)
	h = s.recoverPanic(h)
	h = s.logRequests(h)
	return h
}

func (s *Server) routes(staticFS fs.FS) {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		data, err := assets.ReadFile("static/favicon.ico")
		if err != nil {
			s.notFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
	})

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /about", s.about)

	mux.HandleFunc("GET /random", s.randomPost)
	mux.HandleFunc("GET /feed.xml", s.siteFeed)
	mux.HandleFunc("GET /rss.xml", s.siteFeed)
	mux.HandleFunc("GET /federation/latest.json", s.federationLatest)
	mux.HandleFunc("GET /feed", func(w http.ResponseWriter, r *http.Request) {
		redirect(w, r, "/feed.xml")
	})

	mux.HandleFunc("GET /setup", s.setupForm)
	mux.HandleFunc("POST /setup", s.limitHTML(s.authLimiter, s.setupPost))
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.limitHTML(s.authLimiter, s.loginPost))
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /register", s.registerForm)
	mux.HandleFunc("POST /register", s.limitHTML(s.authLimiter, s.registerPost))
	mux.HandleFunc("GET /recover", s.recoverForm)
	mux.HandleFunc("POST /recover", s.limitHTML(s.authLimiter, s.recoverPost))
	mux.HandleFunc("GET /reset", s.resetForm)
	mux.HandleFunc("POST /reset", s.limitHTML(s.authLimiter, s.resetPost))

	mux.HandleFunc("GET /tags", s.tagsList)
	mux.HandleFunc("GET /tags/{tag}", s.tagView)

	mux.HandleFunc("GET /channels", s.channelsList)
	mux.HandleFunc("GET /c/{channel}", s.channelView)
	mux.HandleFunc("POST /nsfw/confirm", s.nsfwConfirm)

	mux.HandleFunc("GET /users", s.usersList)
	mux.HandleFunc("GET /users/{username}", s.userProfile)
	mux.HandleFunc("GET /users/{username}/feed.xml", s.userFeed)
	mux.HandleFunc("POST /users/{username}/mute", s.userMute)
	mux.HandleFunc("POST /users/{username}/unmute", s.userUnmute)
	mux.HandleFunc("POST /users/{username}/block", s.userBlock)
	mux.HandleFunc("POST /users/{username}/unblock", s.userUnblock)

	mux.HandleFunc("GET /bookmarks", s.bookmarksPage)
	mux.HandleFunc("GET /new", s.postNewForm)
	mux.HandleFunc("POST /new", s.postCreate)
	mux.HandleFunc("GET /posts/{id}", s.postView)
	mux.HandleFunc("POST /posts/{id}/bookmark", s.postBookmark)
	mux.HandleFunc("POST /posts/{id}/unbookmark", s.postUnbookmark)
	mux.HandleFunc("GET /posts/{id}/edit", s.postEditForm)
	mux.HandleFunc("POST /posts/{id}/edit", s.postEdit)
	mux.HandleFunc("POST /posts/{id}/delete", s.postDelete)
	mux.HandleFunc("POST /posts/{id}/comments", s.commentCreate)
	mux.HandleFunc("POST /comments/{id}/delete", s.commentDelete)
	mux.HandleFunc("POST /reactions", s.limitHTML(s.reactionLimiter, s.reactionToggle))

	mux.HandleFunc("GET /settings", s.settingsForm)
	mux.HandleFunc("POST /settings", s.settingsPost)
	mux.HandleFunc("POST /settings/avatar", s.settingsAvatarPost)
	mux.HandleFunc("POST /settings/password", s.passwordPost)
	mux.HandleFunc("POST /settings/token", s.tokenPost)
	mux.HandleFunc("POST /settings/recovery-codes", s.recoveryCodesPost)

	mux.HandleFunc("GET /messages", s.messagesInbox)
	mux.HandleFunc("GET /messages/{username}", s.messageThread)
	mux.HandleFunc("POST /messages/{username}", s.limitHTML(s.dmLimiter, s.messageSend))

	mux.HandleFunc("GET /admin", s.adminDashboard)
	mux.HandleFunc("POST /admin/lockdown", s.adminLockdownEnable)
	mux.HandleFunc("POST /admin/lockdown/disable", s.adminLockdownDisable)
	mux.HandleFunc("GET /admin/news", s.adminNews)
	mux.HandleFunc("POST /admin/news", s.adminNewsCreate)
	mux.HandleFunc("POST /admin/news/{id}/edit", s.adminNewsUpdate)
	mux.HandleFunc("POST /admin/news/{id}/delete", s.adminNewsDelete)
	mux.HandleFunc("GET /admin/invites", s.adminInvites)
	mux.HandleFunc("POST /admin/invites", s.adminInviteCreate)
	mux.HandleFunc("POST /admin/invites/{code}/delete", s.adminInviteDelete)
	mux.HandleFunc("GET /admin/users", s.adminUsers)
	mux.HandleFunc("POST /admin/users/{id}/disable", s.adminUserDisable)
	mux.HandleFunc("POST /admin/users/{id}/enable", s.adminUserEnable)
	mux.HandleFunc("POST /admin/users/{id}/moderate", s.adminUserMakeModerator)
	mux.HandleFunc("POST /admin/users/{id}/promote", s.adminUserPromote)
	mux.HandleFunc("POST /admin/users/{id}/demote", s.adminUserDemote)
	mux.HandleFunc("POST /admin/users/{id}/reset", s.adminUserReset)
	mux.HandleFunc("GET /admin/posts", s.adminPosts)
	mux.HandleFunc("POST /admin/posts/{id}/delete", s.adminPostDelete)
	mux.HandleFunc("GET /admin/dms", s.adminBlockedDMs)
	mux.HandleFunc("POST /admin/dms/{id}/delete", s.adminBlockedDMDelete)

	mux.HandleFunc("GET /api", s.apiDocs)
	mux.HandleFunc("GET /api/{$}", s.apiDocs)
	mux.HandleFunc("GET /api/v1/site", s.apiSite)
	mux.HandleFunc("GET /api/v1/users", s.apiUsers)
	mux.HandleFunc("GET /api/v1/users/{username}", s.apiUser)
	mux.HandleFunc("GET /api/v1/users/{username}/posts", s.apiUserPosts)
	mux.HandleFunc("GET /api/v1/posts", s.apiPosts)
	mux.HandleFunc("POST /api/v1/posts", s.limitAPI(s.apiLimiter, s.apiCreatePost))
	mux.HandleFunc("GET /api/v1/posts/{id}", s.apiPost)
	mux.HandleFunc("DELETE /api/v1/posts/{id}", s.limitAPI(s.apiLimiter, s.apiDeletePost))
	mux.HandleFunc("GET /api/v1/posts/{id}/comments", s.apiComments)
	mux.HandleFunc("POST /api/v1/posts/{id}/comments", s.limitAPI(s.apiLimiter, s.apiCreateComment))

	mux.HandleFunc("/api/", s.apiNotFound)
	mux.HandleFunc("/", s.notFound)

	s.mux = mux
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "That page does not exist. Try the front page.")
}
