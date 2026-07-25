package models

import (
	"time"

	"github.com/zergolf1994/goose"
)

// RemoteBlock ติดตาม failure ของ source URL จาก remote import
// fail เกิน threshold → บล็อกชั่วคราว (cooldown) เพื่อกันยิงซ้ำ
// Collection: "remote_blocks" | _id: sha256(source)
type RemoteBlock struct {
	ID           string     `bson:"_id" json:"id"`
	Source       string     `bson:"source" json:"source"`
	SourceType   string     `bson:"sourceType" json:"sourceType"`
	FailCount    int        `bson:"failCount" json:"failCount"`
	LastError    string     `bson:"lastError" json:"lastError"`
	LastFailedAt time.Time  `bson:"lastFailedAt" json:"lastFailedAt"`
	BlockedUntil *time.Time `bson:"blockedUntil,omitempty" json:"blockedUntil,omitempty"` // nil = ไม่บล็อก
	CreatedAt    time.Time  `bson:"createdAt" json:"createdAt"`
	UpdatedAt    time.Time  `bson:"updatedAt" json:"updatedAt"`
}

// RemoteBlockModel is the goose model for the "remote_blocks" collection.
var RemoteBlockModel = goose.NewModel[RemoteBlock]("remote_blocks")
