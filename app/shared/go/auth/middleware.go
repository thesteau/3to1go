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
	protection := http.NewCrossOriginProtection()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if strings.HasPrefix(path, "/api/") {
			if err := protection.Check(r); err != nil {
				httpx.WriteError(w, http.StatusForbidden, "cross-origin browser writes are not allowed")
				return
			}
		}

		cookie, _ := r.Cookie(a.CookieName)
		var token string
		if cookie != nil {
			token = cookie.Value
		}
		var user *User
		var automation *AutomationToken
		if strings.HasPrefix(path, "/api/") && r.Header.Get("Authorization") != "" {
			store, ok := a.Store.(AutomationStore)
			header := r.Header.Get("Authorization")
			if ok && strings.HasPrefix(header, "Bearer 3to1go_api_") {
				user, automation, _ = store.UserForAutomationToken(r.Context(), AutomationTokenHash(strings.TrimPrefix(header, "Bearer ")))
			}
			if user == nil || automation == nil || !user.IsAdmin {
				httpx.WriteError(w, http.StatusUnauthorized, "invalid automation token")
				return
			}
			if user.MustChangePassword {
				httpx.WriteError(w, http.StatusForbidden, "password change required")
				return
			}
			if !automationAllows(r, automation) {
				httpx.WriteError(w, http.StatusForbidden, "automation token scope does not allow this request")
				return
			}
		} else {
			user, _ = a.Store.UserForSession(r.Context(), token)
		}

		ctx := context.WithValue(r.Context(), ContextKeyUser, user)
		ctx = context.WithValue(ctx, contextKeyAutomation, automation)
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
