package web

import (
	"encoding/xml"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"townsquare/internal/store"
)

const feedItemLimit = 30

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	Language      string    `xml:"language,omitempty"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	Generator     string    `xml:"generator,omitempty"`
	Items         []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	GUID        rssGUID `xml:"guid"`
	PubDate     string  `xml:"pubDate,omitempty"`
	Description string  `xml:"description,omitempty"`
	Comments    string  `xml:"comments,omitempty"`
}

type rssGUID struct {
	IsPermaLink bool   `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

func feedDate(v string) string {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC1123Z)
}

func (s *Server) buildFeed(title, link, description, base string, posts []store.Post) rssFeed {
	channel := rssChannel{
		Title:         title,
		Link:          link,
		Description:   description,
		Language:      "en",
		LastBuildDate: time.Now().UTC().Format(time.RFC1123Z),
		Generator:     "Townsquare " + Version,
	}
	if len(posts) > 0 {
		if d := feedDate(posts[0].CreatedAt); d != "" {
			channel.LastBuildDate = d
		}
	}
	for _, p := range posts {
		postURL := base + "/posts/" + p.ID
		body := p.BodyHTML
		if strings.TrimSpace(body) == "" {
			body = p.Title
		}
		channel.Items = append(channel.Items, rssItem{
			Title:       p.Title,
			Link:        postURL,
			GUID:        rssGUID{IsPermaLink: true, Value: postURL},
			PubDate:     feedDate(p.CreatedAt),
			Description: body,
			Comments:    postURL + "#comments",
		})
	}
	return rssFeed{Version: "2.0", Channel: channel}
}

func writeRSS(w http.ResponseWriter, feed rssFeed) {
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		return
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		log.Printf("write feed: %v", err)
	}
}

func (s *Server) siteFeed(w http.ResponseWriter, r *http.Request) {
	posts, err := s.store.ListPosts(feedItemLimit, 0)
	if err != nil {
		http.Error(w, "could not build feed", http.StatusInternalServerError)
		return
	}
	base := s.baseURL(r)
	feed := s.buildFeed(s.cfg.SiteName, base, s.cfg.Tagline, base, posts)
	writeRSS(w, feed)
}

func (s *Server) userFeed(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.GetUserByUsername(r.PathValue("username"))
	if err != nil || u.Disabled {
		http.NotFound(w, r)
		return
	}
	posts, err := s.store.ListUserPosts(u.ID, feedItemLimit, 0)
	if err != nil {
		http.Error(w, "could not build feed", http.StatusInternalServerError)
		return
	}
	description := u.Bio
	if strings.TrimSpace(description) == "" {
		description = "Posts by " + u.DisplayName + " on " + s.cfg.SiteName
	}
	base := s.baseURL(r)
	feed := s.buildFeed(
		s.cfg.SiteName+" :: "+u.DisplayName,
		base+"/users/"+u.Username,
		description,
		base,
		posts,
	)
	writeRSS(w, feed)
}

func (s *Server) randomPost(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.RandomPost()
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.setFlash(w, "No posts yet. Write the first one!")
			redirect(w, r, "/")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Could not find a random post.")
		return
	}
	redirect(w, r, "/posts/"+p.ID)
}
