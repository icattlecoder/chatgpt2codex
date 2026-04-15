package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

const apiKeyPrefix = "ctc_"

func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return apiKeyPrefix + hex.EncodeToString(bytes), nil
}

func requireAPIKey(apiKey string, next http.HandlerFunc) http.HandlerFunc {
	expected := strings.TrimSpace(apiKey)
	if expected == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if validBearerAuthorization(r.Header.Get("Authorization"), expected) {
			next(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="chatgpt2codex"`)
		body, _ := json.Marshal(tool.Response{
			Content: []tool.ContentItem{{Type: "text", Text: "missing or invalid bearer api key"}},
		})
		writeJSONResponse(w, http.StatusUnauthorized, body)
	}
}

func validBearerAuthorization(rawHeader, expectedAPIKey string) bool {
	rawHeader = strings.TrimSpace(rawHeader)
	expectedAPIKey = strings.TrimSpace(expectedAPIKey)
	if rawHeader == "" || expectedAPIKey == "" {
		return false
	}
	prefix, token, found := strings.Cut(rawHeader, " ")
	if !found || !strings.EqualFold(strings.TrimSpace(prefix), "Bearer") {
		return false
	}
	providedAPIKey := strings.TrimSpace(token)
	if len(providedAPIKey) != len(expectedAPIKey) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(providedAPIKey), []byte(expectedAPIKey)) == 1
}

func sanitizeHeaderValues(key string, values []string) []string {
	copied := make([]string, len(values))
	copy(copied, values)
	if strings.EqualFold(strings.TrimSpace(key), "Authorization") {
		for index := range copied {
			if strings.TrimSpace(copied[index]) != "" {
				copied[index] = "[REDACTED]"
			}
		}
	}
	return copied
}
