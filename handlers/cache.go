package handlers

import (
	"context"
	"database/sql"
	"sync"
)

// DeckCache garde en mémoire les decks existants : deck_id -> nombre de paquets.
//
// La map est lue par presque toutes les requêtes et modifiée seulement quand un
// deck est créé. Sans verrou, une lecture pendant un ajout serait une race
// condition (Go arrête même le programme : « concurrent map read and map
// write »). Avec un RWMutex, plusieurs lectures peuvent se faire en même temps
// (RLock) et seul l'ajout est exclusif (Lock).
type DeckCache struct {
	mu    sync.RWMutex
	decks map[string]int
}

// LoadDeckCache charge les decks de la base. Appelée une fois au démarrage.
func LoadDeckCache(ctx context.Context, db *sql.DB) (*DeckCache, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, deck_count FROM decks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	c := &DeckCache{decks: map[string]int{}}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		c.decks[id] = n
	}
	return c, rows.Err()
}

// Get renvoie le nombre de paquets du deck, et false si le deck n'existe pas.
func (c *DeckCache) Get(id string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n, ok := c.decks[id]
	return n, ok
}

// Add ajoute un deck. Appelée après le commit de sa création.
func (c *DeckCache) Add(id string, deckCount int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decks[id] = deckCount
}
