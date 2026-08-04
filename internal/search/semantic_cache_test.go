package search

import (
	"testing"
	"time"
)

func TestSemanticReadyCacheIsBoundedAndExpires(t *testing.T) {
	now := time.Unix(100, 0)
	cache := newSemanticReadyCache(func() time.Time { return now })

	for index := 0; index < maxSemanticReadyCacheEntries+8; index++ {
		cache.Store("workspace-"+string(rune(index)), true)
	}
	if got := cache.Len(); got != maxSemanticReadyCacheEntries {
		t.Fatalf("semantic ready cache size = %d, want bound %d", got, maxSemanticReadyCacheEntries)
	}

	now = now.Add(semanticReadyCacheTTL + time.Nanosecond)
	if _, ok := cache.Load("workspace-0"); ok {
		t.Fatal("expired semantic readiness entry was still accepted")
	}
	if got := cache.Len(); got != 0 {
		t.Fatalf("semantic ready cache retained expired entries: %d", got)
	}
}
