package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/store"
)

// changePasswordReq is the request body for POST /api/v1/auth/change-password.
type changePasswordReq struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword lets the authenticated user change their own password.
// 401 on wrong current password, 400 on short/missing new password, 204 on success.
func (d Deps) ChangePassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in changePasswordReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.CurrentPassword == "" || in.NewPassword == "" {
		writeErr(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}
	err := d.Users.ChangePassword(r.Context(), userID(r.Context()), in.CurrentPassword, in.NewPassword)
	if errors.Is(err, store.ErrInvalidCredentials) {
		writeErr(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	if errors.Is(err, store.ErrPasswordTooShort) {
		writeErr(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "change password failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// userAdminResp is returned by admin user-list and user-create (no password_hash).
type userAdminResp struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func toUserAdminResp(u db.User) userAdminResp {
	return userAdminResp{ID: u.ID, Email: u.Email, Role: u.Role, CreatedAt: u.CreatedAt}
}

// AdminListUsers returns all users (admin only).
// GET /api/v1/users
func (d Deps) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, _, err := d.Users.ListUsersPage(r.Context(), "", 0, 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list users failed")
		return
	}
	out := make([]userAdminResp, 0, len(users))
	for _, u := range users {
		out = append(out, toUserAdminResp(u))
	}
	writeJSON(w, http.StatusOK, out)
}

// createUserReq is the request body for POST /api/v1/users.
type createUserReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// AdminCreateUser creates a new user account (admin only).
// POST /api/v1/users
func (d Deps) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in createUserReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Email == "" {
		writeErr(w, http.StatusBadRequest, "email, password, and role are required")
		return
	}
	if in.Password == "" {
		writeErr(w, http.StatusBadRequest, "email, password, and role are required")
		return
	}
	if in.Role == "" {
		writeErr(w, http.StatusBadRequest, "email, password, and role are required")
		return
	}
	u, err := d.Users.CreateUser(r.Context(), in.Email, in.Password, in.Role)
	if errors.Is(err, store.ErrEmailConflict) {
		writeErr(w, http.StatusConflict, "email already in use")
		return
	}
	if errors.Is(err, store.ErrPasswordTooShort) {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if errors.Is(err, store.ErrInvalidRole) {
		writeErr(w, http.StatusBadRequest, "role must be 'admin' or 'user'")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "create user failed")
		return
	}
	writeJSON(w, http.StatusCreated, toUserAdminResp(u))
}

// AdminDeleteUser removes a user (admin only).
// DELETE /api/v1/users/{id}
// Returns 400 if the admin attempts to delete themselves (lockout guard).
func (d Deps) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	if targetID == userID(r.Context()) {
		writeErr(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	err := d.Users.DeleteUser(r.Context(), targetID)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "delete user failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
