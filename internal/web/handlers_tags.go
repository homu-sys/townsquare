package web

import (
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) tagsList(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(200)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read the hashtags.")
		return
	}
	page := s.newPage(r)
	page.Heading = "Hashtags"
	page.Tags = tags
	page.Description = "Hashtags used in posts on " + s.cfg.SiteName
	s.render(w, r, http.StatusOK, "tags.html", page)
}

func (s *Server) tagView(w http.ResponseWriter, r *http.Request) {
	tag := strings.ToLower(strings.TrimSpace(r.PathValue("tag")))
	if tag == "" {
		s.renderError(w, r, http.StatusNotFound, "No such hashtag.")
		return
	}
	const limit = 20
	pageNum := pageParam(r)
	posts, err := s.store.ListPostsByTagFor(viewerID(r), tag, limit+1, (pageNum-1)*limit)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read posts for that hashtag.")
		return
	}
	posts, hasNext := trimPage(posts, limit)
	s.markPosts(r, posts)

	base := "/tags/" + url.PathEscape(tag)
	page := s.newPage(r)
	page.Heading = "#" + tag
	page.Tag = tag
	page.Posts = posts
	page.PageNum = pageNum
	page.OGTitle = "#" + tag
	page.Description = "Posts tagged #" + tag + " on " + s.cfg.SiteName
	if pageNum > 1 {
		page.Prev = pageURL(base, pageNum-1)
	}
	if hasNext {
		page.Next = pageURL(base, pageNum+1)
	}
	s.render(w, r, http.StatusOK, "tag.html", page)
}
