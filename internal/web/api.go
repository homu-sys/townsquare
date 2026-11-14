package web

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"townsquare/internal/auth"
	"townsquare/internal/hashtags"
	"townsquare/internal/markdown"
	"townsquare/internal/store"
)

type apiAuthor struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	URL         string `json:"url"`
}

type apiUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
	URL         string `json:"url"`
}

type apiPost struct {
	ID           string       `json:"id"`
	URL          string       `json:"url"`
	Title        string       `json:"title"`
	Body         string       `json:"body"`
	BodyHTML     string       `json:"body_html"`
	Channel      string       `json:"channel"`
	Author       apiAuthor    `json:"author"`
	CreatedAt    string       `json:"created_at"`
	UpdatedAt    string       `json:"updated_at"`
	CommentCount int          `json:"comment_count"`
	Comments     []apiComment `json:"comments,omitempty"`
}

type apiComment struct {
	ID        string    `json:"id"`
	PostID    string    `json:"post_id"`
	URL       string    `json:"url"`
	Body      string    `json:"body"`
	BodyHTML  string    `json:"body_html"`
	Author    apiAuthor `json:"author"`
	CreatedAt string    `json:"created_at"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) apiNotFound(w http.ResponseWriter, r *http.Request) {
	apiError(w, http.StatusNotFound, "no such API endpoint")
}

func (s *Server) apiUserToDTO(u *store.User, base string) apiUser {
	return apiUser{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Bio:         u.Bio,
		Role:        u.Role,
		CreatedAt:   u.CreatedAt,
		URL:         base + "/users/" + u.Username,
	}
}

func (s *Server) apiAuthorOf(u *store.User, base string) apiAuthor {
	return apiAuthor{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		URL:         base + "/users/" + u.Username,
	}
}

func (s *Server) apiPostToDTO(p *store.Post, base string) apiPost {
	return apiPost{
		ID:           p.ID,
		URL:          base + "/posts/" + p.ID,
		Title:        p.Title,
		Body:         p.BodyMD,
		BodyHTML:     p.BodyHTML,
		Channel:      p.Channel,
		Author:       apiAuthor{ID: p.AuthorID, Username: p.AuthorUsername, DisplayName: p.AuthorDisplayName, URL: base + "/users/" + p.AuthorUsername},
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
		CommentCount: p.CommentCount,
	}
}

func (s *Server) apiCommentToDTO(c *store.Comment, base string) apiComment {
	return apiComment{
		ID:        c.ID,
		PostID:    c.PostID,
		URL:       base + "/posts/" + c.PostID + "#comment-" + c.ID,
		Body:      c.BodyMD,
		BodyHTML:  c.BodyHTML,
		Author:    apiAuthor{ID: c.AuthorID, Username: c.AuthorUsername, DisplayName: c.AuthorDisplayName, URL: base + "/users/" + c.AuthorUsername},
		CreatedAt: c.CreatedAt,
	}
}

func (s *Server) apiAuthUser(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	token := auth.BearerToken(r)
	if token == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="townsquare"`)
		apiError(w, http.StatusUnauthorized, "missing API token")
		return nil, false
	}
	u, err := s.store.GetUserByAPITokenHash(auth.HashToken(token))
	if err != nil || u.Disabled {
		w.Header().Set("WWW-Authenticate", `Bearer realm="townsquare"`)
		apiError(w, http.StatusUnauthorized, "invalid API token")
		return nil, false
	}
	return u, true
}

func (s *Server) apiSite(w http.ResponseWriter, r *http.Request) {
	users, posts, comments, err := s.store.Stats()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "could not read statistics")
		return
	}
	hits, _ := s.store.GetHits()
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     s.cfg.SiteName,
		"tagline":  s.cfg.Tagline,
		"base_url": s.baseURL(r),
		"version":  Version,
		"users":    users,
		"posts":    posts,
		"comments": comments,
		"hits":     hits,
	})
}

func (s *Server) apiUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := apiLimitOffset(r)
	list, err := s.store.ListActiveUsers(limit, offset)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "could not read users")
		return
	}
	base := s.baseURL(r)
	out := make([]apiUser, 0, len(list))
	for i := range list {
		out = append(out, s.apiUserToDTO(&list[i], base))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out, "limit": limit, "offset": offset})
}

func (s *Server) apiUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.GetUserByUsername(r.PathValue("username"))
	if err != nil || u.Disabled {
		apiError(w, http.StatusNotFound, "no such user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": s.apiUserToDTO(u, s.baseURL(r))})
}

func (s *Server) apiUserPosts(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.GetUserByUsername(r.PathValue("username"))
	if err != nil || u.Disabled {
		apiError(w, http.StatusNotFound, "no such user")
		return
	}
	limit, offset := apiLimitOffset(r)
	posts, err := s.store.ListUserPosts(u.ID, limit, offset)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "could not read posts")
		return
	}
	base := s.baseURL(r)
	out := make([]apiPost, 0, len(posts))
	for i := range posts {
		out = append(out, s.apiPostToDTO(&posts[i], base))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":   s.apiUserToDTO(u, base),
		"posts":  out,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Server) apiPosts(w http.ResponseWriter, r *http.Request) {
	limit, offset := apiLimitOffset(r)
	posts, err := s.store.ListPosts(limit, offset)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "could not read posts")
		return
	}
	base := s.baseURL(r)
	out := make([]apiPost, 0, len(posts))
	for i := range posts {
		out = append(out, s.apiPostToDTO(&posts[i], base))
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": out, "limit": limit, "offset": offset})
}

func (s *Server) apiPost(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPost(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			apiError(w, http.StatusNotFound, "no such post")
			return
		}
		apiError(w, http.StatusInternalServerError, "could not read post")
		return
	}
	if p.Channel == "nsfw" {
		apiError(w, http.StatusNotFound, "no such post")
		return
	}
	comments, err := s.store.ListComments(p.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "could not read comments")
		return
	}
	base := s.baseURL(r)
	dto := s.apiPostToDTO(p, base)
	dto.Comments = make([]apiComment, 0, len(comments))
	for i := range comments {
		dto.Comments = append(dto.Comments, s.apiCommentToDTO(&comments[i], base))
	}
	writeJSON(w, http.StatusOK, map[string]any{"post": dto})
}

func (s *Server) apiComments(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPost(r.PathValue("id"))
	if err != nil || p.Channel == "nsfw" {
		apiError(w, http.StatusNotFound, "no such post")
		return
	}
	comments, err := s.store.ListComments(p.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "could not read comments")
		return
	}
	base := s.baseURL(r)
	out := make([]apiComment, 0, len(comments))
	for i := range comments {
		out = append(out, s.apiCommentToDTO(&comments[i], base))
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": out})
}

func (s *Server) apiCreatePost(w http.ResponseWriter, r *http.Request) {
	u, ok := s.apiAuthUser(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	title := strings.TrimSpace(in.Title)
	body := strings.TrimSpace(in.Body)
	channel := strings.TrimSpace(in.Channel)
	if channel == "" {
		channel = "general"
	}
	if msg := validatePost(title, body); msg != "" {
		apiError(w, http.StatusBadRequest, msg)
		return
	}
	ch, ok := channelBySlug(channel)
	if !ok {
		apiError(w, http.StatusBadRequest, "invalid channel")
		return
	}
	if ch.NSFW {
		apiError(w, http.StatusBadRequest, "NSFW posts must be created on the website")
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
		apiError(w, http.StatusInternalServerError, "could not save post")
		return
	}
	if err := s.store.SetPostTags(p.ID, hashtags.Extract(body)); err != nil {
		log.Printf("set tags for post %s: %v", p.ID, err)
	}
	full, err := s.store.GetPost(p.ID)
	if err != nil {
		full = p
	}
	writeJSON(w, http.StatusCreated, map[string]any{"post": s.apiPostToDTO(full, s.baseURL(r))})
}

func (s *Server) apiCreateComment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.apiAuthUser(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetPost(r.PathValue("id"))
	if err != nil || p.Channel == "nsfw" {
		apiError(w, http.StatusNotFound, "no such post")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	body := strings.TrimSpace(in.Body)
	if msg := validateComment(body); msg != "" {
		apiError(w, http.StatusBadRequest, msg)
		return
	}
	c := &store.Comment{
		PostID:   p.ID,
		AuthorID: u.ID,
		BodyMD:   body,
		BodyHTML: markdown.Render(body),
	}
	if err := s.store.CreateComment(c); err != nil {
		apiError(w, http.StatusInternalServerError, "could not save comment")
		return
	}
	full, err := s.store.GetComment(c.ID)
	if err != nil {
		full = c
	}
	writeJSON(w, http.StatusCreated, map[string]any{"comment": s.apiCommentToDTO(full, s.baseURL(r))})
}

func (s *Server) apiDeletePost(w http.ResponseWriter, r *http.Request) {
	u, ok := s.apiAuthUser(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetPost(r.PathValue("id"))
	if err != nil {
		apiError(w, http.StatusNotFound, "no such post")
		return
	}
	if u.Role != "admin" && u.ID != p.AuthorID {
		apiError(w, http.StatusForbidden, "you cannot delete that post")
		return
	}
	if err := s.store.DeletePost(p.ID); err != nil {
		apiError(w, http.StatusInternalServerError, "could not delete post")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
