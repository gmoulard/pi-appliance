package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	Handler("1.2.3").ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func TestHealth(t *testing.T) {
	response := get(t, "/pi-appliance/api/health")
	if response.Code != http.StatusOK {
		t.Fatalf("code %d", response.Code)
	}
	var health Health
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health != (Health{Status: "ok", Version: "1.2.3"}) {
		t.Fatalf("réponse %+v", health)
	}
}

func TestConsolePage(t *testing.T) {
	response := get(t, "/pi-appliance/")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<h1>pi-appliance</h1>") {
		t.Fatalf("code %d, page %q", response.Code, response.Body.String())
	}
}

func TestRedirectWithoutSlash(t *testing.T) {
	response := get(t, "/pi-appliance")
	if response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != "/pi-appliance/" {
		t.Fatalf("code %d, Location %q", response.Code, response.Header().Get("Location"))
	}
}

func TestOutsideConsole(t *testing.T) {
	if response := get(t, "/"); response.Code != http.StatusNotFound {
		t.Fatalf("code %d", response.Code)
	}
}
