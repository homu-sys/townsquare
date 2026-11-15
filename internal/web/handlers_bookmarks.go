package web

import (
	"errors"
	"net/http"
	"strings"

	"townsquare/internal/store"
)

func (s *Server) bookmarksPage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	const limit = 20
	pageNum := pageParam(r)
	posts, err := s.store.ListBookmarks(u.ID, s.canViewNSFW(r), limit+1, (pageNum-1)*limit)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read your bookmarks.")
		return
	}
	posts, hasNext := trimPage(posts, limit)
	s.markPosts(r, posts)

	page := s.newPage(r)
	page.Heading = "Bookmarks"
	page.Posts = posts
	page.PageNum = pageNum
	page.Description = "Posts you saved on " + s.cfg.SiteName
	if pageNum > 1 {
		page.Prev = pageURL("/bookmarks", pageNum-1)
	}
	if hasNext {
		page.Next = pageURL("/bookmarks", pageNum+1)
	}
	s.render(w, r, http.StatusOK, "bookmarks.html", page)
}

func (s *Server) postBookmark(w http.ResponseWriter, r *http.Request) {
	s.setBookmark(w, r, true)
}

func (s *Server) postUnbookmark(w http.ResponseWriter, r *http.Request) {
	s.setBookmark(w, r, false)
}

func (s *Server) setBookmark(w http.ResponseWriter, r *http.Request, add bool) {
	u, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	p, err := s.store.GetPost(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "That post does not exist.")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Could not read that post.")
		return
	}
	if p.Channel == "nsfw" && !s.canViewNSFW(r) {
		s.renderError(w, r, http.StatusForbidden, "Confirm the 18+ warning before bookmarking that post.")
		return
	}
	if add {
		err = s.store.BookmarkPost(u.ID, p.ID)
	} else {
		err = s.store.UnbookmarkPost(u.ID, p.ID)
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not update your bookmarks.")
		return
	}
	if add {
		s.setFlash(w, "Bookmark saved.")
	} else {
		s.setFlash(w, "Bookmark removed.")
	}
	next := strings.TrimSpace(r.PostFormValue("next"))
	if next == "" {
		next = "/posts/" + p.ID
	} else {
		next = safeNext(next)
	}
	redirect(w, r, next)
}
