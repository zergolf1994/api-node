# Server API

VDOHide API server สำหรับ Remote file management, web scraping, และ file cloning สำหรับ [VDOHide](https://vdohide.com)

## Features

- **Remote API** — รับ URL จาก WordPress/external clients แล้วสร้าง/clone file อัตโนมัติ
- **Clone** — clone file + media records จาก file ที่มีอยู่ (with MongoDB transaction)
- **Scraping** — ดึง metadata จากเว็บไซต์ต่างๆ
- **Google Drive** — ดึง metadata ผ่าน OAuth2 API v3
- **Anti-Bot** — fallback ไปใช้ Headless Chrome (go-rod + stealth) เมื่อโดน 403
- **CORS** — รองรับ cross-origin requests

### Supported Sources

| Source | Domains | Method |
|---|---|---|
| MissAV | missav.ai, missav.ws | HTML scraping |
| XVideos | xvideos.com | HTML scraping |
| PornHub | pornhub.com | HTML scraping |
| Google Drive | drive.google.com | OAuth2 API v3 |
| Direct URL | *.mp4, *.m3u8, ... | HEAD request |

## Requirements

- **Chromium** หรือ **Google Chrome** (สำหรับ Cloudflare bypass)
- **MongoDB** (required — สำหรับ API keys, files, OAuth)

---

## Installation (Linux Server)

### One-line install

```bash
curl -fsSL https://raw.githubusercontent.com/zergolf1994/api-node/main/install.sh | sudo -E bash
```

### Options

| Option | Default | คำอธิบาย |
|---|---|---|
| `-p, --port` | `8081` | HTTP port |
| `--mongodb-uri` | `""` | MongoDB connection string |
| `--uninstall` | — | ถอนการติดตั้ง |

### Examples

```bash
# Install with custom port + MongoDB
curl -fsSL https://raw.githubusercontent.com/zergolf1994/api-node/main/install.sh | sudo -E bash -s -- \
    --port 8081 \
    --mongodb-uri "mongodb+srv://user:pass@cluster.mongodb.net/vdohide"

# Uninstall
curl -fsSL https://raw.githubusercontent.com/zergolf1994/api-node/main/install.sh | sudo bash -s -- --uninstall
```

### After install

```bash
# ดู logs
journalctl -u api-node -f

# Restart
systemctl restart api-node

# Status
systemctl status api-node
```

---

## Download Latest Release

```bash
# Linux amd64
curl -L https://github.com/zergolf1994/api-node/releases/latest/download/linux -o api-node
chmod +x api-node

# Linux ARM64
curl -L https://github.com/zergolf1994/api-node/releases/latest/download/linux-arm64 -o api-node
chmod +x api-node
```

---

## API Endpoints

### `GET /health`
```json
{ "status": "ok", "service": "api-node" }
```

### `GET /parsers`
```json
{ "parsers": [...], "count": 5 }
```

### `POST /remote`
Remote file creation — สร้าง/clone file จาก URL

**Request:**
```json
{
  "source": "https://drive.google.com/file/d/xxx/view",
  "token": "vh_xxxx..."
}
```

**Response (สร้างใหม่):**
```json
{
  "success": true,
  "msg": "remoted",
  "slug": "abc123def45",
  "title": "Filename.mp4"
}
```

**Response (clone จาก file เดิม):**
```json
{
  "success": true,
  "msg": "cloned",
  "slug": "xyz789abc12",
  "title": "Filename.mp4"
}
```

**Response (URL ซ้ำในระบบ):**
```json
{
  "success": true,
  "msg": "cached",
  "slug": "abc123def45",
  "title": "Filename.mp4"
}
```

### `GET /scraper?url=<URL>`
### `POST /scraper` `{ "url": "<URL>" }`

```json
{
  "success": true,
  "parser": "MissAV Parser",
  "url": "https://missav.ai/en/abcd-123",
  "data": {
    "title": "...",
    "poster": "https://...",
    "m3u8Url": "https://...",
    "duration": 3600
  },
  "timestamp": "2026-01-01T00:00:00Z"
}
```

---

## Configuration (.env)

```env
# HTTP port
HTTP_PORT=8081

# HTTP timeout in seconds (default: 30)
HTTP_TIMEOUT=30

# MongoDB URI (required)
MONGODB_URI=mongodb+srv://user:pass@cluster.mongodb.net/vdohide
```

---

## Development

```bash
# Clone
git clone https://github.com/zergolf1994/api-node.git
cd api-node

# สร้าง .env
cp .env.example .env

# Run
go run ./cmd

# Build all platforms
./build.bat
```

---

## Release

```bash
git tag v1.0.0
git push origin v1.0.0
```

GitHub Actions จะ build และ release อัตโนมัติพร้อม:
- `linux` — Linux amd64 binary
- `linux-arm64` — Linux ARM64 binary
- `install.sh` — Installation script

> Google Drive ต้องการ `oauths` collection ใน MongoDB สำหรับ OAuth credentials
