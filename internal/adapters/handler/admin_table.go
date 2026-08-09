package handler

import (
	"encoding/json"
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
		writeError(w, http.StatusInternalServerError, "internal query failed")
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
		writeError(w, http.StatusInternalServerError, "internal update failed")
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

	// CMP-01 (audit): deleting a user must cascade to every referencing table
	// in one transaction — a single-table delete orphans worker/client
	// profiles, embeddings, conversations, messages, DMs, reports and
	// feedback, leaving residual PII behind on a right-to-deletion erasure.
	if meta.Table == `"user"` {
		h.deleteUserCascade(w, id)
		return
	}

	result := h.db.Exec("DELETE FROM ? WHERE id = ?", gorm.Expr(meta.Table), id)

	if result.Error != nil {
		slog.Error("admin: delete failed", "entity", meta.Table, "id", id, "error", result.Error)
		writeError(w, http.StatusInternalServerError, "internal delete failed")
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

// deleteUserCascade permanently removes a user and every row that references
// them, in a single transaction. Children with edges to other children
// (messages -> conversations, direct_messages / direct_message_reports ->
// direct_conversations) are deleted before their parents, and the auth
// "user" row last. Raw SQL is used deliberately (not GORM model deletes) so
// soft-deleted rows (direct_messages.deleted_at) are physically purged too —
// a GDPR erasure must not leave tombstones or PII behind (CMP-01 audit).
//
// Referencing tables / columns:
//
//	worker_profiles.user_id, client_profiles.user_id,
//	worker_embeddings.user_id, conversations.user_id,
//	messages.conversation_id -> conversations.id,
//	direct_conversations.user_a_id / user_b_id,
//	direct_messages.conversation_id -> direct_conversations.id,
//	direct_message_reports.conversation_id -> direct_conversations.id
//	  (plus reported_by = the user),
//	feedback.user_id
func (h *AdminHandler) deleteUserCascade(w http.ResponseWriter, id string) {
	const q = `
DELETE FROM worker_embeddings      WHERE user_id = $1;
DELETE FROM direct_message_reports WHERE reported_by = $1 OR conversation_id IN (SELECT id FROM direct_conversations WHERE user_a_id = $1 OR user_b_id = $1);
DELETE FROM direct_messages        WHERE conversation_id IN (SELECT id FROM direct_conversations WHERE user_a_id = $1 OR user_b_id = $1);
DELETE FROM direct_conversations   WHERE user_a_id = $1 OR user_b_id = $1;
DELETE FROM messages               WHERE conversation_id IN (SELECT id FROM conversations WHERE user_id = $1);
DELETE FROM conversations          WHERE user_id = $1;
DELETE FROM feedback               WHERE user_id = $1;
DELETE FROM worker_profiles        WHERE user_id = $1;
DELETE FROM client_profiles        WHERE user_id = $1;
DELETE FROM "user"                 WHERE id = $1;`

	var affected int64
	err := h.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(q, id)
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected // command tag of the last statement ("user")
		return nil
	})
	if err != nil {
		slog.Error("admin: user cascade delete failed", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "internal delete failed")
		return
	}

	if affected == 0 {
		slog.Warn("admin: user cascade delete found no matching row", "id", id)
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	slog.Info("admin: user cascade delete completed", "id", id)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "rows_affected": affected})
}
