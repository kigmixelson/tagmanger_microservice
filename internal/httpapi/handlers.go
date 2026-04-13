package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tagmanager-microservice/internal/storage"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type Handler struct {
	repo storage.Repository
}

func NewRouter(repo storage.Repository) http.Handler {
	h := &Handler{repo: repo}
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
	switch r.Method {
	case http.MethodGet:
		h.getTags(w, r)
	case http.MethodPost, http.MethodPut:
		h.createTag(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleTagByID(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/tags/" {
		h.handleTags(w, r)
		return
	}

	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
