package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/3to1go/shared/httpx"
)

type ContextKey string

const ContextKeyUser ContextKey = "user"

func CurrentUser(r *http.Request) *User {
	u, _ := r.Context().Value(ContextKeyUser).(*User)
	return u
}

func RequireUser(w http.ResponseWriter, r *http.Request) *User {
	u := CurrentUser(r)
	if u == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "login required")
		return nil
	}
	return u
}

func RequireAdmin(w http.ResponseWriter, r *http.Request) *User {
	u := RequireUser(w, r)
	if u == nil {
		return nil
	}
	if !u.IsAdmin {
		httpx.WriteError(w, http.StatusForbidden, "admin required")
		return nil
	}
	return u
}

func (a *Handler) Middleware(next http.Handler, isPublicPath func(string) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		cookie, _ := r.Cookie(a.CookieName)
		var token string
		if cookie != nil {
			token = cookie.Value
		}
		user, _ := a.Store.UserForSession(r.Context(), token)

		ctx := context.WithValue(r.Context(), ContextKeyUser, user)
		r = r.WithContext(ctx)

		if isPublicPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(path, "/api/") {
			if user == nil {
				httpx.WriteError(w, http.StatusUnauthorized, "login required")
				return
			}
			if user.MustChangePassword {
				httpx.WriteError(w, http.StatusForbidden, "password change required")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
