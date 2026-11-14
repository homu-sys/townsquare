package web

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"townsquare/internal/auth"
)

func (s *Server) settingsForm(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	page := s.newPage(r)
	page.Heading = "Settings"
	page.Form = map[string]string{"display_name": u.DisplayName, "bio": u.Bio}
	page.RecoveryCodeCount, _ = s.store.CountRecoveryCodes(u.ID)
	s.render(w, r, http.StatusOK, "settings.html", page)
}

func (s *Server) settingsPost(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	display := strings.TrimSpace(r.PostFormValue("display_name"))
	bio := strings.TrimSpace(r.PostFormValue("bio"))
	if display == "" {
		s.setFlash(w, "Your display name cannot be empty.")
		redirect(w, r, "/settings")
		return
	}
	if utf8.RuneCountInString(display) > 60 {
		s.setFlash(w, "Display names must be 60 characters or fewer.")
		redirect(w, r, "/settings")
		return
	}
	if utf8.RuneCountInString(bio) > 1000 {
		s.setFlash(w, "Your bio must be 1,000 characters or fewer.")
		redirect(w, r, "/settings")
		return
	}
	if err := s.store.UpdateProfile(u.ID, display, bio); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save your profile.")
		return
	}
	s.setFlash(w, "Profile saved.")
	redirect(w, r, "/settings")
}

func (s *Server) passwordPost(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	current := r.PostFormValue("current_password")
	next := r.PostFormValue("new_password")
	confirm := r.PostFormValue("confirm_password")

	if !auth.CheckPassword(u.PasswordHash, current) {
		s.setFlash(w, "Your current password is not correct.")
		redirect(w, r, "/settings")
		return
	}
	if msg := auth.ValidatePassword(next); msg != "" {
		s.setFlash(w, msg)
		redirect(w, r, "/settings")
		return
	}
	if next != confirm {
		s.setFlash(w, "The two new passwords do not match.")
		redirect(w, r, "/settings")
		return
	}
	hash, err := auth.HashPassword(next)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not secure the new password.")
		return
	}
	if err := s.store.UpdatePassword(u.ID, hash); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save the new password.")
		return
	}
	if sess := sessionFrom(r); sess != nil {
		_ = s.store.DeleteOtherSessions(u.ID, sess.ID)
	}
	s.setFlash(w, "Password changed. Other sessions have been signed out.")
	redirect(w, r, "/settings")
}

func (s *Server) tokenPost(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	switch r.PostFormValue("action") {
	case "revoke":
		if err := s.store.SetAPIToken(u.ID, ""); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not revoke the token.")
			return
		}
		s.setFlash(w, "API token revoked.")
		redirect(w, r, "/settings")
	case "generate":
		token := auth.NewToken()
		if err := s.store.SetAPIToken(u.ID, auth.HashToken(token)); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not save the token.")
			return
		}
		fresh, err := s.store.GetUserByID(u.ID)
		if err != nil {
			fresh = u
		}
		page := s.newPage(r)
		page.Heading = "Settings"
		page.User = fresh
		page.Token = token
		page.Flash = "New API token created. Copy it now; it will not be shown again."
		page.Form = map[string]string{"display_name": fresh.DisplayName, "bio": fresh.Bio}
		page.RecoveryCodeCount, _ = s.store.CountRecoveryCodes(u.ID)
		s.render(w, r, http.StatusOK, "settings.html", page)
	default:
		redirect(w, r, "/settings")
	}
}
