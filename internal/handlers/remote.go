package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"api-node/internal/db/models"
	"api-node/internal/scraper"
	"api-node/internal/services"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ─── URL Support (domain-based registry) ─────────────────────────────

type sourceDefinition struct {
	Enabled    bool
	Type       string
	Domains    []string
	Extensions []string // for wildcard "direct" type
	Extract    func(u *url.URL, raw string) string
}

type supportResult struct {
	Source string
	Type   string
	Error  bool
}

// directExtensions lists video file extensions supported for direct URLs.
var directExtensions = []string{"m3u8", "mp4", "mkv", "avi", "mov", "webm", "flv", "wmv", "ts", "m4v", "3gp", "mpg", "mpeg"}

// SOURCES registry — matches platform support.ts
var sources = []sourceDefinition{
	{
		Enabled: true,
		Type:    "gdrive",
		Domains: []string{"drive.google.com", "docs.google.com"},
		Extract: func(_ *url.URL, raw string) string {
			// Extract file ID from Google Drive URL
			for _, prefix := range []string{"/file/d/", "/document/d/", "/presentation/d/"} {
				if idx := strings.Index(raw, prefix); idx >= 0 {
					rest := raw[idx+len(prefix):]
					if slashIdx := strings.IndexAny(rest, "/?#"); slashIdx >= 0 {
						return rest[:slashIdx]
					}
					return rest
				}
			}
			// ?id= pattern
			if idx := strings.Index(raw, "id="); idx >= 0 {
				rest := raw[idx+3:]
				if ampIdx := strings.IndexAny(rest, "&# "); ampIdx >= 0 {
					return rest[:ampIdx]
				}
				return rest
			}
			return ""
		},
	},
	{
		Enabled: true,
		Type:    "missav",
		Domains: []string{"missav.ai", "missav.ws"},
		Extract: func(u *url.URL, _ string) string {
			return "https://missav.ai" + u.Path
		},
	},
	{
		Enabled: true,
		Type:    "xvideos",
		Domains: []string{"xvideos.com", "xvideos.es", "xvideos.red", "xvideos.xxx"},
		Extract: func(u *url.URL, _ string) string {
			return u.Scheme + "://" + u.Host + u.Path
		},
	},
	{
		Enabled: true,
		Type:    "pornhub",
		Domains: []string{"pornhub.com", "pornhub.net", "pornhub.org"},
		Extract: func(u *url.URL, _ string) string {
			// /embed/<id> → canonical
			if strings.HasPrefix(u.Path, "/embed/") {
				parts := strings.Split(strings.TrimPrefix(u.Path, "/embed/"), "/")
				if len(parts) > 0 && parts[0] != "" {
					return "https://www.pornhub.com/view_video.php?viewkey=" + parts[0]
				}
			}
			// ?viewkey=<id>
			viewkey := u.Query().Get("viewkey")
			if viewkey != "" {
				return "https://www.pornhub.com/view_video.php?viewkey=" + viewkey
			}
			return ""
		},
	},
	{
		Enabled:    true,
		Type:       "direct",
		Domains:    []string{"*"},
		Extensions: directExtensions,
		Extract: func(u *url.URL, _ string) string {
			ext := strings.TrimPrefix(path.Ext(u.Path), ".")
			ext = strings.ToLower(ext)
			for _, e := range directExtensions {
				if ext == e {
					cleanURL := *u
					cleanURL.Fragment = ""
					return cleanURL.String()
				}
			}
			return ""
		},
	},
}

func findSource(typeName string) *sourceDefinition {
	for i := range sources {
		if sources[i].Type == typeName {
			return &sources[i]
		}
	}
	return nil
}

func matchDomain(hostname, pattern string) bool {
	if pattern == "*" {
		return true
	}
	bare := strings.TrimPrefix(hostname, "www.")
	barePattern := strings.TrimPrefix(pattern, "www.")
	return bare == barePattern
}

// supportURL checks if a URL is supported and extracts the source identifier.
func supportURL(link string) supportResult {
	u, err := url.Parse(link)
	if err != nil {
		return supportResult{Error: true}
	}
	hostname := strings.ToLower(u.Hostname())

	for _, src := range sources {
		if !src.Enabled {
			continue
		}
		matched := false
		for _, d := range src.Domains {
			if matchDomain(hostname, d) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		extracted := ""
		if src.Extract != nil {
			extracted = src.Extract(u, link)
		} else {
			// Default: clean URL without hash/query
			cleanURL := *u
			cleanURL.Fragment = ""
			cleanURL.RawQuery = ""
			extracted = cleanURL.String()
		}

		if extracted == "" {
			continue
		}
		return supportResult{Source: extracted, Type: src.Type}
	}

	return supportResult{Error: true}
}

// ─── Token Auth ──────────────────────────────────────────────────────

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func validateToken(token string) (*models.ApiKey, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	col := models.ApiKeyModel.Col()
	now := time.Now()

	keyHash := hashKey(token)

	// 1. Try new platform format: keyHash (SHA-256)
	var apiKey models.ApiKey
	err := col.FindOne(ctx, bson.M{"keyHash": keyHash}).Decode(&apiKey)
	if err == nil {
		col.UpdateOne(ctx, bson.M{"_id": apiKey.ID}, bson.M{"$set": bson.M{"lastUsedAt": now}})
		if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(now) {
			return nil, nil
		}
		return &apiKey, nil
	}
	if err != mongo.ErrNoDocuments {
		return nil, err
	}

	// 2. Fallback: legacy format — plain text token field
	err = col.FindOne(ctx, bson.M{"token": token}).Decode(&apiKey)
	if err == nil {
		col.UpdateOne(ctx, bson.M{"_id": apiKey.ID}, bson.M{"$set": bson.M{"lastAt": now}})
		return &apiKey, nil
	}
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	return nil, err
}

// ─── Scraped Data ────────────────────────────────────────────────────

// ScrapedData holds parsed metadata from a source URL.
type ScrapedData struct {
	Title      string
	Code       string // missav code (e.g. "SNOS-092")
	M3u8URL    string // playlist URL
	Accessible bool
}

// scrapeSource uses the parser registry to get metadata for a source URL.
func (h *Handler) scrapeSource(sourceType, source string) (*ScrapedData, error) {
	// Build a URL the parser can handle
	var parserURL string
	switch sourceType {
	case "gdrive":
		parserURL = "https://drive.google.com/file/d/" + source + "/view"
	case "missav", "xvideos", "pornhub":
		parserURL = source
	case "direct":
		parserURL = source
	default:
		return nil, fmt.Errorf("unsupported source type: %s", sourceType)
	}

	parser := h.registry.FindParser(parserURL)
	if parser == nil {
		return nil, fmt.Errorf("no parser found for URL: %s", parserURL)
	}

	normalizedURL, slug := parser.NormalizeURL(parserURL)

	var data map[string]interface{}
	var err error

	if parser.NeedsHTML() {
		client := scraper.NewHTMLClient()
		html, fetchErr := client.FetchHTMLWithRetry(normalizedURL, 3)
		if fetchErr != nil {
			return nil, fetchErr
		}
		data, err = parser.Parse(html)
	} else {
		data, err = parser.FetchAndParse(normalizedURL)
	}

	if err != nil {
		return nil, err
	}

	result := &ScrapedData{Accessible: true}

	// Extract fields
	if v, ok := data["title"].(string); ok {
		result.Title = v
	}
	if v, ok := data["accessible"].(bool); ok {
		result.Accessible = v
	}

	// Code: from slug or data
	if slug != "" {
		result.Code = strings.ToUpper(slug)
	}
	if v, ok := data["code"].(string); ok && v != "" {
		result.Code = strings.ToUpper(v)
	}

	// M3U8/playlist
	if v, ok := data["m3u8Url"].(string); ok {
		result.M3u8URL = v
	}
	if v, ok := data["playlist"].(string); ok && result.M3u8URL == "" {
		result.M3u8URL = v
	}

	return result, nil
}

// ─── Remote Handler ──────────────────────────────────────────────────

// Remote handles the /remote endpoint for WordPress and external clients.
// POST /remote  {"source":"<URL>", "token":"<API_KEY>", "title":"optional"}
// GET  /remote?source=<URL>&token=<API_KEY>&title=optional
func (h *Handler) Remote(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var source, token, title string

	switch r.Method {
	case http.MethodPost:
		var body struct {
			Source string `json:"source"`
			Token  string `json:"token"`
			Title  string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respondRemoteError(w, "no_source", http.StatusBadRequest)
			return
		}
		source = body.Source
		token = body.Token
		title = body.Title
	case http.MethodGet:
		source = r.URL.Query().Get("source")
		token = r.URL.Query().Get("token")
		title = r.URL.Query().Get("title")
	default:
		respondRemoteError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Validate source
	if source == "" {
		respondRemoteError(w, "no_source", http.StatusBadRequest)
		return
	}

	// 2. Validate token
	if token == "" {
		respondRemoteError(w, "no_token", http.StatusForbidden)
		return
	}
	apiKey, err := validateToken(token)
	if err != nil {
		log.Printf("❌ Token validation error: %v", err)
		respondRemoteError(w, "token_error", http.StatusInternalServerError)
		return
	}
	if apiKey == nil {
		respondRemoteError(w, "token_not_found", http.StatusForbidden)
		return
	}

	// 3. Check URL support
	support := supportURL(source)
	if support.Error {
		respondRemoteError(w, "url_no_supported", http.StatusBadRequest)
		return
	}

	creatorID := apiKey.GetCreatorID()
	spaceID := apiKey.SpaceID
	log.Printf("📥 Remote: type=%s source=%s creator=%s space=%s", support.Type, support.Source, creatorID, spaceID)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fileCol := models.FileModel.Col()

	// 4. Duplicate check — ไฟล์ source+type เดียวกันใน space นี้ (ไม่ trashed/deleted)
	duplicateFilter := bson.M{
		"metadata.source":     support.Source,
		"metadata.sourceType": support.Type,
		"metadata.deletedAt":  bson.M{"$exists": false},
		"metadata.trashedAt":  bson.M{"$exists": false},
		"spaceId":             spaceID,
	}

	var existingFile models.File
	err = fileCol.FindOne(ctx, duplicateFilter).Decode(&existingFile)
	if err == nil {
		log.Printf("✅ Duplicate found: slug=%s name=%s", existingFile.Slug, existingFile.Name)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"msg":     "cached",
			"slug":    existingFile.Slug,
			"title":   existingFile.Name,
		})
		return
	}

	// 5. Scrape URL — ดึง metadata จาก parser
	scraped, scrapeErr := h.scrapeSource(support.Type, support.Source)
	if scrapeErr != nil {
		log.Printf("⚠️ Scrape failed: %v", scrapeErr)
		// Don't return error yet — try clone first
	}

	// 5.5 Accessible check
	if scraped != nil && !scraped.Accessible {
		log.Printf("❌ URL not accessible: %s", support.Source)
		respondRemoteError(w, "URL is not accessible", http.StatusUnprocessableEntity)
		return
	}

	// 6. Clone check — หา file เดิมจาก SPACE อื่น (ข้าม space ปัจจุบัน)
	globalFilter := bson.M{
		"metadata.source":     support.Source,
		"metadata.sourceType": support.Type,
		"metadata.deletedAt":  bson.M{"$exists": false},
		"metadata.trashedAt":  bson.M{"$exists": false},
		"spaceId":             bson.M{"$ne": spaceID}, // เฉพาะ space อื่น
	}

	var sourceFile models.File
	err = fileCol.FindOne(ctx, globalFilter).Decode(&sourceFile)
	if err == nil {
		// มี file ต้นฉบับ → clone
		cloneName := resolveFileName(support.Type, scraped, &sourceFile, title)

		result, cloneErr := services.CloneFile(ctx, sourceFile.ID, creatorID, spaceID, cloneName)
		if cloneErr != nil {
			log.Printf("⚠️ Clone failed: %v — falling through to create", cloneErr)
		} else if result != nil {
			log.Printf("✅ Cloned: %s → slug=%s name=%s", sourceFile.ID, result.Slug, result.Name)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"msg":     "cloned",
				"slug":    result.Slug,
				"title":   result.Name,
			})
			return
		}
		// Clone failed → fall through to create new file
	}

	// 7. Scrape ล้มเหลวและไม่มี file ให้ clone → error
	if scrapeErr != nil || scraped == nil {
		respondRemoteError(w, "Failed to scrape source", http.StatusInternalServerError)
		return
	}

	// 8. Race condition guard — เช็คซ้ำอีกครั้งก่อนสร้าง
	var raceCheck models.File
	err = fileCol.FindOne(ctx, duplicateFilter).Decode(&raceCheck)
	if err == nil {
		log.Printf("✅ Race condition caught: slug=%s name=%s", raceCheck.Slug, raceCheck.Name)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"msg":     "cached",
			"slug":    raceCheck.Slug,
			"title":   raceCheck.Name,
		})
		return
	}

	// 9. Create new file via goose
	fileName := resolveFileName(support.Type, scraped, nil, title)
	if fileName == "" || fileName == "Remote Video" {
		if support.Type == "gdrive" {
			respondRemoteError(w, "Google Drive file not found or inaccessible", http.StatusNotFound)
			return
		}
		fileName = "Remote Video"
	}

	newFile := models.FileModel.New()
	newFile.Status = models.FileStatusWaiting
	newFile.Type = models.FileTypeVideo
	newFile.Name = fileName
	newFile.CreatorID = &creatorID
	newFile.SpaceID = &spaceID
	newFile.Metadata = &models.FileMetadata{
		Source:     &support.Source,
		SourceType: &support.Type,
	}

	// Set playlist for missav/pornhub/xvideos
	if scraped.M3u8URL != "" {
		newFile.Metadata.Playlist = &scraped.M3u8URL
	}

	_, err = models.FileModel.Create(ctx, newFile)
	if err != nil {
		log.Printf("❌ File create error: %v", err)
		respondRemoteError(w, "Failed to create file", http.StatusInternalServerError)
		return
	}

	log.Printf("✅ Created: slug=%s name=%s", newFile.Slug, newFile.Name)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"msg":     "remoted",
		"slug":    newFile.Slug,
		"title":   newFile.Name,
	})
}

// ─── Helpers ─────────────────────────────────────────────────────────

// resolveFileName determines the file name based on source type and scraped data.
// Matches platform route.ts logic.
func resolveFileName(sourceType string, scraped *ScrapedData, sourceFile *models.File, titleOverride string) string {
	if titleOverride != "" {
		return titleOverride
	}

	if scraped != nil {
		switch sourceType {
		case "missav":
			if scraped.Code != "" {
				return scraped.Code
			}
			if scraped.Title != "" {
				return scraped.Title
			}
		case "gdrive", "pornhub", "xvideos":
			if scraped.Title != "" {
				return scraped.Title
			}
		case "direct":
			if scraped.Title != "" {
				return scraped.Title
			}
		}
	}

	// Fallback to source file name
	if sourceFile != nil {
		return sourceFile.Name
	}

	return "Remote Video"
}

// respondRemoteError sends a JSON error response for the remote endpoint.
func respondRemoteError(w http.ResponseWriter, message string, statusCode int) {
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": message,
	})
}
