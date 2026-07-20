package parsers

import (
	"fmt"
	"regexp"
	"strings"
)

// Upload18Parser ดึง m3u8 จากหน้าเล่นของ upload18 (helvid player)
// URL: https://upload18.org/play/index/{slug}
// ค่าอยู่ใน window.PLAYER_CONFIG = { m3u8, thumb, videoKey, ... }
type Upload18Parser struct{}

// NewUpload18Parser creates a new upload18 parser
func NewUpload18Parser() *Upload18Parser {
	return &Upload18Parser{}
}

// GetName returns the parser name
func (p *Upload18Parser) GetName() string {
	return "Upload18 Parser"
}

// CanHandle checks if this parser can handle the given URL
func (p *Upload18Parser) CanHandle(rawURL string) bool {
	return strings.Contains(rawURL, "upload18.")
}

// NeedsHTML returns true — ต้องโหลด HTML แล้วดึง PLAYER_CONFIG
func (p *Upload18Parser) NeedsHTML() bool {
	return true
}

// FetchAndParse is not used for upload18 (NeedsHTML = true)
func (p *Upload18Parser) FetchAndParse(url string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("upload18 parser requires HTML content, use Parse() instead")
}

// NormalizeURL คืน URL หน้าเล่นมาตรฐาน + slug จาก /play/index/{slug}
func (p *Upload18Parser) NormalizeURL(rawURL string) (string, string) {
	clean := rawURL
	if idx := strings.IndexAny(clean, "?#"); idx != -1 {
		clean = clean[:idx]
	}
	clean = strings.TrimRight(clean, "/")

	slug := ""
	if idx := strings.Index(clean, "/play/index/"); idx != -1 {
		slug = clean[idx+len("/play/index/"):]
		if s := strings.IndexAny(slug, "/?#"); s != -1 {
			slug = slug[:s]
		}
	} else {
		parts := strings.Split(clean, "/")
		if len(parts) > 0 {
			slug = parts[len(parts)-1]
		}
	}
	return clean, slug
}

// configBlockRe จำกัดขอบเขตให้อยู่ในบล็อก window.PLAYER_CONFIG = { ... };
// (กันจับ m3u8 จาก JS อื่นในหน้า) — PLAYER_CONFIG เป็น JS object literal
// key ไม่มี quote json.Unmarshal เลยใช้ไม่ได้ ต้องดึงทีละ field ด้วย regex
var configBlockRe = regexp.MustCompile(`(?s)window\.PLAYER_CONFIG\s*=\s*\{(.*?)\};`)

// fieldRe สร้าง regex ดึงค่า string ของ field ชื่อ name ในบล็อก config
func fieldRe(name string) *regexp.Regexp {
	return regexp.MustCompile(name + `\s*:\s*"((?:[^"\\]|\\.)*)"`)
}

var (
	m3u8Re     = fieldRe("m3u8")
	alt720Re   = fieldRe("alternate720")
	thumbRe    = fieldRe("thumb")
	videoKeyRe = fieldRe("videoKey")
)

func matchField(re *regexp.Regexp, block string) string {
	if m := re.FindStringSubmatch(block); len(m) > 1 {
		// PLAYER_CONFIG หนี slash เป็น \/ — คืนกลับให้ URL ใช้ได้
		return strings.ReplaceAll(m[1], `\/`, "/")
	}
	return ""
}

// Parse ดึง window.PLAYER_CONFIG จาก HTML แล้วแปลงเป็น metadata กลาง
func (p *Upload18Parser) Parse(html string) (map[string]interface{}, error) {
	bm := configBlockRe.FindStringSubmatch(html)
	if len(bm) < 2 {
		return map[string]interface{}{
			"accessible": false,
			"error":      "PLAYER_CONFIG not found",
		}, nil
	}
	block := bm[1]

	m3u8 := matchField(m3u8Re, block)
	if m3u8 == "" {
		return map[string]interface{}{
			"accessible": false,
			"error":      "m3u8 not found in PLAYER_CONFIG",
		}, nil
	}

	result := map[string]interface{}{
		"accessible": true,
		"m3u8Url":    m3u8,
	}
	if alt := matchField(alt720Re, block); alt != "" {
		result["alternate720"] = alt
	}
	if thumb := matchField(thumbRe, block); thumb != "" {
		result["poster"] = thumb
		result["thumb"] = thumb
	}
	// ชื่อ/โค้ดจาก videoKey (เช่น "406fsdss-917-uncensored-leak")
	if vk := matchField(videoKeyRe, block); vk != "" {
		result["title"] = vk
		result["code"] = strings.ToUpper(vk)
	}
	return result, nil
}
