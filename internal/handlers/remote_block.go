package handlers

import (
	"context"
	"log"
	"time"

	"api-node/internal/config"
	"api-node/internal/db/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ─── Remote source blocking ──────────────────────────────────────────
// source URL ที่ import fail (scrape/access ไม่ได้) เกิน threshold → บล็อก
// แบบ cooldown เพื่อกัน client ยิงซ้ำ พอ cooldown หมดให้ลองใหม่ได้เอง
// สำเร็จเมื่อไหร่ = ล้าง record ทิ้ง

// isSourceBlocked คืน true ถ้า source กำลังโดนบล็อก (ยังไม่พ้น cooldown)
func isSourceBlocked(ctx context.Context, source string) bool {
	var blk models.RemoteBlock
	err := models.RemoteBlockModel.Col().FindOne(ctx, bson.M{"_id": hashKey(source)}).Decode(&blk)
	if err != nil {
		return false
	}
	return blk.BlockedUntil != nil && blk.BlockedUntil.After(time.Now())
}

// recordSourceFailure เพิ่มตัวนับ fail; ถ้าถึง threshold → เริ่ม cooldown block
func recordSourceFailure(ctx context.Context, source, sourceType, errMsg string) {
	now := time.Now()
	id := hashKey(source)
	col := models.RemoteBlockModel.Col()

	var blk models.RemoteBlock
	err := col.FindOneAndUpdate(ctx,
		bson.M{"_id": id},
		bson.M{
			"$inc": bson.M{"failCount": 1},
			"$set": bson.M{
				"source":       source,
				"sourceType":   sourceType,
				"lastError":    errMsg,
				"lastFailedAt": now,
				"updatedAt":    now,
			},
			"$setOnInsert": bson.M{"createdAt": now},
		},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&blk)
	if err != nil {
		log.Printf("⚠️ recordSourceFailure: %v", err)
		return
	}

	threshold := config.AppConfig.RemoteFailThreshold
	if threshold <= 0 {
		threshold = 3
	}
	// ถึง threshold และยังไม่ได้บล็อก (หรือ cooldown หมดแล้ว) → เริ่ม/ต่อ cooldown
	if blk.FailCount >= threshold && (blk.BlockedUntil == nil || blk.BlockedUntil.Before(now)) {
		hours := config.AppConfig.RemoteBlockHours
		if hours <= 0 {
			hours = 24
		}
		until := now.Add(time.Duration(hours) * time.Hour)
		col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"blockedUntil": until, "updatedAt": now}})
		log.Printf("🚫 source blocked (%d fails): %s → until %s", blk.FailCount, source, until.Format(time.RFC3339))
	}
}

// clearSourceFailure ล้าง record เมื่อ import สำเร็จ (source ใช้ได้แล้ว)
func clearSourceFailure(ctx context.Context, source string) {
	models.RemoteBlockModel.Col().DeleteOne(ctx, bson.M{"_id": hashKey(source)})
}
