package web

import (
	"net/http"
	"strings"
)

const adultCookieName = "ts_adult"

type ChannelInfo struct {
	Slug        string
	Name        string
	Description string
	NSFW        bool
	Count       int
}

var channelCatalog = []ChannelInfo{
	{Slug: "general", Name: "General", Description: "Everything that does not fit elsewhere."},
	{Slug: "memes", Name: "Memes", Description: "Jokes, images, and internet debris."},
	{Slug: "daily", Name: "Daily", Description: "What are you up to today?"},
	{Slug: "nsfw", Name: "NSFW (18+)", Description: "Adult and graphic content. 18+ only.", NSFW: true},
}

func channelBySlug(slug string) (ChannelInfo, bool) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	for _, ch := range channelCatalog {
		if ch.Slug == slug {
			return ch, true
		}
	}
	return ChannelInfo{}, false
}

func (s *Server) channelViews() []ChannelInfo {
	out := make([]ChannelInfo, len(channelCatalog))
	copy(out, channelCatalog)
	for i := range out {
		if n, err := s.store.CountPostsByChannel(out[i].Slug); err == nil {
			out[i].Count = n
		}
	}
	return out
}

func (s *Server) adultOK(r *http.Request) bool {
	c, err := r.Cookie(adultCookieName)
	return err == nil && c.Value == "1"
}

func (s *Server) canViewNSFW(r *http.Request) bool {
	if s.adultOK(r) {
		return true
	}
	u := userFrom(r)
	return u != nil && u.Role == "admin"
}

func (s *Server) setAdultOK(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     adultCookieName,
		Value:    "1",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})
}

func (s *Server) renderNSFWWarning(w http.ResponseWriter, r *http.Request, next string) {
	page := s.newPage(r)
	page.Heading = "18+ warning"
	page.NoIndex = true
	page.AdultNext = next
	s.render(w, r, http.StatusOK, "nsfw_warning.html", page)
}

func (s *Server) channelsList(w http.ResponseWriter, r *http.Request) {
	page := s.newPage(r)
	page.Heading = "Channels"
	page.Description = "Browse channels on " + s.cfg.SiteName
	page.Channels = s.channelViews()
	s.render(w, r, http.StatusOK, "channels.html", page)
}

func (s *Server) channelView(w http.ResponseWriter, r *http.Request) {
	ch, ok := channelBySlug(r.PathValue("channel"))
	if !ok {
		s.renderError(w, r, http.StatusNotFound, "No such channel.")
		return
	}
	if ch.NSFW && !s.canViewNSFW(r) {
		s.renderNSFWWarning(w, r, "/c/"+ch.Slug)
		return
	}

	const limit = 20
	pageNum := pageParam(r)
	posts, err := s.store.ListPostsByChannel(ch.Slug, limit+1, (pageNum-1)*limit)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read posts.")
		return
	}
	posts, hasNext := trimPage(posts, limit)

	base := "/c/" + ch.Slug
	page := s.newPage(r)
	page.Heading = ch.Name
	page.Description = ch.Description
	page.Channel = ch.Slug
	page.ChannelName = ch.Name
	page.ChannelDescription = ch.Description
	page.Channels = s.channelViews()
	page.Posts = posts
	page.PageNum = pageNum
	page.NoIndex = ch.NSFW
	if pageNum > 1 {
		page.Prev = pageURL(base, pageNum-1)
	}
	if hasNext {
		page.Next = pageURL(base, pageNum+1)
	}
	s.render(w, r, http.StatusOK, "channel.html", page)
}

func (s *Server) nsfwConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(w, r) {
		return
	}
	next := safeNext(r.PostFormValue("next"))
	if next == "/" {
		next = "/c/nsfw"
	}
	s.setAdultOK(w)
	redirect(w, r, next)
}
