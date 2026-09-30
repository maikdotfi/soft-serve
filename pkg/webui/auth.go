package webui

import (
	"errors"
	"net/http"

	"github.com/charmbracelet/soft-serve/pkg/webui/adminauth"
)

const basicAuthChallenge = `Basic realm="Soft Serve", charset="UTF-8"`

// requireAdmin gates next behind HTTP Basic auth checked by a. Browsers show
// their native login prompt on the 401 challenge.
func requireAdmin(a adminauth.Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, secret, ok := r.BasicAuth()
		if !ok {
			challenge(w)
			return
		}
		err := a.Authenticate(r.Context(), username, secret)
		switch {
		case err == nil:
			next.ServeHTTP(w, r)
		case errors.Is(err, adminauth.ErrInvalidCredentials):
			challenge(w)
		case errors.Is(err, adminauth.ErrNotAdmin):
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		default:
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	})
}

func challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", basicAuthChallenge)
	http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
}
