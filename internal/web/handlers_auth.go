package web

import (
	"errors"
	"net/http"
	"strings"

	"townsquare/internal/auth"
	"townsquare/internal/store"
)

func (s *Server) setupForm(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read the database.")
		return
	}
	if n > 0 || !s.cfg.AllowSetup {
		redirect(w, r, "/login")
		return
	}
	page := s.newPage(r)
	page.Heading = "Set up your town"
	s.render(w, r, http.StatusOK, "setup.html", page)
}

func (s *Server) setupPost(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(w, r) {
		return
	}
	n, err := s.store.CountUsers()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read the database.")
		return
	}
	if n > 0 || !s.cfg.AllowSetup {
		redirect(w, r, "/login")
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	display := strings.TrimSpace(r.PostFormValue("display_name"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm")

	page := s.newPage(r)
	page.Heading = "Set up your town"
	page.Form = map[string]string{
		"username":     username,
		"display_name": display,
		"email":        email,
	}
	if msg := auth.ValidateUsername(username); msg != "" {
		page.Error = msg
		s.render(w, r, http.StatusBadRequest, "setup.html", page)
		return
	}
	if msg := auth.ValidateEmail(email); msg != "" {
		page.Error = msg
		s.render(w, r, http.StatusBadRequest, "setup.html", page)
		return
	}
	if msg := auth.ValidatePassword(password); msg != "" {
		page.Error = msg
		s.render(w, r, http.StatusBadRequest, "setup.html", page)
		return
	}
	if password != confirm {
		page.Error = "The two passwords do not match."
		s.render(w, r, http.StatusBadRequest, "setup.html", page)
		return
	}
	if display == "" {
		display = username
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not secure the password.")
		return
	}
	u := &store.User{
		ID:           store.NewID(),
		Username:     username,
		DisplayName:  display,
		Email:        email,
		PasswordHash: hash,
		Role:         "admin",
		CreatedAt:    store.Now(),
	}
	if err := s.store.CreateFirstUser(u); err != nil {
		if errors.Is(err, store.ErrConflict) {
			if n, _ := s.store.CountUsers(); n > 0 {
				s.setFlash(w, "This town has already been set up.")
				redirect(w, r, "/login")
				return
			}
			page.Error = "That username or email is already in use."
			s.render(w, r, http.StatusConflict, "setup.html", page)
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Could not create the account.")
		return
	}
	sess, err := s.startSession(w, r, u)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not start a session.")
		return
	}
	codes, hashes := auth.GenerateRecoveryCodes(8)
	if err := s.store.ReplaceRecoveryCodes(u.ID, hashes); err != nil {
		s.setFlash(w, "Welcome, "+u.DisplayName+". Your account was created, but recovery codes could not be generated. You can generate them in Settings.")
		redirect(w, r, "/")
		return
	}
	s.showRecoveryCodes(w, r, u, codes, sess.CSRFToken, "Welcome, "+u.DisplayName+". This is your town now.")
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if userFrom(r) != nil {
		redirect(w, r, "/")
		return
	}
	n, _ := s.store.CountUsers()
	if n == 0 && s.cfg.AllowSetup {
		redirect(w, r, "/setup")
		return
	}
	page := s.newPage(r)
	page.Heading = "Log in"
	page.Next = safeNext(r.URL.Query().Get("next"))
	s.render(w, r, http.StatusOK, "login.html", page)
}

func (s *Server) loginPost(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(w, r) {
		return
	}
	login := strings.TrimSpace(r.PostFormValue("login"))
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	page := s.newPage(r)
	page.Heading = "Log in"
	page.Form = map[string]string{"login": login}
	page.Next = next

	if login == "" || password == "" {
		page.Error = "Enter your username (or email) and password."
		s.render(w, r, http.StatusBadRequest, "login.html", page)
		return
	}
	if ok, retry := s.loginLimiter.Allow(strings.ToLower(login)); !ok {
		w.Header().Set("Retry-After", retryAfter(retry))
		page.Error = "Too many login attempts for that account. Try again later."
		s.render(w, r, http.StatusTooManyRequests, "login.html", page)
		return
	}
	u, err := s.store.GetUserByLogin(login)
	valid := false
	if err != nil {
		auth.DummyPasswordCheck(password)
	} else {
		valid = auth.CheckPassword(u.PasswordHash, password)
	}
	if !valid {
		page.Error = "Wrong username/email or password."
		s.render(w, r, http.StatusUnauthorized, "login.html", page)
		return
	}
	if u.Disabled {
		page.Error = "That account has been disabled."
		s.render(w, r, http.StatusForbidden, "login.html", page)
		return
	}
	if old := sessionFrom(r); old != nil {
		_ = s.store.DeleteSession(old.ID)
	}
	if _, err := s.startSession(w, r, u); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not start a session.")
		return
	}
	s.setFlash(w, "Welcome back, "+u.DisplayName+".")
	redirect(w, r, next)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(w, r) {
		return
	}
	if sess := sessionFrom(r); sess != nil {
		_ = s.store.DeleteSession(sess.ID)
	}
	http.SetCookie(w, auth.ClearSessionCookie(s.cfg.SecureCookies))
	s.setFlash(w, "You have been logged out.")
	redirect(w, r, "/")
}

func (s *Server) registerForm(w http.ResponseWriter, r *http.Request) {
	if userFrom(r) != nil {
		redirect(w, r, "/")
		return
	}
	n, _ := s.store.CountUsers()
	if n == 0 && s.cfg.AllowSetup {
		redirect(w, r, "/setup")
		return
	}
	page := s.newPage(r)
	page.Heading = "Create an account"
	page.Form = map[string]string{
		"invite": strings.TrimSpace(r.URL.Query().Get("invite")),
	}
	s.render(w, r, http.StatusOK, "register.html", page)
}

func (s *Server) registerPost(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(w, r) {
		return
	}
	code := strings.TrimSpace(r.PostFormValue("invite"))
	username := strings.TrimSpace(r.PostFormValue("username"))
	display := strings.TrimSpace(r.PostFormValue("display_name"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm")

	page := s.newPage(r)
	page.Heading = "Create an account"
	page.Form = map[string]string{
		"invite":       code,
		"username":     username,
		"display_name": display,
		"email":        email,
	}

	if code == "" && !s.cfg.OpenRegistration {
		page.Error = "You need an invite code to register."
		s.render(w, r, http.StatusBadRequest, "register.html", page)
		return
	}
	if msg := auth.ValidateUsername(username); msg != "" {
		page.Error = msg
		s.render(w, r, http.StatusBadRequest, "register.html", page)
		return
	}
	if msg := auth.ValidateEmail(email); msg != "" {
		page.Error = msg
		s.render(w, r, http.StatusBadRequest, "register.html", page)
		return
	}
	if msg := auth.ValidatePassword(password); msg != "" {
		page.Error = msg
		s.render(w, r, http.StatusBadRequest, "register.html", page)
		return
	}
	if password != confirm {
		page.Error = "The two passwords do not match."
		s.render(w, r, http.StatusBadRequest, "register.html", page)
		return
	}
	if display == "" {
		display = username
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not secure the password.")
		return
	}
	u := &store.User{
		ID:           store.NewID(),
		Username:     username,
		DisplayName:  display,
		Email:        email,
		PasswordHash: hash,
		Role:         "user",
		CreatedAt:    store.Now(),
	}
	var regErr error
	if code == "" {
		regErr = s.store.CreateUser(u)
	} else {
		regErr = s.store.RegisterWithInvite(code, u)
	}
	if regErr != nil {
		switch {
		case errors.Is(regErr, store.ErrNotFound):
			page.Error = "That invite code is not valid."
		case errors.Is(regErr, store.ErrInviteUsed):
			page.Error = "That invite code has already been used."
		case errors.Is(regErr, store.ErrInviteExpired):
			page.Error = "That invite code has expired."
		case errors.Is(regErr, store.ErrConflict):
			page.Error = "That username or email is already in use."
		default:
			s.renderError(w, r, http.StatusInternalServerError, "Could not create the account.")
			return
		}
		s.render(w, r, http.StatusConflict, "register.html", page)
		return
	}
	sess, err := s.startSession(w, r, u)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not start a session.")
		return
	}
	codes, hashes := auth.GenerateRecoveryCodes(8)
	if err := s.store.ReplaceRecoveryCodes(u.ID, hashes); err != nil {
		s.setFlash(w, "Welcome to "+s.cfg.SiteName+", "+u.DisplayName+". Your account was created, but recovery codes could not be generated. You can generate them in Settings.")
		redirect(w, r, "/")
		return
	}
	s.showRecoveryCodes(w, r, u, codes, sess.CSRFToken, "Welcome to "+s.cfg.SiteName+", "+u.DisplayName+".")
}
