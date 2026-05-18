package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

const sessionCookieName = "burrow_session"
const csrfCookieName = "burrow_csrf"
const sessionMaxAge = 7 * 24 * time.Hour

func setSessionCookie(w http.ResponseWriter, id string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// generateCSRFToken returns a 32-byte random token encoded as a 64-char hex string.
func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// setCSRFCookie sets the burrow_csrf cookie. It is intentionally NOT HttpOnly
// so that JavaScript can read and send it back as the X-CSRF-Token header
// (double-submit cookie pattern). SameSite and Secure mirror the session cookie.
func setCSRFCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // JS-readable: required for the double-submit pattern
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge.Seconds()),
	})
}

// clearCSRFCookie expires the burrow_csrf cookie (called on Logout).
func clearCSRFCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
