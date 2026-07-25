// Package realtime publishes space events to Ably from api-node.
//
// Channel naming + event names MUST match the TypeScript @workspace/realtime
// package so browser subscribers (space channel) receive these:
//   - channel: "space:{slug}" with the "ws_" URL prefix stripped (same as
//     getSpaceChannel → normalizeSpaceSlug)
//   - event:   "file:uploaded" (RealtimeEvents.FILE_UPLOADED)
//
// No shared Go module by design — the convention is copied here (see the
// no-shared-go-module note). If the TS side changes the naming, update this.
package realtime

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"api-node/internal/config"

	"github.com/ably/ably-go/ably"
)

// Event names (mirror RealtimeEvents in @workspace/realtime).
const (
	EventFileUploaded = "file:uploaded"
)

const spaceSlugPrefix = "ws_"

var (
	restOnce sync.Once
	rest     *ably.REST
)

// client lazily builds the Ably REST client from ABLY_API_KEY.
// Returns nil when the key is unset → realtime is disabled (no-op), matching
// the "no key = disabled" behavior on the TS side.
func client() *ably.REST {
	restOnce.Do(func() {
		key := config.AppConfig.AblyAPIKey
		if key == "" {
			log.Printf("ℹ️ realtime disabled — ABLY_API_KEY not set")
			return
		}
		c, err := ably.NewREST(ably.WithKey(key))
		if err != nil {
			log.Printf("⚠️ Ably init failed: %v", err)
			return
		}
		log.Printf("📡 realtime enabled (Ably)")
		rest = c
	})
	return rest
}

// spaceChannel normalizes the slug (strips the "ws_" URL prefix) so publish,
// subscribe and token capability all resolve to the same channel.
func spaceChannel(slug string) string {
	return "space:" + strings.TrimPrefix(slug, spaceSlugPrefix)
}

// PublishSpaceEvent fires a space event — fire-and-forget, never blocks the
// caller's request. No-op when ABLY_API_KEY is unset or slug is empty.
//
// Remote imports run under an API key (no browser actor), so callers omit
// "userId" → every subscriber viewing the space refreshes.
func PublishSpaceEvent(slug, event string, data map[string]any) {
	c := client()
	if c == nil || slug == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	channel := spaceChannel(slug)
	if err := c.Channels.Get(channel).Publish(ctx, event, data); err != nil {
		log.Printf("⚠️ Ably publish failed (channel=%s): %v", channel, err)
		return
	}
	log.Printf("📡 published %s → channel=%s", event, channel)
}
