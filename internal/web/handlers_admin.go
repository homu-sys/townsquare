package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"townsquare/internal/store"
)

func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	users, posts, comments, err := s.store.Stats()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read the statistics.")
		return
	}
	admins, _ := s.store.CountActiveAdmins()
	hits, _ := s.store.GetHits()
	recentUsers, _ := s.store.ListUsers(6, 0)
	recentPosts, _ := s.store.ListAllPosts(6, 0)
	invites, _ := s.store.ListInvites()

	page := s.newPage(r)
	page.Heading = "Admin"
	page.Stats = &StatsView{
		Users:        users,
		Posts:        posts,
		Comments:     comments,
		ActiveAdmins: admins,
		Hits:         hits,
	}
	page.Users = recentUsers
	page.Posts = recentPosts
	page.Invites = invites
	s.render(w, r, http.StatusOK, "admin.html", page)
}

func (s *Server) adminInvites(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	invites, err := s.store.ListInvites()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read invites.")
		return
	}
	page := s.newPage(r)
	page.Heading = "Invites"
	page.Invites = invites
	s.render(w, r, http.StatusOK, "admin_invites.html", page)
}

func (s *Server) adminInviteCreate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	days := 0
	if n, err := strconv.Atoi(r.PostFormValue("days")); err == nil && n > 0 {
		days = n
	}
	if days > 365 {
		days = 365
	}
	expires := ""
	if days > 0 {
		expires = time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	}

	var code string
	var err error
	for i := 0; i < 5; i++ {
		code = store.NewCode()
		err = s.store.CreateInvite(code, userFrom(r).ID, expires)
		if err == nil {
			break
		}
		if !errors.Is(err, store.ErrConflict) {
			s.renderError(w, r, http.StatusInternalServerError, "Could not create an invite.")
			return
		}
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not create an invite.")
		return
	}

	inv, _ := s.store.GetInvite(code)
	invites, _ := s.store.ListInvites()
	page := s.newPage(r)
	page.Heading = "Invites"
	page.Invites = invites
	page.NewInvite = inv
	page.Flash = "Invite created. Send this code to the person you want to invite."
	s.render(w, r, http.StatusOK, "admin_invites.html", page)
}

func (s *Server) adminInviteDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	err := s.store.DeleteInvite(r.PathValue("code"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.setFlash(w, "That invite was not found or has already been used.")
		} else {
			s.setFlash(w, "Could not delete that invite.")
		}
		redirect(w, r, "/admin/invites")
		return
	}
	s.setFlash(w, "Invite deleted.")
	redirect(w, r, "/admin/invites")
}

func (s *Server) adminNews(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	news, err := s.store.ListNews()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read the news.")
		return
	}
	page := s.newPage(r)
	page.Heading = "News"
	page.News = news
	s.render(w, r, http.StatusOK, "admin_news.html", page)
}

func (s *Server) adminNewsCreate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	if _, err := s.store.CreateNews(r.PostFormValue("body")); err != nil {
		if errors.Is(err, store.ErrInvalid) {
			s.setFlash(w, "News lines must be between 1 and 280 characters.")
		} else {
			s.setFlash(w, "Could not add that news line.")
		}
		redirect(w, r, "/admin/news")
		return
	}
	s.setFlash(w, "News line added.")
	redirect(w, r, "/admin/news")
}

func (s *Server) adminNewsUpdate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	err := s.store.UpdateNews(r.PathValue("id"), r.PostFormValue("body"))
	switch {
	case errors.Is(err, store.ErrInvalid):
		s.setFlash(w, "News lines must be between 1 and 280 characters.")
	case errors.Is(err, store.ErrNotFound):
		s.setFlash(w, "That news line was not found.")
	case err != nil:
		s.setFlash(w, "Could not save that news line.")
	default:
		s.setFlash(w, "News line updated.")
	}
	redirect(w, r, "/admin/news")
}

func (s *Server) adminNewsDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	err := s.store.DeleteNews(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.setFlash(w, "That news line was not found.")
		} else {
			s.setFlash(w, "Could not delete that news line.")
		}
		redirect(w, r, "/admin/news")
		return
	}
	s.setFlash(w, "News line deleted.")
	redirect(w, r, "/admin/news")
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	const limit = 50
	pageNum := pageParam(r)
	users, err := s.store.ListUsers(limit+1, (pageNum-1)*limit)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read users.")
		return
	}
	users, hasNext := trimPage(users, limit)

	page := s.newPage(r)
	page.Heading = "Users"
	page.Users = users
	page.PageNum = pageNum
	if pageNum > 1 {
		page.Prev = pageURL("/admin/users", pageNum-1)
	}
	if hasNext {
		page.Next = pageURL("/admin/users", pageNum+1)
	}
	s.render(w, r, http.StatusOK, "admin_users.html", page)
}

func (s *Server) adminUserDisable(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	target, ok := s.adminTarget(w, r, admin)
	if !ok {
		return
	}
	if target.Role == "admin" && !target.Disabled {
		n, _ := s.store.CountActiveAdminsExcluding(target.ID)
		if n == 0 {
			s.setFlash(w, "There must be at least one active admin.")
			redirect(w, r, "/admin/users")
			return
		}
	}
	if err := s.store.SetUserDisabled(target.ID, true); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not disable that account.")
		return
	}
	s.setFlash(w, target.DisplayName+" has been disabled.")
	redirect(w, r, "/admin/users")
}

func (s *Server) adminUserEnable(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	target, ok := s.adminTarget(w, r, admin)
	if !ok {
		return
	}
	if err := s.store.SetUserDisabled(target.ID, false); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not enable that account.")
		return
	}
	s.setFlash(w, target.DisplayName+" has been enabled.")
	redirect(w, r, "/admin/users")
}

func (s *Server) adminUserPromote(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	target, ok := s.adminTarget(w, r, admin)
	if !ok {
		return
	}
	if err := s.store.SetUserRole(target.ID, "admin"); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not change that role.")
		return
	}
	s.setFlash(w, target.DisplayName+" is now an admin.")
	redirect(w, r, "/admin/users")
}

func (s *Server) adminUserDemote(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	target, ok := s.adminTarget(w, r, admin)
	if !ok {
		return
	}
	if target.Role == "admin" {
		n, _ := s.store.CountActiveAdminsExcluding(target.ID)
		if n == 0 {
			s.setFlash(w, "There must be at least one active admin.")
			redirect(w, r, "/admin/users")
			return
		}
	}
	if err := s.store.SetUserRole(target.ID, "user"); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not change that role.")
		return
	}
	s.setFlash(w, target.DisplayName+" is now a regular user.")
	redirect(w, r, "/admin/users")
}

func (s *Server) adminTarget(w http.ResponseWriter, r *http.Request, admin *store.User) (*store.User, bool) {
	id := r.PathValue("id")
	if id == admin.ID {
		s.setFlash(w, "You cannot change your own account here.")
		redirect(w, r, "/admin/users")
		return nil, false
	}
	target, err := s.store.GetUserByID(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.setFlash(w, "That user does not exist.")
		} else {
			s.setFlash(w, "Could not read that user.")
		}
		redirect(w, r, "/admin/users")
		return nil, false
	}
	return target, true
}

func (s *Server) adminPosts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	const limit = 50
	pageNum := pageParam(r)
	posts, err := s.store.ListAllPosts(limit+1, (pageNum-1)*limit)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not read posts.")
		return
	}
	posts, hasNext := trimPage(posts, limit)

	page := s.newPage(r)
	page.Heading = "Posts"
	page.Posts = posts
	page.PageNum = pageNum
	if pageNum > 1 {
		page.Prev = pageURL("/admin/posts", pageNum-1)
	}
	if hasNext {
		page.Next = pageURL("/admin/posts", pageNum+1)
	}
	s.render(w, r, http.StatusOK, "admin_posts.html", page)
}

func (s *Server) adminPostDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if !s.checkCSRF(w, r) {
		return
	}
	err := s.store.DeletePost(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.setFlash(w, "That post does not exist.")
		} else {
			s.setFlash(w, "Could not delete that post.")
		}
		redirect(w, r, "/admin/posts")
		return
	}
	s.setFlash(w, "Post deleted.")
	redirect(w, r, "/admin/posts")
}
