package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// AdminHandler provides generic CRUD for all database entities.
// Routes:
//
//	GET    /api/v1/admin/{entity}           — list all rows
//	GET    /api/v1/admin/{entity}/{id}      — get single row
//	PUT    /api/v1/admin/{entity}/{id}      — update row
//	DELETE /api/v1/admin/{entity}/{id}      — delete row
type AdminHandler struct {
	db *gorm.DB
}

// entityMeta defines table name and columns for each allowed entity.
type entityMeta struct {
	Table   string
	Columns []string
}

var entities = map[string]entityMeta{
	"users": {
		Table:   "\"user\"",
		Columns: []string{"id", "name", "email", "\"is_admin\"", "\"emailVerified\"", "image", "\"createdAt\"", "\"updatedAt\""},
	},
	"worker-profiles": {
		Table:   "worker_profiles",
		Columns: []string{"id", "user_id", "profession", "business_name", "bio", "phone", "city", "service_radius_km", "address", "hourly_rate", "minimum_charge", "free_estimate", "years_experience", "certifications", "has_insurance", "languages", "emergency_service", "website", "social_links", "created_at", "updated_at"},
	},
	"client-profiles": {
		Table:   "client_profiles",
		Columns: []string{"id", "user_id", "full_name", "phone", "city", "address", "bio", "preferred_contact", "property_type", "notes", "created_at", "updated_at"},
	},
	"conversations": {
		Table:   "conversations",
		Columns: []string{"id", "user_id", "type", "metadata", "created_at", "updated_at"},
	},
	"messages": {
		Table:   "messages",
		Columns: []string{"id", "conversation_id", "role", "content", "created_at"},
	},
}

func NewAdminHandler(db *gorm.DB) *AdminHandler {
	return &AdminHandler{db: db}
}

// cleanCol strips surrounding quotes from a column name (e.g. "\"emailVerified\"" → "emailVerified").
func cleanCol(col string) string {
	return strings.Trim(col, `"`)
}

func (h *AdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse path: /api/v1/admin/{entity} or /api/v1/admin/{entity}/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/")
	parts := strings.SplitN(path, "/", 2)

	entitySlug := parts[0]
	meta, ok := entities[entitySlug]
	if !ok {
		http.Error(w, fmt.Sprintf(`{"error":"unknown entity: %s"}`, entitySlug), http.StatusNotFound)
		return
	}

	if len(parts) == 2 && parts[1] != "" {
		// Single row operations
		id := parts[1]
		switch r.Method {
		case http.MethodGet:
			h.getRow(w, meta, id)
		case http.MethodPut:
			h.updateRow(w, r, meta, id)
		case http.MethodDelete:
			h.deleteRow(w, meta, id)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	} else {
		// Collection operations
		switch r.Method {
		case http.MethodGet:
			h.listRows(w, r, meta)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	}
}

func (h *AdminHandler) listRows(w http.ResponseWriter, r *http.Request, meta entityMeta) {
	slog.Info("admin: list", "entity", meta.Table)
	q := h.db.Table(meta.Table).Select(strings.Join(meta.Columns, ", "))

	if meta.Table == "\"user\"" {
		q = q.Order("\"createdAt\" DESC")
	} else {
		q = q.Order("id DESC")
	}

	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
			q = q.Limit(l)
		}
	} else {
		q = q.Limit(100)
	}

	rows, err := q.Rows()
	if err != nil {
		slog.Error("admin: list query failed", "entity", meta.Table, "error", err)
		http.Error(w, fmt.Sprintf(`{"error":"query failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := make([]map[string]interface{}, 0)
	for rows.Next() {
		vals := make([]interface{}, len(meta.Columns))
		ptrs := make([]interface{}, len(meta.Columns))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			slog.Error("admin: scan row failed", "error", err)
			continue
		}
		row := make(map[string]interface{})
		for i, col := range meta.Columns {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				row[cleanCol(col)] = string(b)
			} else {
				row[cleanCol(col)] = v
			}
		}
		result = append(result, row)
	}

	slog.Info("admin: list completed", "entity", meta.Table, "count", len(result))
	json.NewEncoder(w).Encode(result)
}

func (h *AdminHandler) getRow(w http.ResponseWriter, meta entityMeta, id string) {
	slog.Info("admin: getRow", "entity", meta.Table, "id", id)
	row := make(map[string]interface{})
	vals := make([]interface{}, len(meta.Columns))
	ptrs := make([]interface{}, len(meta.Columns))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := h.db.Table(meta.Table).Select(strings.Join(meta.Columns, ", ")).Where("id = ?", id).Row().Scan(ptrs...); err != nil {
		slog.Warn("admin: row not found", "entity", meta.Table, "id", id)
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	for i, col := range meta.Columns {
		v := vals[i]
		if b, ok := v.([]byte); ok {
			row[cleanCol(col)] = string(b)
		} else {
			row[cleanCol(col)] = v
		}
	}

	json.NewEncoder(w).Encode(row)
}

func (h *AdminHandler) updateRow(w http.ResponseWriter, r *http.Request, meta entityMeta, id string) {
	slog.Info("admin: updateRow", "entity", meta.Table, "id", id)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Warn("admin: failed to read request body", "entity", meta.Table, "id", id, "error", err)
		http.Error(w, `{"error":"read body failed"}`, http.StatusBadRequest)
		return
	}

	var updates map[string]interface{}
	if err := json.Unmarshal(body, &updates); err != nil {
		slog.Warn("admin: invalid JSON in update body", "entity", meta.Table, "id", id, "error", err)
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Map clean column names to original (quoted) names for whitelist check
	allowed := make(map[string]string)
	for _, col := range meta.Columns {
		allowed[cleanCol(col)] = col
	}

	filtered := make(map[string]interface{})
	for col, val := range updates {
		originalCol, ok := allowed[col]
		if !ok || col == "id" {
			continue
		}
		filtered[cleanCol(originalCol)] = val
	}

	if len(filtered) == 0 {
		http.Error(w, `{"error":"no valid fields to update"}`, http.StatusBadRequest)
		return
	}

	// Set updated_at if column exists
	for _, col := range meta.Columns {
		if col == "updated_at" || col == "updatedAt" {
			filtered["updated_at"] = gorm.Expr("NOW()")
			break
		}
	}

	result := h.db.Table(meta.Table).Where("id = ?", id).Updates(filtered)
	if result.Error != nil {
		slog.Error("admin: update failed", "entity", meta.Table, "id", id, "error", result.Error)
		http.Error(w, fmt.Sprintf(`{"error":"update failed: %s"}`, result.Error.Error()), http.StatusInternalServerError)
		return
	}

	if result.RowsAffected == 0 {
		slog.Warn("admin: update found no matching row", "entity", meta.Table, "id", id)
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	slog.Info("admin: update completed", "entity", meta.Table, "id", id, "rows_affected", result.RowsAffected)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "rows_affected": result.RowsAffected})
}

func (h *AdminHandler) deleteRow(w http.ResponseWriter, meta entityMeta, id string) {
	slog.Info("admin: deleteRow", "entity", meta.Table, "id", id)
	result := h.db.Exec("DELETE FROM ? WHERE id = ?", gorm.Expr(meta.Table), id)

	if result.Error != nil {
		slog.Error("admin: delete failed", "entity", meta.Table, "id", id, "error", result.Error)
		http.Error(w, fmt.Sprintf(`{"error":"delete failed: %s"}`, result.Error.Error()), http.StatusInternalServerError)
		return
	}

	if result.RowsAffected == 0 {
		slog.Warn("admin: delete found no matching row", "entity", meta.Table, "id", id)
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	slog.Info("admin: delete completed", "entity", meta.Table, "id", id, "rows_affected", result.RowsAffected)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "rows_affected": result.RowsAffected})
}
