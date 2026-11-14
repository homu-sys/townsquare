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
)

func withAuth(r *http.Request, user *store.User, session *store.Session, csrf string) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, userKey, user)
	ctx = context.WithValue(ctx, sessionKey, session)
	ctx = context.WithValue(ctx, csrfKey, csrf)
	return r.WithContext(ctx)
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
