package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"TP1/models"
)

func benchPool(b *testing.B) *WorkerPool {
	db, err := OpenDB(filepath.Join(b.TempDir(), "bench.db"), 2)
	if err != nil {
		b.Fatal(err)
	}
	pool, err := NewWorkerPool(db.Read, db.Write, 2)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		pool.Close()
		db.Close()
	})
	return pool
}

func benchDeck(b *testing.B, pool *WorkerPool, deckCount int) string {
	res, err := pool.SubmitWrite(context.Background(), func(ctx context.Context, tx *sql.Tx) (any, error) {
		return CreateDeck(ctx, tx, NewDeckParams{DeckCount: deckCount})
	})
	if err != nil {
		b.Fatal(err)
	}
	return res.(DeckInfo).ID
}

// BenchmarkDrawCard : piger une carte (un job d'écriture complet).
func BenchmarkDrawCard(b *testing.B) {
	pool := benchPool(b)
	id := benchDeck(b, pool, 100) // 5200 cartes
	piger := func() error {
		_, err := pool.SubmitWrite(context.Background(), func(ctx context.Context, tx *sql.Tx) (any, error) {
			return DrawCards(ctx, tx, id, 1)
		})
		return err
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := piger()
		if errors.Is(err, models.ErrDeckEmpty) {
			// Deck vide : on remet toutes les cartes (pas compté dans le temps).
			b.StopTimer()
			pool.SubmitWrite(context.Background(), func(ctx context.Context, tx *sql.Tx) (any, error) {
				return ShuffleDeck(ctx, tx, id, false)
			})
			b.StartTimer()
			err = piger()
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCreateDeck : créer un deck de 52 cartes.
func BenchmarkCreateDeck(b *testing.B) {
	pool := benchPool(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchDeck(b, pool, 1)
	}
}
