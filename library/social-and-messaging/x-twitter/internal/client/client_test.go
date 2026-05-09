package client

import (
	"testing"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/x-twitter/internal/config"
)

func TestCacheKeyIncludesAuthIdentity(t *testing.T) {
	base := &Client{
		BaseURL: "https://api.x.com",
		Config: &config.Config{
			AuthSource:    "config",
			AuthHeaderVal: "Bearer token-a",
			Path:          "/tmp/x-a.toml",
		},
	}
	otherToken := &Client{
		BaseURL: "https://api.x.com",
		Config: &config.Config{
			AuthSource:    "config",
			AuthHeaderVal: "Bearer token-b",
			Path:          "/tmp/x-a.toml",
		},
	}
	otherPath := &Client{
		BaseURL: "https://api.x.com",
		Config: &config.Config{
			AuthSource:    "config",
			AuthHeaderVal: "Bearer token-a",
			Path:          "/tmp/x-b.toml",
		},
	}

	baseKey := base.cacheKey("/2/users/me", map[string]string{"expansions": "pinned_tweet_id"})
	if baseKey == otherToken.cacheKey("/2/users/me", map[string]string{"expansions": "pinned_tweet_id"}) {
		t.Fatal("expected cache key to change when auth header changes")
	}
	if baseKey == otherPath.cacheKey("/2/users/me", map[string]string{"expansions": "pinned_tweet_id"}) {
		t.Fatal("expected cache key to change when config path changes")
	}
}

func TestCacheKeySortsParams(t *testing.T) {
	c := &Client{
		BaseURL: "https://api.x.com",
		Config:  &config.Config{AuthHeaderVal: "Bearer token"},
	}

	left := c.cacheKey("/2/users", map[string]string{"b": "2", "a": "1"})
	right := c.cacheKey("/2/users", map[string]string{"a": "1", "b": "2"})
	if left != right {
		t.Fatalf("expected param order not to affect cache key: %s != %s", left, right)
	}
}
