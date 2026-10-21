package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func newTestPool(t *testing.T, lecteurs int) *WorkerPool {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"), lecteurs)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := NewWorkerPool(db.Read, db.Write, lecteurs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		db.Close()
	})
	return pool
}

// maxEnMemeTemps lance n jobs en même temps et renvoie le nombre maximal de
// jobs qui se sont exécutés en même temps.
func maxEnMemeTemps(n int, soumettre func(job func())) int {
	var mu sync.Mutex // protège actifs et record
	actifs, record := 0, 0
	job := func() {
		mu.Lock()
		actifs++
		if actifs > record {
			record = actifs
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		actifs--
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			soumettre(job)
		}()
	}
	wg.Wait()
	return record
}

// Les écritures passent une à la fois, les lectures en parallèle.
func TestPoolReadersWriter(t *testing.T) {
	pool := newTestPool(t, 3)
	ctx := context.Background()

	ecritures := maxEnMemeTemps(10, func(job func()) {
		pool.SubmitWrite(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
			job()
			return nil, nil
		})
	})
	if ecritures != 1 {
		t.Errorf("%d écritures en même temps, attendu 1", ecritures)
	}

	lectures := maxEnMemeTemps(10, func(job func()) {
		pool.SubmitRead(ctx, func(ctx context.Context, db *sql.DB) (any, error) {
			job()
			return nil, nil
		})
	})
	if lectures < 2 {
		t.Errorf("%d lecture à la fois avec 3 lecteurs, attendu plus", lectures)
	}
}

// Close() se termine (aucun worker bloqué) et toutes les goroutines du pool
// sont arrêtées.
func TestPoolClose(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"), 3)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	avant := runtime.NumGoroutine()

	pool, err := NewWorkerPool(db.Read, db.Write, 3)
	if err != nil {
		t.Fatal(err)
	}
	pool.SubmitWrite(context.Background(), func(ctx context.Context, tx *sql.Tx) (any, error) {
		return nil, nil
	})

	fini := make(chan struct{})
	go func() {
		pool.Close()
		close(fini)
	}()
	select {
	case <-fini:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() est bloqué")
	}

	// Une goroutine terminée peut prendre un instant avant de disparaître du compte.
	for i := 0; i < 100 && runtime.NumGoroutine() > avant; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if apres := runtime.NumGoroutine(); apres > avant {
		t.Fatalf("%d goroutines avant le pool, %d après Close() : fuite", avant, apres)
	}
}
