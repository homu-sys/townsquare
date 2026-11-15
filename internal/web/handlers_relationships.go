package web

import (
	"net/http"
	"net/url"

	"townsquare/internal/store"
)

func (s *Server) userMute(w http.ResponseWriter, r *http.Request) {
	s.relationshipAction(w, r, func(viewer, target *store.User) error {
		return s.store.MuteUser(viewer.ID, target.ID)
	}, "You muted @"+r.PathValue("username")+".")
}

func (s *Server) userUnmute(w http.ResponseWriter, r *http.Request) {
	s.relationshipAction(w, r, func(viewer, target *store.User) error {
		return s.store.UnmuteUser(viewer.ID, target.ID)
	}, "You unmuted @"+r.PathValue("username")+".")
}

func (s *Server) userBlock(w http.ResponseWriter, r *http.Request) {
	s.relationshipAction(w, r, func(viewer, target *store.User) error {
		return s.store.BlockUser(viewer.ID, target.ID)
	}, "You blocked @"+r.PathValue("username")+". Their posts are hidden and direct messages are closed.")
}

func (s *Server) userUnblock(w http.ResponseWriter, r *http.Request) {
	s.relationshipAction(w, r, func(viewer, target *store.User) error {
		return s.store.UnblockUser(viewer.ID, target.ID)
	}, "You unblocked @"+r.PathValue("username")+".")
}

func (s *Server) relationshipAction(w http.ResponseWriter, r *http.Request, apply func(viewer, target *store.User) error, flash string) {
	viewer, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	target, err := s.store.GetUserByUsername(r.PathValue("username"))
	if err != nil || target.Disabled || target.ID == viewer.ID {
		s.renderError(w, r, http.StatusNotFound, "No such person lives here.")
		return
	}
	if err := apply(viewer, target); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not update that relationship.")
		return
	}
	s.setFlash(w, flash)
	if r.PostFormValue("next") == "/settings" {
		redirect(w, r, "/settings")
		return
	}
	redirect(w, r, "/users/"+url.PathEscape(target.Username))
}
