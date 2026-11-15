package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"townsquare/internal/store"
)

const (
	federationFeedPath = "/federation/latest.json"
	federationMaxPosts = 100
	federationMaxBody  = 2 << 20
)

type federationFeed struct {
	Site        federationSite   `json:"site"`
	GeneratedAt string           `json:"generated_at"`
	Posts       []federationPost `json:"posts"`
}

type federationSite struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Tagline string `json:"tagline"`
}

type federationPost struct {
	ID           string           `json:"id"`
	URL          string           `json:"url"`
	Title        string           `json:"title"`
	Body         string           `json:"body"`
	Channel      string           `json:"channel"`
	Author       federationAuthor `json:"author"`
	CreatedAt    string           `json:"created_at"`
	UpdatedAt    string           `json:"updated_at"`
	CommentCount int              `json:"comment_count"`
}

type federationAuthor struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	URL         string `json:"url"`
}

func (s *Server) federationLatest(w http.ResponseWriter, r *http.Request) {
	posts, err := s.store.ListPosts(50, 0)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "could not read posts")
		return
	}
	base := s.baseURL(r)
	feed := federationFeed{
		Site: federationSite{
			Name:    s.cfg.SiteName,
			URL:     base,
			Tagline: s.cfg.Tagline,
		},
		GeneratedAt: store.Now(),
	}
	for i := range posts {
		p := &posts[i]
		feed.Posts = append(feed.Posts, federationPost{
			ID:      p.ID,
			URL:     base + "/posts/" + p.ID,
			Title:   p.Title,
			Body:    p.BodyMD,
			Channel: p.Channel,
			Author: federationAuthor{
				Username:    p.AuthorUsername,
				DisplayName: p.AuthorDisplayName,
				URL:         base + "/users/" + p.AuthorUsername,
			},
			CreatedAt:    p.CreatedAt,
			UpdatedAt:    p.UpdatedAt,
			CommentCount: p.CommentCount,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	if err := json.NewEncoder(w).Encode(feed); err != nil {
		log.Printf("federation: encode feed: %v", err)
	}
}

func (s *Server) RunFederation(ctx context.Context) {
	if len(s.cfg.FederationInstances) == 0 {
		return
	}
	s.SyncFederation(ctx)
	interval := s.cfg.FederationInterval
	if interval < time.Minute {
		interval = 10 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SyncFederation(ctx)
		}
	}
}

func (s *Server) SyncFederation(ctx context.Context) {
	for _, instance := range s.cfg.FederationInstances {
		if err := s.fetchFederation(ctx, instance); err != nil {
			log.Printf("federation: %s: %v", instance, err)
		}
	}
}

func (s *Server) fetchFederation(ctx context.Context, raw string) error {
	feedURL, err := federationFeedURL(raw)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("redirect to unsupported scheme")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Townsquare/"+Version)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	var feed federationFeed
	if err := json.NewDecoder(io.LimitReader(resp.Body, federationMaxBody)).Decode(&feed); err != nil {
		return fmt.Errorf("decode feed: %w", err)
	}
	if len(feed.Posts) > federationMaxPosts {
		feed.Posts = feed.Posts[:federationMaxPosts]
	}

	instance := feedURL.Scheme + "://" + feedURL.Host
	siteName := truncateRunes(strings.TrimSpace(feed.Site.Name), 100)
	if siteName == "" {
		siteName = feedURL.Host
	}
	now := store.Now()
	seen := make(map[string]bool, len(feed.Posts))
	out := make([]store.RemotePost, 0, len(feed.Posts))
	for _, p := range feed.Posts {
		id := strings.TrimSpace(p.ID)
		if id == "" || seen[id] {
			continue
		}
		postURL := strings.TrimSpace(p.URL)
		if !isHTTPURL(postURL) {
			continue
		}
		title := truncateRunes(strings.TrimSpace(p.Title), 300)
		if title == "" {
			continue
		}
		channel := truncateRunes(strings.TrimSpace(p.Channel), 40)
		if channel == "" {
			channel = "general"
		}
		created := strings.TrimSpace(p.CreatedAt)
		if _, err := time.Parse(time.RFC3339, created); err != nil {
			created = now
		}
		updated := strings.TrimSpace(p.UpdatedAt)
		if _, err := time.Parse(time.RFC3339, updated); err != nil {
			updated = created
		}
		username := truncateRunes(strings.TrimSpace(p.Author.Username), 80)
		displayName := truncateRunes(strings.TrimSpace(p.Author.DisplayName), 120)
		if displayName == "" {
			displayName = username
		}
		if displayName == "" {
			displayName = "unknown"
		}
		authorURL := strings.TrimSpace(p.Author.URL)
		if !isHTTPURL(authorURL) {
			authorURL = instance
		}
		count := p.CommentCount
		if count < 0 {
			count = 0
		}
		if count > 1000000 {
			count = 1000000
		}
		out = append(out, store.RemotePost{
			ID:                id,
			SiteName:          siteName,
			URL:               postURL,
			Title:             title,
			BodyMD:            truncateRunes(p.Body, 50000),
			AuthorUsername:    username,
			AuthorDisplayName: displayName,
			AuthorURL:         authorURL,
			Channel:           channel,
			CreatedAt:         created,
			UpdatedAt:         updated,
			CommentCount:      count,
		})
		seen[id] = true
	}
	if err := s.store.ReplaceRemotePosts(instance, out); err != nil {
		return fmt.Errorf("store posts: %w", err)
	}
	log.Printf("federation: %s: %d posts", instance, len(out))
	return nil
}

func federationFeedURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("instance URL must use http or https")
	}
	if u.Host == "" {
		return nil, errors.New("instance URL has no host")
	}
	if !strings.HasSuffix(u.Path, ".json") {
		u.Path = strings.TrimRight(u.Path, "/") + federationFeedPath
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}
