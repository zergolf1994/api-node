package services

import (
	"context"
	"log"

	"api-node/internal/db/models"

	"github.com/zergolf1994/goose"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// CloneResult holds the result of a clone operation.
type CloneResult struct {
	FileID string
	Slug   string
	Name   string
}

// CloneFile clones a source file and its media records into a new file.
// Uses MongoDB transaction to ensure atomicity.
//
// Logic (matches platform commands.clone-file.ts):
//  1. Find source file (must not be trashed/deleted/folder)
//  2. Create new file: copy metadata, set clonedFrom
//  3. Clone media records: video + thumbnails (no custom path)
func CloneFile(ctx context.Context, sourceFileID, creatorID, spaceID, nameOverride string) (*CloneResult, error) {
	var result *CloneResult

	err := goose.WithTransaction(ctx, func(sc mongo.SessionContext) (interface{}, error) {
		// 1. Find source file
		var sourceFile models.File
		err := models.FileModel.Col().FindOne(sc, bson.M{
			"_id":                sourceFileID,
			"type":               bson.M{"$ne": models.FileTypeFolder},
			"metadata.trashedAt": bson.M{"$exists": false},
			"metadata.deletedAt": bson.M{"$exists": false},
		}).Decode(&sourceFile)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				return nil, nil // source not found, not an error
			}
			return nil, err
		}

		// 2. Create new file
		newFile := models.FileModel.New()
		newFile.Status = sourceFile.Status
		newFile.Type = sourceFile.Type
		newFile.CreatorID = &creatorID
		newFile.SpaceID = &spaceID

		// Name: override or source name
		if nameOverride != "" {
			newFile.Name = nameOverride
		} else {
			newFile.Name = sourceFile.Name
		}

		// ClonedFrom: chain from original
		if sourceFile.ClonedFrom != nil {
			newFile.ClonedFrom = sourceFile.ClonedFrom
		} else {
			newFile.ClonedFrom = &sourceFile.ID
		}

		// Copy metadata
		if sourceFile.Metadata != nil {
			newFile.Metadata = &models.FileMetadata{
				Description: sourceFile.Metadata.Description,
				Duration:    sourceFile.Metadata.Duration,
				Highest:     sourceFile.Metadata.Highest,
				MimeType:    sourceFile.Metadata.MimeType,
				Size:        sourceFile.Metadata.Size,
				Source:      sourceFile.Metadata.Source,
				SourceType:  sourceFile.Metadata.SourceType,
				SourceHash:  sourceFile.Metadata.SourceHash,
				Playlist:    sourceFile.Metadata.Playlist,
			}
		}

		// Insert file via goose (auto _id, slug, timestamps)
		_, err = models.FileModel.Create(sc, newFile)
		if err != nil {
			return nil, err
		}

		log.Printf("📋 Cloned file: %s → %s (slug=%s)", sourceFile.ID, newFile.ID, newFile.Slug)

		// 3. Clone media records
		mediaFilter := bson.M{
			"fileId":    sourceFileID,
			"deletedAt": bson.M{"$exists": false},
		}

		// For VIDEO: clone video media + thumbnails without custom path
		if sourceFile.Type == models.FileTypeVideo {
			mediaFilter["$or"] = []bson.M{
				{"type": "video"},
				{"type": "thumbnail", "path": bson.M{"$exists": false}},
			}
		}

		mediaCur, err := models.MediaModel.Col().Find(sc, mediaFilter)
		if err != nil {
			return nil, err
		}
		defer mediaCur.Close(sc)

		mediaCount := 0
		for mediaCur.Next(sc) {
			var sourceMedia models.Media
			if err := mediaCur.Decode(&sourceMedia); err != nil {
				continue
			}

			newMedia := models.MediaModel.New()
			newMedia.Type = sourceMedia.Type
			newMedia.FileName = sourceMedia.FileName
			newMedia.MimeType = sourceMedia.MimeType
			newMedia.Resolution = sourceMedia.Resolution
			newMedia.StorageID = sourceMedia.StorageID
			newMedia.Path = sourceMedia.Path
			newMedia.SourceHash = sourceMedia.SourceHash
			newMedia.FileID = &newFile.ID

			// ClonedFrom: chain from original
			if sourceMedia.ClonedFrom != nil {
				newMedia.ClonedFrom = sourceMedia.ClonedFrom
			} else {
				newMedia.ClonedFrom = &sourceFileID
			}

			// Copy metadata
			if sourceMedia.Metadata != nil {
				newMedia.Metadata = &models.MediaMetadata{
					Size:      sourceMedia.Metadata.Size,
					Width:     sourceMedia.Metadata.Width,
					Height:    sourceMedia.Metadata.Height,
					Duration:  sourceMedia.Metadata.Duration,
					DirectURL: sourceMedia.Metadata.DirectURL,
				}
			}

			_, err = models.MediaModel.Create(sc, newMedia)
			if err != nil {
				log.Printf("⚠️ Failed to clone media %s: %v", sourceMedia.ID, err)
				continue
			}
			mediaCount++
		}

		log.Printf("📋 Cloned %d media records for file %s", mediaCount, newFile.ID)

		result = &CloneResult{
			FileID: newFile.ID,
			Slug:   newFile.Slug,
			Name:   newFile.Name,
		}

		return nil, nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}
