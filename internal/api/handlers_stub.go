package api

// Temporary stubs: ListTunnels/EventsStream are removed in Task 6.
// They keep the router compiling meanwhile.

import "net/http"

func (d Deps) ListTunnels(w http.ResponseWriter, _ *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not implemented")
}
func (d Deps) EventsStream(w http.ResponseWriter, _ *http.Request) {
	writeErr(w, http.StatusNotImplemented, "not implemented")
}
