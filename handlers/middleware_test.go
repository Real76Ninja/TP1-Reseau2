package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"TP1/models"
)

func TestRecover(t *testing.T) {
	h := Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boum")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	var r models.Response
	json.NewDecoder(rec.Body).Decode(&r)
	if rec.Code != http.StatusInternalServerError || r.Success || r.Error != "Internal server error" {
		t.Fatalf("code %d, %+v", rec.Code, r)
	}
}

// 100 requêtes en même temps : 100 identifiants différents, tous dans le contexte.
func TestRequestID(t *testing.T) {
	var mu sync.Mutex // protège vus
	vus := map[string]bool{}
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		vus[RequestIDFrom(r.Context())] = true
	}))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
		}()
	}
	wg.Wait()
	if len(vus) != 100 || vus[""] {
		t.Fatalf("%d identifiants différents, attendu 100", len(vus))
	}
}

// Lectures et ajouts en même temps dans le cache. Avec go test -race, ce test
// échoue si le RWMutex est retiré.
func TestCacheConcurrent(t *testing.T) {
	c := &DeckCache{decks: map[string]int{}}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.Add(fmt.Sprint(i), 1)
		}()
		go func() {
			defer wg.Done()
			c.Get(fmt.Sprint(i))
		}()
	}
	wg.Wait()
	for i := 0; i < 50; i++ {
		if _, ok := c.Get(fmt.Sprint(i)); !ok {
			t.Fatalf("deck %d absent du cache", i)
		}
	}
}
