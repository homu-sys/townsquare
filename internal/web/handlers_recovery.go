package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"townsquare/internal/auth"
	"townsquare/internal/store"
)

const resetLinkTTL = 24 * time.Hour

func (s *Server) showRecoveryCodes(w http.ResponseWriter, r *http.Request, u *store.User, codes []string, csrf, message string) {
	page := s.newPage(r)
	page.Heading = "Save your recovery codes"
	page.User = u
	page.LoggedIn = true
	page.IsAdmin = u.IsAdmin()
	page.IsOwner = u.IsOwner()
	page.IsModerator = u.IsModerator()
	page.IsStaff = u.IsStaff()
	if csrf != "" {
		page.CSRFToken = csrf
	}
	page.RecoveryCodes = codes
	page.Flash = message
	s.render(w, r, http.StatusOK, "recovery_codes.html", page)
}

func (s *Server) recoverForm(w http.ResponseWriter, r *http.Request) {
	page := s.newPage(r)
	page.Heading = "Recover your account"
	s.render(w, r, http.StatusOK, "recover.html", page)
}

func (s *Server) recoverPost(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(w, r) {
		return
	}
	login := strings.TrimSpace(r.PostFormValue("login"))
	code := strings.TrimSpace(r.PostFormValue("code"))
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm")

	page := s.newPage(r)
	page.Heading = "Recover your account"
	page.Form = map[string]string{"login": login, "code": code}

	if login == "" || code == "" {
		page.Error = "Enter your username (or email) and a recovery code."
		s.render(w, r, http.StatusBadRequest, "recover.html", page)
		return
	}
	if msg := auth.ValidatePassword(password); msg != "" {
		page.Error = msg
		s.render(w, r, http.StatusBadRequest, "recover.html", page)
		return
	}
	if password != confirm {
		page.Error = "The two new passwords do not match."
		s.render(w, r, http.StatusBadRequest, "recover.html", page)
		return
	}

	u, err := s.store.GetUserByLogin(login)
	if err != nil || u.Disabled {
		page.Error = "That username and recovery code do not match."
		s.render(w, r, http.StatusUnauthorized, "recover.html", page)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not secure the new password.")
		return
	}
	normalized := auth.NormalizeRecoveryCode(code)
	if err := s.store.ConsumeRecoveryCode(u.ID, auth.HashToken(normalized), hash); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			page.Error = "That recovery code is not valid or has already been used."
			s.render(w, r, http.StatusUnauthorized, "recover.html", page)
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Could not reset the password.")
		return
	}
	s.setFlash(w, "Password reset. Log in with your new password.")
	redirect(w, r, "/login")
}

func (s *Server) adminUserReset(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	target, ok := s.adminTarget(w, r, admin)
	if !ok {
		return
	}

	token := auth.NewToken()
	expires := time.Now().UTC().Add(resetLinkTTL)
	if err := s.store.CreatePasswordReset(auth.HashToken(token), target.ID, expires.Format(time.RFC3339)); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not create a reset link.")
		return
	}

	page := s.newPage(r)
	page.Heading = "Password reset link"
	page.ResetTarget = target
	page.ResetLink = s.baseURL(r) + "/reset?token=" + url.QueryEscape(token)
	page.ResetExpires = expires.Format(time.RFC3339)
	s.render(w, r, http.StatusOK, "admin_reset.html", page)
}

func (s *Server) resetForm(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		s.renderError(w, r, http.StatusBadRequest, "This reset link is invalid or has expired.")
		return
	}
	if _, err := s.store.GetPasswordReset(auth.HashToken(token)); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "This reset link is invalid or has expired.")
		return
	}
	page := s.newPage(r)
	page.Heading = "Choose a new password"
	page.ResetToken = token
	s.render(w, r, http.StatusOK, "reset.html", page)
}

func (s *Server) resetPost(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(w, r) {
		return
	}
	token := strings.TrimSpace(r.PostFormValue("token"))
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm")

	page := s.newPage(r)
	page.Heading = "Choose a new password"
	page.ResetToken = token

	if token == "" {
		s.renderError(w, r, http.StatusBadRequest, "This reset link is invalid or has expired.")
		return
	}
	if msg := auth.ValidatePassword(password); msg != "" {
		page.Error = msg
		s.render(w, r, http.StatusBadRequest, "reset.html", page)
		return
	}
	if password != confirm {
		page.Error = "The two new passwords do not match."
		s.render(w, r, http.StatusBadRequest, "reset.html", page)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not secure the new password.")
		return
	}
	if err := s.store.ConsumePasswordReset(auth.HashToken(token), hash); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusBadRequest, "This reset link is invalid or has expired.")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Could not reset the password.")
		return
	}
	s.setFlash(w, "Password reset. Log in with your new password.")
	redirect(w, r, "/login")
}

func (s *Server) recoveryCodesPost(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	if !auth.CheckPassword(u.PasswordHash, r.PostFormValue("current_password")) {
		s.setFlash(w, "Your current password is not correct.")
		redirect(w, r, "/settings")
		return
	}
	codes, hashes := auth.GenerateRecoveryCodes(8)
	if err := s.store.ReplaceRecoveryCodes(u.ID, hashes); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not create recovery codes.")
		return
	}
	s.showRecoveryCodes(w, r, u, codes, "", "New recovery codes created. Your old codes no longer work.")
}
