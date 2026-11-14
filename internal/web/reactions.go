package web

import (
	"errors"
	"net/http"
	"strings"

	"townsquare/internal/store"
)

var emojiPalette = []string{"👍", "❤️", "😂", "🎉", "😢", "👀"}

type ReactionView struct {
	TargetType string
	TargetID   string
	Next       string
	Items      []store.Reaction
	LoggedIn   bool
	CSRFToken  string
}

type MessageView struct {
	store.DirectMessage
	Reactions ReactionView
}

func (s *Server) reactionView(r *http.Request, targetType, targetID, next string) ReactionView {
	viewerID := ""
	u := userFrom(r)
	if u != nil {
		viewerID = u.ID
	}
	items, _ := s.store.ListReactions(targetType, targetID, viewerID)
	return ReactionView{
		TargetType: targetType,
		TargetID:   targetID,
		Next:       next,
		Items:      items,
		LoggedIn:   u != nil,
		CSRFToken:  csrfFrom(r),
	}
}

func validReaction(emoji string) bool {
	for _, choice := range emojiPalette {
		if emoji == choice {
			return true
		}
	}
	return false
}

func (s *Server) reactionToggle(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	targetType := r.PostFormValue("target_type")
	targetID := strings.TrimSpace(r.PostFormValue("target_id"))
	emoji := r.PostFormValue("emoji")
	next := safeNext(r.PostFormValue("next"))
	if targetID == "" || !validReaction(emoji) {
		s.renderError(w, r, http.StatusBadRequest, "That reaction is not available.")
		return
	}

	var err error
	switch targetType {
	case "post":
		_, err = s.store.GetPost(targetID)
	case "comment":
		_, err = s.store.GetComment(targetID)
	case "dm":
		var m *store.DirectMessage
		m, err = s.store.GetDirectMessage(targetID)
		if err == nil && (m.Blocked || (m.SenderID != u.ID && m.RecipientID != u.ID)) {
			err = store.ErrNotFound
		}
	default:
		s.renderError(w, r, http.StatusBadRequest, "That reaction target is not supported.")
		return
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "That page does not exist.")
		} else {
			s.renderError(w, r, http.StatusInternalServerError, "Could not save that reaction.")
		}
		return
	}
	if err := s.store.ToggleReaction(u.ID, targetType, targetID, emoji); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save that reaction.")
		return
	}
	redirect(w, r, next)
}
