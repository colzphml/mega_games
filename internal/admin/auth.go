package admin

import (
	"crypto/subtle"
	"net/http"
)

// BasicAuth guards the admin panel. It exposes DELETE /teams/{name} and
// POST /schedule/upload, which were reachable by anyone on the network.
//
// An empty password disables the guard so a local run needs no setup;
// production sets ADMIN_PASSWORD.
func BasicAuth(username, password string, next http.Handler) http.Handler {
	if password == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="mega-games admin"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
