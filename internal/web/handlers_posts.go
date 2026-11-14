package web

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"townsquare/internal/hashtags"
	"townsquare/internal/markdown"
	"townsquare/internal/store"
)

func validatePost(title, body string) string {
	if strings.TrimSpace(title) == "" {
		return "Give your post a subject."
	}
	if utf8.RuneCountInString(title) > 200 {
		return "The subject must be 200 characters or fewer."
	}
	if strings.TrimSpace(body) == "" {
		return "Write something in the body."
	}
	if utf8.RuneCountInString(body) > 50000 {
		return "The body must be 50,000 characters or fewer."
	}
	return ""
}

func validateComment(body string) string {
	if strings.TrimSpace(body) == "" {
		return "Write something before posting your comment."
	}
	if utf8.RuneCountInString(body) > 5000 {
		return "Comments must be 5,000 characters or fewer."
	}
	return ""
}

func canManagePost(u *store.User, p *store.Post) bool {
	return u != nil && (u.Role == "admin" || u.ID == p.AuthorID)
}

func (s *Server) postNewForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUser(w, r); !ok {
		return
	}
	page := s.newPage(r)
	page.Heading = "Write a new post"
	page.FormAction = "/new"
	page.Channels = s.channelViews()
	page.Form["channel"] = "general"
	s.render(w, r, http.StatusOK, "new.html", page)
}

func (s *Server) postCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	title := strings.TrimSpace(r.PostFormValue("title"))
	body := strings.TrimSpace(r.PostFormValue("body"))
	channel := strings.TrimSpace(r.PostFormValue("channel"))
	if channel == "" {
		channel = "general"
	}

	page := s.newPage(r)
	page.Heading = "Write a new post"
	page.FormAction = "/new"
	page.Channels = s.channelViews()
	page.Form = map[string]string{"title": title, "body": body, "channel": channel}
	if msg := validatePost(title, body); msg != "" {
		page.Error = msg
		s.render(w, r, http.StatusBadRequest, "new.html", page)
		return
	}
	ch, ok := channelBySlug(channel)
	if !ok {
		page.Error = "Choose a valid channel."
		s.render(w, r, http.StatusBadRequest, "new.html", page)
		return
	}
	if ch.NSFW && !s.canViewNSFW(r) {
		page.Error = "Open the NSFW channel and confirm the 18+ warning before posting there."
		s.render(w, r, http.StatusForbidden, "new.html", page)
		return
	}
	p := &store.Post{
		AuthorID: u.ID,
		Channel:  ch.Slug,
		Title:    title,
		BodyMD:   body,
		BodyHTML: markdown.Render(body),
	}
	if err := s.store.CreatePost(p); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save that post.")
		return
	}
	if err := s.store.SetPostTags(p.ID, hashtags.Extract(body)); err != nil {
		log.Printf("set tags for post %s: %v", p.ID, err)
	}
	s.setFlash(w, "Your post is up.")
	redirect(w, r, "/posts/"+p.ID)
}

func (s *Server) postView(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.store.GetPost(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "That post does not exist.")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Could not read that post.")
		return
	}
	if p.Channel == "nsfw" && !s.canViewNSFW(r) {
		s.renderNSFWWarning(w, r, "/posts/"+p.ID)
		return
	}
	comments, err := s.store.ListComments(id)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read the comments.")
		return
	}
	u := userFrom(r)
	page := s.newPage(r)
	views := make([]CommentView, 0, len(comments))
	for _, c := range comments {
		canDelete := u != nil && (u.Role == "admin" || u.ID == c.AuthorID || u.ID == p.AuthorID)
		views = append(views, CommentView{
			Comment:   c,
			CanDelete: canDelete,
			Reactions: s.reactionView(r, "comment", c.ID, "/posts/"+p.ID+"#comment-"+c.ID),
		})
	}
	page.Heading = p.Title
	page.Post = p
	page.Comments = views
	page.PostReactions = s.reactionView(r, "post", p.ID, "/posts/"+p.ID)
	page.PostTags, _ = s.store.ListPostTags(p.ID)
	page.CanEdit = canManagePost(u, p)
	page.CanDelete = page.CanEdit
	page.OGTitle = p.Title
	page.OGType = "article"
	if p.Channel == "nsfw" {
		page.NoIndex = true
	}
	if summary := summarizeHTML(p.BodyHTML, 200); summary != "" {
		page.Description = summary
	}
	s.render(w, r, http.StatusOK, "post.html", page)
}

func (s *Server) postEditForm(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetPost(r.PathValue("id"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That post does not exist.")
		return
	}
	if !canManagePost(u, p) {
		s.renderError(w, r, http.StatusForbidden, "You cannot edit that post.")
		return
	}
	if p.Channel == "nsfw" && !s.canViewNSFW(r) {
		s.renderNSFWWarning(w, r, "/posts/"+p.ID+"/edit")
		return
	}
	page := s.newPage(r)
	page.Heading = "Edit post"
	page.FormAction = "/posts/" + p.ID + "/edit"
	page.Channels = s.channelViews()
	page.Form = map[string]string{"title": p.Title, "body": p.BodyMD, "channel": p.Channel}
	page.Post = p
	page.CanDelete = true
	s.render(w, r, http.StatusOK, "edit.html", page)
}

func (s *Server) postEdit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	p, err := s.store.GetPost(r.PathValue("id"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That post does not exist.")
		return
	}
	if !canManagePost(u, p) {
		s.renderError(w, r, http.StatusForbidden, "You cannot edit that post.")
		return
	}
	if p.Channel == "nsfw" && !s.canViewNSFW(r) {
		s.renderNSFWWarning(w, r, "/posts/"+p.ID+"/edit")
		return
	}
	title := strings.TrimSpace(r.PostFormValue("title"))
	body := strings.TrimSpace(r.PostFormValue("body"))
	channel := strings.TrimSpace(r.PostFormValue("channel"))
	if channel == "" {
		channel = p.Channel
	}

	page := s.newPage(r)
	page.Heading = "Edit post"
	page.FormAction = "/posts/" + p.ID + "/edit"
	page.Channels = s.channelViews()
	page.Form = map[string]string{"title": title, "body": body, "channel": channel}
	page.Post = p
	page.CanDelete = true
	if msg := validatePost(title, body); msg != "" {
		page.Error = msg
		s.render(w, r, http.StatusBadRequest, "edit.html", page)
		return
	}
	ch, ok := channelBySlug(channel)
	if !ok {
		page.Error = "Choose a valid channel."
		s.render(w, r, http.StatusBadRequest, "edit.html", page)
		return
	}
	if ch.NSFW && !s.canViewNSFW(r) {
		page.Error = "Open the NSFW channel and confirm the 18+ warning before posting there."
		s.render(w, r, http.StatusForbidden, "edit.html", page)
		return
	}
	if err := s.store.UpdatePost(p.ID, title, body, markdown.Render(body), ch.Slug); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save that post.")
		return
	}
	if err := s.store.SetPostTags(p.ID, hashtags.Extract(body)); err != nil {
		log.Printf("set tags for post %s: %v", p.ID, err)
	}
	s.setFlash(w, "Post updated.")
	redirect(w, r, "/posts/"+p.ID)
}

func (s *Server) postDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	p, err := s.store.GetPost(r.PathValue("id"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That post does not exist.")
		return
	}
	if !canManagePost(u, p) {
		s.renderError(w, r, http.StatusForbidden, "You cannot delete that post.")
		return
	}
	if err := s.store.DeletePost(p.ID); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not delete that post.")
		return
	}
	s.setFlash(w, "Post deleted.")
	redirect(w, r, "/")
}

func (s *Server) commentCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	p, err := s.store.GetPost(r.PathValue("id"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That post does not exist.")
		return
	}
	if p.Channel == "nsfw" && !s.canViewNSFW(r) {
		s.setFlash(w, "Confirm the 18+ warning before commenting on that post.")
		redirect(w, r, "/posts/"+p.ID)
		return
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	if msg := validateComment(body); msg != "" {
		s.setFlash(w, msg)
		redirect(w, r, "/posts/"+p.ID+"#comments")
		return
	}
	c := &store.Comment{
		PostID:   p.ID,
		AuthorID: u.ID,
		BodyMD:   body,
		BodyHTML: markdown.Render(body),
	}
	if err := s.store.CreateComment(c); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save that comment.")
		return
	}
	s.setFlash(w, "Comment added.")
	redirect(w, r, "/posts/"+p.ID+"#comments")
}

func (s *Server) commentDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	c, err := s.store.GetComment(r.PathValue("id"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That comment does not exist.")
		return
	}
	p, err := s.store.GetPost(c.PostID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That post does not exist.")
		return
	}
	if !(u.Role == "admin" || u.ID == c.AuthorID || u.ID == p.AuthorID) {
		s.renderError(w, r, http.StatusForbidden, "You cannot delete that comment.")
		return
	}
	if err := s.store.DeleteComment(c.ID); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not delete that comment.")
		return
	}
	s.setFlash(w, "Comment deleted.")
	redirect(w, r, "/posts/"+p.ID+"#comments")
}
