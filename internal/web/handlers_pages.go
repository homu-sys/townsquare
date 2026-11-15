package web

import (
	"errors"
	"net/http"
	"net/url"

	"townsquare/internal/store"
)

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read the database.")
		return
	}
	if n == 0 {
		if s.cfg.AllowSetup {
			redirect(w, r, "/setup")
			return
		}
		s.renderError(w, r, http.StatusServiceUnavailable, "This town has not been set up yet.")
		return
	}

	hits, err := s.store.Visit(s.clientIP(r))
	if err != nil {
		hits, _ = s.store.GetHits()
	}
	news, _ := s.store.ListNews()
	const limit = 20
	pageNum := pageParam(r)
	posts, err := s.store.ListPostsFor(viewerID(r), limit+1, (pageNum-1)*limit)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read posts.")
		return
	}
	posts, hasNext := trimPage(posts, limit)
	s.markPosts(r, posts)

	page := s.newPage(r)
	page.Posts = posts
	page.News = news
	page.Hits = hits
	page.PageNum = pageNum
	if pageNum == 1 {
		page.RemotePosts, _ = s.store.ListRemotePosts(10)
	}
	if pageNum > 1 {
		page.Prev = pageURL("/", pageNum-1)
	}
	if hasNext {
		page.Next = pageURL("/", pageNum+1)
	}
	s.render(w, r, http.StatusOK, "home.html", page)
}

func (s *Server) about(w http.ResponseWriter, r *http.Request) {
	page := s.newPage(r)
	page.Heading = "About"
	s.render(w, r, http.StatusOK, "about.html", page)
}

func (s *Server) usersList(w http.ResponseWriter, r *http.Request) {
	const limit = 20
	pageNum := pageParam(r)
	users, err := s.store.ListActiveUsers(limit+1, (pageNum-1)*limit)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read the user list.")
		return
	}
	users, hasNext := trimPage(users, limit)

	page := s.newPage(r)
	page.Heading = "People"
	page.Users = users
	page.PageNum = pageNum
	if pageNum > 1 {
		page.Prev = pageURL("/users", pageNum-1)
	}
	if hasNext {
		page.Next = pageURL("/users", pageNum+1)
	}
	s.render(w, r, http.StatusOK, "users.html", page)
}

func (s *Server) userProfile(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	profile, err := s.store.GetUserByUsername(username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "No such person lives here.")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Could not read that profile.")
		return
	}
	viewer := userFrom(r)
	if profile.Disabled && (viewer == nil || !viewer.IsStaff()) {
		s.renderError(w, r, http.StatusNotFound, "No such person lives here.")
		return
	}

	muted, blocked := false, false
	if viewer != nil && viewer.ID != profile.ID {
		muted, _ = s.store.IsMuted(viewer.ID, profile.ID)
		blocked, _ = s.store.IsBlocked(viewer.ID, profile.ID)
	}

	const limit = 20
	pageNum := pageParam(r)
	var posts []store.Post
	var hasNext bool
	if !blocked {
		posts, err = s.store.ListUserPosts(profile.ID, limit+1, (pageNum-1)*limit)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not read posts.")
			return
		}
		posts, hasNext = trimPage(posts, limit)
		s.markPosts(r, posts)
	}

	base := "/users/" + url.PathEscape(profile.Username)
	page := s.newPage(r)
	page.Heading = profile.DisplayName
	page.Profile = profile
	page.Posts = posts
	page.Muted = muted
	page.Blocked = blocked
	page.PageNum = pageNum
	page.OGTitle = profile.DisplayName
	page.OGType = "profile"
	page.Description = summarizeHTML(profile.Bio, 200)
	if page.Description == "" {
		page.Description = "Posts by " + profile.DisplayName + " on " + s.cfg.SiteName
	}
	if pageNum > 1 {
		page.Prev = pageURL(base, pageNum-1)
	}
	if hasNext {
		page.Next = pageURL(base, pageNum+1)
	}
	s.render(w, r, http.StatusOK, "profile.html", page)
}
