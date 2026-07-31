package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestBasicAuthRejectsMissingCredentials(t *testing.T) {
	h := BasicAuth("admin", "secret", okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/teams/Falcons", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401: DELETE must not be reachable without credentials", rec.Code)
	}
}

func TestBasicAuthRejectsWrongPassword(t *testing.T) {
	h := BasicAuth("admin", "secret", okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rec.Code)
	}
}

func TestBasicAuthAcceptsCorrectCredentials(t *testing.T) {
	h := BasicAuth("admin", "secret", okHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rec.Code)
	}
}

func TestBasicAuthDisabledWhenPasswordEmpty(t *testing.T) {
	h := BasicAuth("", "", okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200 when auth is not configured", rec.Code)
	}
}
