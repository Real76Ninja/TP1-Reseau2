package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"TP1/utils"
)

// 52 piges simultanées d'une carte sur un deck non mélangé : ensemble, les
// réponses doivent contenir les 52 cartes, chacune une seule fois.
func TestLoadDrawAllCards(t *testing.T) {
	e := newEnv(t, 30*time.Second)
	d := "/api/deck/" + e.ok(t, "/api/deck/new/").DeckID

	var mu sync.Mutex // protège recues : plusieurs goroutines y ajoutent
	var recues []string
	var wg sync.WaitGroup
	debut := time.Now()
	for i := 0; i < 52; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, r, err := e.get(d + "/draw/?count=1")
			if err != nil || code != http.StatusOK || len(r.Cards) != 1 {
				t.Errorf("pige : code %d, %+v, %v", code, r, err)
				return
			}
			mu.Lock()
			recues = append(recues, r.Cards[0].Code)
			mu.Unlock()
		}()
	}
	wg.Wait()
	duree := time.Since(debut)
	t.Logf("52 requêtes simultanées en %v : %.0f requêtes/s", duree, 52/duree.Seconds())

	attendues := utils.DeckCodes(1, false)
	slices.Sort(attendues)
	slices.Sort(recues)
	if !slices.Equal(recues, attendues) {
		t.Fatalf("cartes reçues : %v", recues)
	}
}

// 140 requêtes simultanées sur le même deck : 120 piges et 20 mélanges des
// cartes restantes. Il n'y a que 52 cartes : 52 piges réussissent, les 68
// autres reçoivent « Deck is empty », et aucune carte n'est perdue ou en double.
func TestLoadMixedSameDeck(t *testing.T) {
	e := newEnv(t, 30*time.Second)
	d := "/api/deck/" + e.ok(t, "/api/deck/new/").DeckID

	var mu sync.Mutex // protège recues et vides
	var recues []string
	vides := 0
	var wg sync.WaitGroup
	debut := time.Now()
	for i := 0; i < 120; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, r, err := e.get(d + "/draw/")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && code == http.StatusOK && len(r.Cards) == 1:
				recues = append(recues, r.Cards[0].Code)
			case err == nil && code == http.StatusNotFound && r.Error == "Deck is empty":
				vides++
			default:
				t.Errorf("pige : code %d, %+v, %v", code, r, err)
			}
		}()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, r, err := e.get(d + "/shuffle/?remaining=true")
			if err != nil || code != http.StatusOK || !r.Success {
				t.Errorf("mélange : code %d, %+v, %v", code, r, err)
			}
		}()
	}
	wg.Wait()
	duree := time.Since(debut)
	t.Logf("140 requêtes simultanées en %v : %.0f requêtes/s", duree, 140/duree.Seconds())

	attendues := utils.DeckCodes(1, false)
	slices.Sort(attendues)
	slices.Sort(recues)
	if !slices.Equal(recues, attendues) || vides != 68 {
		t.Fatalf("%d cartes reçues, %d « Deck is empty » : %v", len(recues), vides, recues)
	}
}

// Dix decks utilisés en même temps (20 piges chacun) : chaque deck reçoit ses
// 20 premières cartes, différentes et consécutives, sans interférence.
func TestIsolationBetweenDecks(t *testing.T) {
	e := newEnv(t, 30*time.Second)
	decks := make([]string, 10)
	for i := range decks {
		decks[i] = "/api/deck/" + e.ok(t, "/api/deck/new/").DeckID
	}

	var mu sync.Mutex // protège recues
	recues := map[string][]string{}
	var wg sync.WaitGroup
	for _, d := range decks {
		for j := 0; j < 20; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				code, r, err := e.get(d + "/draw/")
				if err != nil || code != http.StatusOK || len(r.Cards) != 1 {
					t.Errorf("%s : code %d, %+v, %v", d, code, r, err)
					return
				}
				mu.Lock()
				recues[d] = append(recues[d], r.Cards[0].Code)
				mu.Unlock()
			}()
		}
	}
	wg.Wait()

	premieres := utils.DeckCodes(1, false)[:20]
	slices.Sort(premieres)
	for _, d := range decks {
		slices.Sort(recues[d])
		if !slices.Equal(recues[d], premieres) {
			t.Errorf("%s : cartes reçues %v", d, recues[d])
		}
		if r := e.ok(t, d+"/draw/"); r.Cards[0].Code != "8D" || *r.Remaining != 31 {
			t.Errorf("%s : la 21e carte devrait être 8D : %+v", d, r)
		}
	}
}

// Le worker écrivain est bloqué par un job lent : la requête dépasse son délai
// et reçoit 503 « Request timeout ». Son job, abandonné, n'est jamais exécuté.
func TestTimeout(t *testing.T) {
	e := newEnv(t, 200*time.Millisecond)
	d := "/api/deck/" + e.ok(t, "/api/deck/new/").DeckID

	commence := make(chan struct{})
	go e.pool.SubmitWrite(context.Background(), func(ctx context.Context, tx *sql.Tx) (any, error) {
		close(commence)
		time.Sleep(time.Second) // worker volontairement lent
		return nil, nil
	})
	<-commence

	e.erreur(t, d+"/draw/", http.StatusServiceUnavailable, "Request timeout")

	// Ce job vide passe après la pige abandonnée (les channels sont FIFO) :
	// quand il a fini, le worker a déjà vu la pige et l'a ignorée.
	e.pool.SubmitWrite(context.Background(), func(ctx context.Context, tx *sql.Tx) (any, error) {
		return nil, nil
	})
	var restantes int
	e.db.Read.QueryRow(`SELECT COUNT(*) FROM cards WHERE state = 'deck'`).Scan(&restantes)
	if restantes != 52 {
		t.Fatalf("%d cartes dans le deck : la pige abandonnée a été exécutée", restantes)
	}
}
