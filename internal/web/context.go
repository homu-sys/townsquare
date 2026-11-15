package web

import (
	"context"
	"net/http"

	"townsquare/internal/store"
)

type contextKey int

const (
	userKey contextKey = iota
	sessionKey
	csrfKey
	sinceKey
)

func withAuth(r *http.Request, user *store.User, session *store.Session, csrf string) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, userKey, user)
	ctx = context.WithValue(ctx, sessionKey, session)
	ctx = context.WithValue(ctx, csrfKey, csrf)
	return r.WithContext(ctx)
}

func viewerID(r *http.Request) string {
	if u := userFrom(r); u != nil {
		return u.ID
	}
	return ""
}

func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

func sessionFrom(r *http.Request) *store.Session {
	s, _ := r.Context().Value(sessionKey).(*store.Session)
	return s
}

func csrfFrom(r *http.Request) string {
	s, _ := r.Context().Value(csrfKey).(string)
	return s
}

func withVisitSince(r *http.Request, since string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), sinceKey, since))
}

func visitSince(r *http.Request) string {
	s, _ := r.Context().Value(sinceKey).(string)
	return s
}
