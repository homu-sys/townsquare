package web

import (
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"townsquare/internal/markdown"
	"townsquare/internal/store"
)

func validateDirectMessage(body string) string {
	if strings.TrimSpace(body) == "" {
		return "Write a message first."
	}
	if utf8.RuneCountInString(body) > 5000 {
		return "Messages must be 5,000 characters or fewer."
	}
	return ""
}

func (s *Server) dmBlockReason(body string) string {
	lower := strings.ToLower(body)
	var matched []string
	for _, term := range s.cfg.DMBlocklist {
		if strings.Contains(lower, term) {
			matched = append(matched, term)
		}
	}
	return strings.Join(matched, ", ")
}

func (s *Server) messagesInbox(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	conversations, err := s.store.ListConversations(u.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read your messages.")
		return
	}
	page := s.newPage(r)
	page.Heading = "Messages"
	page.Conversations = conversations
	s.render(w, r, http.StatusOK, "messages.html", page)
}

func (s *Server) messageThread(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	peer, ok := s.directMessagePeer(w, r, u)
	if !ok {
		return
	}
	s.renderMessageThread(w, r, http.StatusOK, u, peer, "", "")
}

func (s *Server) messageSend(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	peer, ok := s.directMessagePeer(w, r, u)
	if !ok {
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	if msg := validateDirectMessage(body); msg != "" {
		s.renderMessageThread(w, r, http.StatusBadRequest, u, peer, body, msg)
		return
	}

	m := &store.DirectMessage{
		SenderID:    u.ID,
		RecipientID: peer.ID,
		BodyMD:      body,
	}
	if reason := s.dmBlockReason(body); reason != "" {
		m.Blocked = true
		m.BlockReason = reason
	} else {
		m.BodyHTML = markdown.Render(body)
	}
	if err := s.store.CreateDirectMessage(m); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not send that message.")
		return
	}
	if m.Blocked {
		s.setFlash(w, "Your message was blocked by moderation and was not sent.")
	} else {
		s.setFlash(w, "Message sent.")
	}
	redirect(w, r, "/messages/"+url.PathEscape(peer.Username))
}

func (s *Server) directMessagePeer(w http.ResponseWriter, r *http.Request, u *store.User) (*store.User, bool) {
	peer, err := s.store.GetUserByUsername(r.PathValue("username"))
	if err != nil || peer.Disabled || peer.ID == u.ID {
		s.renderError(w, r, http.StatusNotFound, "No such person lives here.")
		return nil, false
	}
	return peer, true
}

func (s *Server) renderMessageThread(w http.ResponseWriter, r *http.Request, status int, u, peer *store.User, body, errMsg string) {
	messages, err := s.store.ListDirectMessages(u.ID, peer.ID)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read this conversation.")
		return
	}
	page := s.newPage(r)
	page.Heading = "Messages with " + peer.DisplayName
	page.Peer = peer
	next := "/messages/" + url.PathEscape(peer.Username)
	views := make([]MessageView, 0, len(messages))
	for _, m := range messages {
		views = append(views, MessageView{
			DirectMessage: m,
			Reactions:     s.reactionView(r, "dm", m.ID, next),
		})
	}
	page.Messages = views
	page.Form = map[string]string{"body": body}
	page.Error = errMsg
	s.render(w, r, status, "messages_thread.html", page)
}

func (s *Server) adminBlockedDMs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	const limit = 50
	pageNum := pageParam(r)
	messages, err := s.store.ListBlockedDirectMessages(limit+1, (pageNum-1)*limit)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read blocked messages.")
		return
	}
	messages, hasNext := trimPage(messages, limit)

	page := s.newPage(r)
	page.Heading = "Blocked messages"
	page.BlockedDMs = messages
	page.PageNum = pageNum
	if pageNum > 1 {
		page.Prev = pageURL("/admin/dms", pageNum-1)
	}
	if hasNext {
		page.Next = pageURL("/admin/dms", pageNum+1)
	}
	s.render(w, r, http.StatusOK, "admin_dms.html", page)
}

func (s *Server) adminBlockedDMDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	if err := s.store.DeleteBlockedDirectMessage(r.PathValue("id")); err != nil {
		s.setFlash(w, "Could not delete that blocked message.")
	} else {
		s.setFlash(w, "Blocked message deleted.")
	}
	redirect(w, r, "/admin/dms")
}
