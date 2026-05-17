package api

import (
	"encoding/json"
	"net/http"
)

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
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
	sid, err := d.Users.CreateSession(r.Context(), u.ID, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	setSessionCookie(w, sid, d.SecureCookies)
	writeJSON(w, http.StatusOK, map[string]string{"id": u.ID, "email": u.Email, "role": u.Role})
}

// Logout deletes the session and clears the cookie.
func (d Deps) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		_ = d.Users.DeleteSession(r.Context(), c.Value)
	}
	clearSessionCookie(w, d.SecureCookies)
	w.WriteHeader(http.StatusNoContent)
}

// Me returns the authenticated user's public profile.
func (d Deps) Me(w http.ResponseWriter, r *http.Request) {
	u, err := d.Users.GetUserByID(r.Context(), userID(r.Context()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": u.ID, "email": u.Email, "role": u.Role})
}
