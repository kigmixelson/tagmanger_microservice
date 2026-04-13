package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"tagmanager-microservice/internal/storage"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type Handler struct {
	repo       storage.Repository
	httpClient *http.Client
	debugLogs  bool
}

func NewRouter(repo storage.Repository) http.Handler {
	debugLogs := parseBoolEnv(os.Getenv("DEBUG_REQUEST_LOGS"))
	h := &Handler{
		repo: repo,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		debugLogs: debugLogs,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.handleHealth)
	mux.HandleFunc("/api/tags/search", h.handleSearchTags)
	mux.HandleFunc("/api/tags/", h.handleTagByID)
	mux.HandleFunc("/api/tags", h.handleTags)
	return mux
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.repo.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unhealthy",
			"error":  "mongodb unavailable",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleTags(w http.ResponseWriter, r *http.Request) {
	h.logIncomingRequest(r)

	switch r.Method {
	case http.MethodGet:
		if !h.authorizeRead(w, r) {
			return
		}
		h.getTags(w, r)
	case http.MethodPost, http.MethodPut:
		if !h.authorizeWrite(w, r) {
			return
		}
		h.createTag(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleTagByID(w http.ResponseWriter, r *http.Request) {
	h.logIncomingRequest(r)

	if r.URL.Path == "/api/tags/" {
		h.handleTags(w, r)
		return
	}

	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeWrite(w, r) {
		return
	}

	idHex := strings.TrimPrefix(r.URL.Path, "/api/tags/")
	if idHex == "" || strings.Contains(idHex, "/") {
		writeError(w, http.StatusBadRequest, "invalid id in path")
		return
	}

	objID, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ObjectID")
		return
	}

	var payload bson.M
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	delete(payload, "_id")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	updated, err := h.repo.UpdateTag(ctx, objID, payload)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			writeError(w, http.StatusNotFound, "tag not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, normalizeDocument(updated))
}

func (h *Handler) getTags(w http.ResponseWriter, r *http.Request) {
	idParam := r.URL.Query().Get("id")
	nameParam := r.URL.Query().Get("name")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	switch {
	case idParam != "":
		objID, err := primitive.ObjectIDFromHex(idParam)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid ObjectID")
			return
		}
		doc, err := h.repo.GetTagByID(ctx, objID)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				writeError(w, http.StatusNotFound, "tag not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, normalizeDocument(doc))
		return
	case nameParam != "":
		doc, err := h.repo.GetTagByName(ctx, nameParam)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				writeError(w, http.StatusNotFound, "tag not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, normalizeDocument(doc))
		return
	default:
		docs, err := h.repo.GetAllTags(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, normalizeDocuments(docs))
	}
}

func (h *Handler) createTag(w http.ResponseWriter, r *http.Request) {
	var payload bson.M
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	delete(payload, "_id")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	doc, err := h.repo.CreateTag(ctx, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, normalizeDocument(doc))
}

func (h *Handler) handleSearchTags(w http.ResponseWriter, r *http.Request) {
	h.logIncomingRequest(r)

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.authorizeRead(w, r) {
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter q is required")
		return
	}

	fieldsParam := strings.TrimSpace(r.URL.Query().Get("fields"))
	fields := []string{"name", "description", "color", "class", "public", "visibility"}
	if fieldsParam != "" {
		fields = make([]string, 0)
		for _, f := range strings.Split(fieldsParam, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				fields = append(fields, f)
			}
		}
		if len(fields) == 0 {
			writeError(w, http.StatusBadRequest, "fields must not be empty")
			return
		}
	}

	limit := int64(50)
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.ParseInt(rawLimit, 10, 64)
		if err != nil || parsed <= 0 || parsed > 200 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = parsed
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	docs, err := h.repo.SearchTags(ctx, query, fields, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":  query,
		"fields": fields,
		"count":  len(docs),
		"items":  normalizeDocuments(docs),
	})
}

func (h *Handler) authorizeRead(w http.ResponseWriter, r *http.Request) bool {
	currentUser, err := h.fetchCurrentUser(r, authOptions{
		requireCSRFHeader: false,
		requireCSRFCookie: false,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if strings.TrimSpace(currentUser.ID) == "" {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func (h *Handler) authorizeWrite(w http.ResponseWriter, r *http.Request) bool {
	currentUser, err := h.fetchCurrentUser(r, authOptions{
		requireCSRFHeader: true,
		requireCSRFCookie: true,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if strings.TrimSpace(currentUser.ID) == "" {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}

	for _, p := range currentUser.Permissions {
		if p == "manage-configuration" {
			return true
		}
	}

	writeError(w, http.StatusForbidden, "forbidden")
	return false
}

type currentUserResponse struct {
	ID          string   `json:"id"`
	Permissions []string `json:"permissions"`
}

type authOptions struct {
	requireCSRFHeader bool
	requireCSRFCookie bool
}

func (h *Handler) fetchCurrentUser(r *http.Request, options authOptions) (currentUserResponse, error) {
	csrfHeader := strings.TrimSpace(r.Header.Get("x-csrf-token"))
	if options.requireCSRFHeader && csrfHeader == "" {
		h.debugf("auth precheck rejected: missing x-csrf-token, method=%s path=%s", r.Method, r.URL.Path)
		return currentUserResponse{}, errors.New("missing csrf header")
	}

	sidCookie, err := r.Cookie("sid")
	if err != nil || strings.TrimSpace(sidCookie.Value) == "" {
		h.debugf("auth precheck rejected: missing sid cookie, method=%s path=%s", r.Method, r.URL.Path)
		return currentUserResponse{}, errors.New("missing sid cookie")
	}

	csrfCookieValue := ""
	csrfCookie, err := r.Cookie("csrf")
	if err == nil {
		csrfCookieValue = strings.TrimSpace(csrfCookie.Value)
	}

	if options.requireCSRFCookie && csrfCookieValue == "" {
		h.debugf("auth precheck rejected: missing csrf cookie, method=%s path=%s", r.Method, r.URL.Path)
		return currentUserResponse{}, errors.New("missing csrf cookie")
	}

	targetURL, err := buildCurrentUserURL(r)
	if err != nil {
		return currentUserResponse{}, err
	}

	h.debugf(
		"auth precheck outbound request: method=GET url=%s x-csrf-token=%q cookie_sid=%q cookie_csrf=%q",
		targetURL,
		csrfHeader,
		sidCookie.Value,
		csrfCookieValue,
	)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURL, nil)
	if err != nil {
		return currentUserResponse{}, err
	}

	if csrfHeader != "" {
		req.Header.Set("x-csrf-token", csrfHeader)
	}

	cookieHeader := "sid=" + sidCookie.Value
	if csrfCookieValue != "" {
		cookieHeader += "; csrf=" + csrfCookieValue
	}
	req.Header.Set("Cookie", cookieHeader)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.debugf("auth precheck outbound error: url=%s err=%v", targetURL, err)
		return currentUserResponse{}, err
	}
	defer resp.Body.Close()

	h.debugf("auth precheck inbound response: url=%s status=%d", targetURL, resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return currentUserResponse{}, errors.New("current user check failed")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return currentUserResponse{}, err
	}

	var currentUser currentUserResponse
	if err := json.Unmarshal(body, &currentUser); err != nil {
		return currentUserResponse{}, err
	}

	h.debugf(
		"auth precheck parsed user: id=%q permissions_count=%d",
		currentUser.ID,
		len(currentUser.Permissions),
	)

	return currentUser, nil
}

func buildCurrentUserURL(r *http.Request) (string, error) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "", errors.New("missing host")
	}

	u := &url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   "/node/api/users/current",
	}
	return u.String(), nil
}

func (h *Handler) logIncomingRequest(r *http.Request) {
	h.debugf(
		"incoming request: method=%s host=%s uri=%s path=%s query=%q raw_url=%q remote_addr=%s cookie_header=%q x-csrf-token=%q",
		r.Method,
		r.Host,
		r.RequestURI,
		r.URL.Path,
		r.URL.RawQuery,
		r.URL.String(),
		r.RemoteAddr,
		r.Header.Get("Cookie"),
		r.Header.Get("x-csrf-token"),
	)

	for _, c := range r.Cookies() {
		h.debugf("incoming request cookie: name=%s value=%q", c.Name, c.Value)
	}
}

func (h *Handler) debugf(format string, args ...interface{}) {
	if !h.debugLogs {
		return
	}
	log.Printf(format, args...)
}

func parseBoolEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func normalizeDocuments(docs []bson.M) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(docs))
	for _, doc := range docs {
		out = append(out, normalizeDocument(doc))
	}
	return out
}

func normalizeDocument(doc bson.M) map[string]interface{} {
	result := make(map[string]interface{}, len(doc))
	for k, v := range doc {
		if k == "_id" {
			result["id"] = normalizeValue(v)
			continue
		}
		result[k] = normalizeValue(v)
	}
	return result
}

func normalizeValue(v interface{}) interface{} {
	switch t := v.(type) {
	case primitive.ObjectID:
		return t.Hex()
	case bson.M:
		return normalizeDocument(t)
	case map[string]interface{}:
		return normalizeDocument(bson.M(t))
	case []interface{}:
		items := make([]interface{}, len(t))
		for i := range t {
			items[i] = normalizeValue(t[i])
		}
		return items
	default:
		return v
	}
}
