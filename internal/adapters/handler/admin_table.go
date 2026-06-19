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

func cleanCol(col string) string {
	return strings.Trim(col, `"`)
}

func scanRow(meta entityMeta, vals []interface{}) map[string]interface{} {
	row := make(map[string]interface{})
	for i, col := range meta.Columns {
		v := vals[i]
		if b, ok := v.([]byte); ok {
			row[cleanCol(col)] = string(b)
		} else {
			row[cleanCol(col)] = v
		}
	}
	return row
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
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %s", err.Error()))
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
		result = append(result, scanRow(meta, vals))
	}

	slog.Info("admin: list completed", "entity", meta.Table, "count", len(result))
	_ = json.NewEncoder(w).Encode(result)
}

func (h *AdminHandler) getRow(w http.ResponseWriter, meta entityMeta, id string) {
	slog.Info("admin: getRow", "entity", meta.Table, "id", id)
	vals := make([]interface{}, len(meta.Columns))
	ptrs := make([]interface{}, len(meta.Columns))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := h.db.Table(meta.Table).Select(strings.Join(meta.Columns, ", ")).Where("id = ?", id).Row().Scan(ptrs...); err != nil {
		slog.Warn("admin: row not found", "entity", meta.Table, "id", id)
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	_ = json.NewEncoder(w).Encode(scanRow(meta, vals))
}

func (h *AdminHandler) updateRow(w http.ResponseWriter, r *http.Request, meta entityMeta, id string) {
	slog.Info("admin: updateRow", "entity", meta.Table, "id", id)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Warn("admin: failed to read body", "entity", meta.Table, "id", id, "error", err)
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	var updates map[string]interface{}
	if err := json.Unmarshal(body, &updates); err != nil {
		slog.Warn("admin: invalid JSON", "entity", meta.Table, "id", id, "error", err)
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

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
		writeError(w, http.StatusBadRequest, "no valid fields to update")
		return
	}

	for _, col := range meta.Columns {
		if col == "updated_at" || col == "updatedAt" {
			filtered["updated_at"] = gorm.Expr("NOW()")
			break
		}
	}

	result := h.db.Table(meta.Table).Where("id = ?", id).Updates(filtered)
	if result.Error != nil {
		slog.Error("admin: update failed", "entity", meta.Table, "id", id, "error", result.Error)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("update failed: %s", result.Error.Error()))
		return
	}

	if result.RowsAffected == 0 {
		slog.Warn("admin: update found no matching row", "entity", meta.Table, "id", id)
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	slog.Info("admin: update completed", "entity", meta.Table, "id", id, "rows_affected", result.RowsAffected)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "rows_affected": result.RowsAffected})
}

func (h *AdminHandler) deleteRow(w http.ResponseWriter, meta entityMeta, id string) {
	slog.Info("admin: deleteRow", "entity", meta.Table, "id", id)
	result := h.db.Exec("DELETE FROM ? WHERE id = ?", gorm.Expr(meta.Table), id)

	if result.Error != nil {
		slog.Error("admin: delete failed", "entity", meta.Table, "id", id, "error", result.Error)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete failed: %s", result.Error.Error()))
		return
	}

	if result.RowsAffected == 0 {
		slog.Warn("admin: delete found no matching row", "entity", meta.Table, "id", id)
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	slog.Info("admin: delete completed", "entity", meta.Table, "id", id, "rows_affected", result.RowsAffected)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "rows_affected": result.RowsAffected})
}
