package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type dashboardAuthConfig struct {
	username string
	password string
}

var errIncompleteDashboardAuth = errors.New("WA_APP_AUTH_USERNAME and WA_APP_AUTH_PASSWORD must be configured together")

func newDashboardAuthConfig(username string, password string) (dashboardAuthConfig, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" && password == "" {
		return dashboardAuthConfig{}, nil
	}
	if username == "" || password == "" {
		return dashboardAuthConfig{}, errIncompleteDashboardAuth
	}
	return dashboardAuthConfig{username: username, password: password}, nil
}

func (c dashboardAuthConfig) enabled() bool {
	return c.username != "" && c.password != ""
}

func withOptionalDashboardAuth(next http.Handler, auth dashboardAuthConfig) http.Handler {
	if !auth.enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			next.ServeHTTP(w, r)
		case "/login":
			handleDashboardLogin(w, r, auth)
		case "/logout":
			handleDashboardLogout(w, r)
		default:
			if authenticatedDashboardRequest(r, auth) {
				next.ServeHTTP(w, r)
				return
			}
			rejectDashboardAuth(w, r)
		}
	})
}

func rejectDashboardAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if wantsDashboardJSON(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(dashboardNextFromRequest(r)), http.StatusSeeOther)
}

func wantsDashboardJSON(r *http.Request) bool {
	return r.Method != http.MethodGet || strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Accept"), "application/json")
}

func dashboardNextFromRequest(r *http.Request) string {
	next := r.URL.RequestURI()
	if next == "" || strings.HasPrefix(next, "/login") || strings.HasPrefix(next, "/logout") {
		return "/"
	}
	return dashboardSafeRedirectTarget(next)
}

func dashboardSafeRedirectTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "//") {
		return "/"
	}
	target, err := url.Parse(raw)
	if err != nil || target.IsAbs() || !strings.HasPrefix(target.Path, "/") || target.Path == "/login" || target.Path == "/logout" {
		return "/"
	}
	return target.RequestURI()
}

func authenticatedDashboardRequest(r *http.Request, auth dashboardAuthConfig) bool {
	return authenticatedDashboardSessionRequest(r, auth) || authenticatedDashboardBasicRequest(r, auth)
}

func authenticatedDashboardBasicRequest(r *http.Request, auth dashboardAuthConfig) bool {
	username, password, ok := r.BasicAuth()
	return ok && validDashboardCredentials(username, password, auth)
}

func validDashboardCredentials(username string, password string, auth dashboardAuthConfig) bool {
	return secureEqualString(strings.TrimSpace(username), auth.username) && secureEqualString(password, auth.password)
}

func secureEqualString(left string, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}
