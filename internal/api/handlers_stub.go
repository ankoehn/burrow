package api

// Temporary stubs: ListTokens/CreateToken/RevokeToken are removed in Task 5;
// ListTunnels/EventsStream in Task 6. They keep the router compiling meanwhile.

import "net/http"

func (d Deps) ListTokens(w http.ResponseWriter, _ *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not implemented")
}
func (d Deps) CreateToken(w http.ResponseWriter, _ *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not implemented")
}
func (d Deps) RevokeToken(w http.ResponseWriter, _ *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not implemented")
}
func (d Deps) ListTunnels(w http.ResponseWriter, _ *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not implemented")
}
func (d Deps) EventsStream(w http.ResponseWriter, _ *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not implemented")
}
