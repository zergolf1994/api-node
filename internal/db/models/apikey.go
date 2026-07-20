package models

import (
	"time"

	"github.com/zergolf1994/goose"
)

// ApiKey represents an API key for programmatic access.
// Collection: "api_keys" | _id: String (UUID)
//
// New platform: keyHash (SHA-256), creatorId, spaceId
// Legacy (old TS): token (plain text), ownerId
type ApiKey struct {
	ID         string     `bson:"_id" json:"id" goose:"required,default:uuid"`
	Name       string     `bson:"name" json:"name" goose:"required"`
	KeyHash    string     `bson:"keyHash,omitempty" json:"-"`
	Prefix     string     `bson:"prefix,omitempty" json:"prefix"`
	CreatorID  string     `bson:"creatorId,omitempty" json:"creatorId,omitempty" goose:"ref:user"`
	SpaceID    string     `bson:"spaceId,omitempty" json:"spaceId,omitempty" goose:"ref:workspaces,index"`
	LastUsedAt *time.Time `bson:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
	CreatedAt  time.Time  `bson:"createdAt" json:"createdAt" goose:"default:now"`
	UpdatedAt  time.Time  `bson:"updatedAt" json:"updatedAt" goose:"default:now"`

	// Legacy fields (old TS system)
	Token   string  `bson:"token,omitempty" json:"-"`             // plain text token (legacy)
	OwnerID *string `bson:"ownerId,omitempty" json:"-"`           // legacy owner reference
	Enable  *bool   `bson:"enable,omitempty" json:"-"`            // legacy enable flag
}

// GetCreatorID returns CreatorID (new) or OwnerID (legacy).
func (a *ApiKey) GetCreatorID() string {
	if a.CreatorID != "" {
		return a.CreatorID
	}
	if a.OwnerID != nil {
		return *a.OwnerID
	}
	return ""
}

// ApiKeyModel is the goose model for the "api_keys" collection.
var ApiKeyModel = goose.NewModel[ApiKey]("api_keys")
