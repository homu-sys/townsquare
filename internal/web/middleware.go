package web

import (
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"townsquare/internal/auth"
	"townsquare/internal/store"
)

const (
	visitCookie     = "ts_since"
	firstVisitSince = "1970-01-01T00:00:00Z"
	visitWindow     = time.Hour
)

func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSessionlessRequest(r) {
			next.ServeHTTP(w, withAuth(r, nil, nil, ""))
			return
		}

		var user *store.User
		var sess *store.Session

		if c, err := r.Cookie(auth.SessionCookie); err == nil && c.Value != "" {
			se, err := s.store.GetSession(c.Value)
			if err == nil {
				u, uerr := s.store.GetUserByID(se.UserID)
				if uerr == nil && !u.Disabled {
					user = u
					sess = se
				} else {
					_ = s.store.DeleteSession(se.ID)
					http.SetCookie(w, auth.ClearSessionCookie(s.cfg.SecureCookies))
				}
			}
		}

		csrf := ""
		if sess != nil {
			csrf = sess.CSRFToken
		} else if c, err := r.Cookie(auth.CSRFCookie); err == nil {
			csrf = c.Value
		}
		if csrf == "" {
			csrf = auth.NewToken()
		}
		http.SetCookie(w, auth.CSRFCookieValue(csrf, s.cfg.SecureCookies))

		next.ServeHTTP(w, withAuth(r, user, sess, csrf))
	})
}

func (s *Server) trackVisitor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || isSessionlessRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := s.store.Visit(s.clientIP(r)); err != nil {
			log.Printf("record visit: %v", err)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) visitMarkers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r) == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			next.ServeHTTP(w, r)
			return
		}
		since := ""
		if c, err := r.Cookie(visitCookie); err == nil {
			if v, err := url.QueryUnescape(c.Value); err == nil && validSince(v) {
				since = v
			}
		}
		if since == "" {
			previous, err := s.store.StartVisit(viewerID(r))
			if err != nil {
				log.Printf("start visit: %v", err)
				since = firstVisitSince
			} else if previous == "" {
				since = firstVisitSince
			} else {
				since = previous
			}
			s.setVisitSince(w, since)
		}
		next.ServeHTTP(w, withVisitSince(r, since))
	})
}

func validSince(v string) bool {
	_, err := time.Parse(time.RFC3339, v)
	return err == nil
}

func (s *Server) setVisitSince(w http.ResponseWriter, since string) {
	http.SetCookie(w, &http.Cookie{
		Name:     visitCookie,
		Value:    url.QueryEscape(since),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(visitWindow.Seconds()),
	})
}

func clearVisitSince(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     visitCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		imgSrc := "'self' data:"
		if s.cfg.RemoteImages {
			imgSrc = "'self' data: http: https:"
		}
		csp := "default-src 'self'; img-src " + imgSrc +
			"; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
		if s.cfg.SecureCookies {
			csp += "; upgrade-insecure-requests"
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		h.Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic: %v\n%s", v, debug.Stack())
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			limit := int64(1 << 20)
			if r.URL.Path == "/settings/avatar" {
				limit = maxAvatarBytes + (1 << 20)
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

func isFeedRequest(r *http.Request) bool {
	p := r.URL.Path
	return p == "/feed.xml" || p == "/rss.xml" || strings.HasSuffix(p, "/feed.xml")
}

func isSessionlessRequest(r *http.Request) bool {
	p := r.URL.Path
	return isFeedRequest(r) || p == "/healthz" || p == "/favicon.ico" ||
		strings.HasPrefix(p, "/static/") || strings.HasPrefix(p, "/api/")
}
