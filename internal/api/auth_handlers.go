package api

import (
	"encoding/json"
	"net"
	"net/http"
)

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// userResp is returned by Login and Me — id/email/role.
type userResp struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Login verifies credentials, creates a session, and sets the cookie.
func (d Deps) Login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in loginReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Email == "" {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ok, err := d.Users.VerifyUserPassword(r.Context(), in.Email, in.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	u, err := d.Users.GetUserByEmail(r.Context(), in.Email)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	// Store the host without the port so session.ip is a clean IP address
	// regardless of whether TrustedProxyMiddleware rewrote RemoteAddr to
	// "<clientIP>:0" (trusted proxy path) or the TCP peer is host:ephemeralport.
	clientHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		clientHost = r.RemoteAddr // fallback: RemoteAddr is already a bare IP
	}
	sid, err := d.Users.CreateSession(r.Context(), u.ID, r.UserAgent(), clientHost)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	csrfToken, err := generateCSRFToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	setSessionCookie(w, sid, d.SecureCookies)
	setCSRFCookie(w, csrfToken, d.SecureCookies)
	writeJSON(w, http.StatusOK, userResp{ID: u.ID, Email: u.Email, Role: u.Role})
}

// Logout deletes the session and clears both the session and CSRF cookies.
func (d Deps) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		_ = d.Users.DeleteSession(r.Context(), c.Value)
	}
	clearSessionCookie(w, d.SecureCookies)
	clearCSRFCookie(w, d.SecureCookies)
	w.WriteHeader(http.StatusNoContent)
}

// Me returns the authenticated user's public profile.
func (d Deps) Me(w http.ResponseWriter, r *http.Request) {
	u, err := d.Users.GetUserByID(r.Context(), userID(r.Context()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, userResp{ID: u.ID, Email: u.Email, Role: u.Role})
}
