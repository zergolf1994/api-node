package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"api-node/internal/db/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FileData: GET /file/data?slug=<slug>
//
// สำหรับ track-node (FILE_API_URL) — resolve slug ที่ player ส่งมาเป็น
// {fileId, spaceId} เพื่อผูกยอดดูเข้ากับ workspace เจ้าของไฟล์
//
// ตอบเบาที่สุด: อ่านแค่ field ที่ใช้จริง ไม่ลาก metadata ก้อนใหญ่มาด้วย
// track-node แคชผลไว้ 30 นาทีต่อ slug จึงโดนถามแค่ครั้งเดียวต่อไฟล์ต่อ TTL
// ไม่ใช่ทุก heartbeat
//
//	200 {"fileId":"...","spaceId":"...","name":"...","duration":123}
//	404 {"error":true}   — slug ไม่มีจริง/ถูกลบ (track-node จะ negative-cache)
func (h *Handler) FileData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	slug := r.URL.Query().Get("slug")
	if slug == "" || len(slug) > 64 {
		respondError(w, "Missing 'slug' parameter", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var doc struct {
		ID       string  `bson:"_id"`
		SpaceID  *string `bson:"spaceId"`
		Name     string  `bson:"name"`
		Metadata *struct {
			Duration *float64 `bson:"duration"`
		} `bson:"metadata"`
	}
	err := models.FileModel.Col().FindOne(ctx,
		bson.M{
			"slug":               slug,
			"type":               "video",
			"metadata.trashedAt": bson.M{"$exists": false},
			"metadata.deletedAt": bson.M{"$exists": false},
		},
		options.FindOne().SetProjection(bson.M{
			"_id": 1, "spaceId": 1, "name": 1, "metadata.duration": 1,
		}),
	).Decode(&doc)

	if err != nil {
		status := http.StatusInternalServerError
		if err == mongo.ErrNoDocuments {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"error": true})
		return
	}

	spaceID := ""
	if doc.SpaceID != nil {
		spaceID = *doc.SpaceID
	}
	duration := 0.0
	if doc.Metadata != nil && doc.Metadata.Duration != nil {
		duration = *doc.Metadata.Duration
	}

	json.NewEncoder(w).Encode(map[string]any{
		"fileId":   doc.ID,
		"spaceId":  spaceID,
		"name":     doc.Name,
		"duration": duration,
	})
}
