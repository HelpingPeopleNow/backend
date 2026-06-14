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
	cols := strings.Join(meta.Columns, ", ")
	query := fmt.Sprintf("SELECT %s FROM %s", cols, meta.Table)

	// Add ORDER BY based on primary key type
	if meta.Table == "\"user\"" {
		query += " ORDER BY \"createdAt\" DESC"
	} else {
		query += " ORDER BY id DESC"
	}

	// Limit
	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
			query += fmt.Sprintf(" LIMIT %d", l)
		}
	} else {
		query += " LIMIT 100"
	}

	rows, err := h.db.Raw(query).Rows()
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
			// Convert []byte to string for JSON
			if b, ok := v.([]byte); ok {
				row[cleanCol(col)] = string(b)
			} else {
				row[cleanCol(col)] = v
			}
		}
		result = append(result, row)
	}

	json.NewEncoder(w).Encode(result)
}

func (h *AdminHandler) getRow(w http.ResponseWriter, meta entityMeta, id string) {
	cols := strings.Join(meta.Columns, ", ")
	query := fmt.Sprintf("SELECT %s FROM %s WHERE id = $1", cols, meta.Table)

	// Users table uses text ID, others use bigint
	var row map[string]interface{}
	if meta.Table == "\"user\"" {
		row = make(map[string]interface{})
		vals := make([]interface{}, len(meta.Columns))
		ptrs := make([]interface{}, len(meta.Columns))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := h.db.Raw(query, id).Row().Scan(ptrs...); err != nil {
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
	} else {
		idInt, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		row = make(map[string]interface{})
		vals := make([]interface{}, len(meta.Columns))
		ptrs := make([]interface{}, len(meta.Columns))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := h.db.Raw(query, idInt).Row().Scan(ptrs...); err != nil {
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
	}

	json.NewEncoder(w).Encode(row)
}

func (h *AdminHandler) updateRow(w http.ResponseWriter, r *http.Request, meta entityMeta, id string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body failed"}`, http.StatusBadRequest)
		return
	}

	var updates map[string]interface{}
	if err := json.Unmarshal(body, &updates); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Filter to only allowed columns
	allowed := make(map[string]bool)
	for _, col := range meta.Columns {
		allowed[col] = true
	}

	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1
	for col, val := range updates {
		if !allowed[col] || col == "id" {
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}

	if len(setClauses) == 0 {
		http.Error(w, `{"error":"no valid fields to update"}`, http.StatusBadRequest)
		return
	}

	// Add updated_at if column exists
	hasUpdatedAt := false
	for _, col := range meta.Columns {
		if col == "updated_at" || col == "updatedAt" {
			hasUpdatedAt = true
			break
		}
	}
	if hasUpdatedAt {
		setClauses = append(setClauses, fmt.Sprintf("updated_at = NOW()"))
	}

	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = $%d", meta.Table, strings.Join(setClauses, ", "), argIdx)

	// Users table uses text ID, others use numeric
	if meta.Table == "\"user\"" {
		args = append(args, id)
	} else {
		idInt, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		args = append(args, idInt)
	}

	result := h.db.Exec(query, args...)
	if result.Error != nil {
		slog.Error("admin: update failed", "entity", meta.Table, "id", id, "error", result.Error)
		http.Error(w, fmt.Sprintf(`{"error":"update failed: %s"}`, result.Error.Error()), http.StatusInternalServerError)
		return
	}

	if result.RowsAffected == 0 {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "rows_affected": result.RowsAffected})
}

func (h *AdminHandler) deleteRow(w http.ResponseWriter, meta entityMeta, id string) {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = $1", meta.Table)

	var result *gorm.DB
	if meta.Table == "\"user\"" {
		result = h.db.Exec(query, id)
	} else {
		idInt, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		result = h.db.Exec(query, idInt)
	}

	if result.Error != nil {
		slog.Error("admin: delete failed", "entity", meta.Table, "id", id, "error", result.Error)
		http.Error(w, fmt.Sprintf(`{"error":"delete failed: %s"}`, result.Error.Error()), http.StatusInternalServerError)
		return
	}

	if result.RowsAffected == 0 {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "rows_affected": result.RowsAffected})
}
