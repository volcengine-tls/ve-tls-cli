package console

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestFileCacheLinuxSharesOnePrivateKeyAcrossSessions(t *testing.T) {
	dir := t.TempDir()
	start := make(chan struct{})
	const parallel = 8
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		session := string(rune('a' + i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			cache, err := NewFileCache(dir)
			if err == nil {
				err = cache.WriteRaw(session, []byte(session))
			}
			if err != nil {
				t.Errorf("write session %q: %v", session, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "master.key")
	key, err := readCacheKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < parallel; i++ {
		session := string(rune('a' + i))
		payload, present, err := cache.ReadRaw(session)
		if err != nil || !present || string(payload) != session {
			t.Fatalf("read session %q: present=%v err=%v", session, present, err)
		}
	}
	if err := cache.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if after, err := readCacheKey(keyPath); err != nil || !bytes.Equal(key, after) {
		t.Fatalf("key changed while deleting a session: %v", err)
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cache.ReadRaw("b"); !errors.Is(err, errCacheKeyUnavailable) {
		t.Fatalf("read with an insecure key file: %v", err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cache.ReadRaw("b"); !errors.Is(err, errCacheKeyMissing) {
		t.Fatalf("read without key: %v", err)
	}
	if _, err := os.Stat(keyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read recreated the missing key: %v", err)
	}
}
