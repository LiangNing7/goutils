package token

import (
	jwt "github.com/golang-jwt/jwt/v4"
	"sync"
	"testing"
	"time"
)

func TestConcurrentInitAndSign(t *testing.T) {
	original := configSnapshot()
	t.Cleanup(func() { configMu.Lock(); config = original; configMu.Unlock() })
	Init("key-a", "a", time.Hour)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			Init("key-a", "a", time.Hour)
			Init("key-b", "b", time.Hour)
		}
	}()
	for i := 0; i < 1000; i++ {
		signed, _, err := Sign("user")
		if err != nil {
			t.Error(err)
			break
		}
		tok, err := jwt.Parse(signed, func(tok *jwt.Token) (any, error) {
			claims := tok.Claims.(jwt.MapClaims)
			if claims["a"] == "user" {
				return []byte("key-a"), nil
			}
			return []byte("key-b"), nil
		})
		if err != nil || !tok.Valid {
			t.Errorf("mixed configuration: %v", err)
			break
		}
		_, _ = Parse(signed, "key-a")
	}
	wg.Wait()
}
