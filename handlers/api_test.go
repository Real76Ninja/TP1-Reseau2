package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"TP1/database"
	"TP1/models"
	"TP1/utils"
)

// env : un vrai serveur de test (httptest) avec une base SQLite temporaire,
// le pool, le cache, l'historique et les middlewares, comme dans main.
type env struct {
	url  string
	db   *database.DB
	pool *database.WorkerPool
	hist *Historique
}

func newEnv(t *testing.T, timeout time.Duration) *env {
	t.Helper()
	db, err := database.OpenDB(filepath.Join(t.TempDir(), "test.db"), 2)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.NewWorkerPool(db.Read, db.Write, 2)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := LoadDeckCache(context.Background(), db.Read)
	if err != nil {
		t.Fatal(err)
	}
	hist := NewHistorique(pool)

	mux := http.NewServeMux()
	New(pool, cache, hist, "http://localhost:8080").Register(mux)
	ts := httptest.NewServer(Recover(Timeout(timeout)(RequestID(mux))))

	t.Cleanup(func() { // même ordre d'arrêt que dans main
		ts.Close()
		hist.Attendre()
		pool.Close()
		db.Close()
	})
	return &env{url: ts.URL, db: db, pool: pool, hist: hist}
}

// get fait la requête et décode la réponse. Pas de t.Fatal ici : on peut
// l'appeler depuis plusieurs goroutines.
func (e *env) get(path string) (int, models.Response, error) {
	resp, err := http.Get(e.url + path)
	if err != nil {
		return 0, models.Response{}, err
	}
	defer resp.Body.Close()
	var r models.Response
	err = json.NewDecoder(resp.Body).Decode(&r)
	return resp.StatusCode, r, err
}

// ok exige une réponse 200 avec success = true.
func (e *env) ok(t *testing.T, path string) models.Response {
	t.Helper()
	code, r, err := e.get(path)
	if err != nil || code != http.StatusOK || !r.Success {
		t.Fatalf("GET %s : code %d, %+v, %v", path, code, r, err)
	}
	return r
}

// erreur exige ce code HTTP et ce message d'erreur.
func (e *env) erreur(t *testing.T, path string, code int, message string) {
	t.Helper()
	c, r, err := e.get(path)
	if err != nil || c != code || r.Success || r.Error != message {
		t.Errorf("GET %s : code %d, erreur %q (%v) ; attendu %d %q", path, c, r.Error, err, code, message)
	}
}

func codes(cards []models.Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.Code
	}
	return out
}

func TestErrors(t *testing.T) {
	e := newEnv(t, 5*time.Second)
	d := "/api/deck/" + e.ok(t, "/api/deck/new/").DeckID
	sabot := "/api/deck/" + e.ok(t, "/api/deck/new/?deck_count=6").DeckID
	partiel := "/api/deck/" + e.ok(t, "/api/deck/new/?cards=AS,2S").DeckID

	e.erreur(t, "/api/deck/inconnu/draw/", 404, "Deck not found")
	e.erreur(t, d+"/draw/?count=60", 400, "Not enough cards remaining")
	e.erreur(t, d+"/draw/?count=abc", 400, "Invalid parameter")
	e.erreur(t, "/api/deck/new/?deck_count=0", 400, "Parameter out of range")
	e.erreur(t, "/api/deck/new/?cards=AS,99Z", 400, "Invalid card code")
	e.erreur(t, "/api/deck/new/?cards=AS,as", 400, "Duplicate cards in request")
	e.erreur(t, d+"/pile/main/add/", 400, "Invalid parameter") // cards est obligatoire
	e.erreur(t, d+"/pile/main/list/", 404, "Pile not found")
	e.erreur(t, partiel+"/pile/main/add/?cards=KH", 404, "Card not found in deck")
	e.erreur(t, sabot+"/pile/main/list/", 400, "Pile operations not supported with multiple decks")

	e.ok(t, d+"/draw/") // AS
	e.ok(t, d+"/pile/main/add/?cards=AS")
	e.erreur(t, d+"/pile/main/draw/?cards=KS", 404, "Card not found in pile")
	e.ok(t, d+"/pile/main/draw/")
	e.erreur(t, d+"/pile/main/draw/", 404, "Pile is empty")

	e.ok(t, d+"/draw/?count=51")
	e.erreur(t, d+"/draw/", 404, "Deck is empty")
}

func TestNewDecks(t *testing.T) {
	e := newEnv(t, 5*time.Second)
	for _, c := range []struct {
		path      string
		remaining int
		shuffled  bool
	}{
		{"/api/deck/new/", 52, false},
		{"/api/deck/new/?deck_count=6", 312, false},
		{"/api/deck/new/?jokers_enabled=true", 54, false},
		{"/api/deck/new/shuffle/", 52, true},
		{"/api/deck/new/shuffle/?cards=AS,2S,KS,AD,2D,KD,AC,2C,KC,AH,2H,KH", 12, true},
	} {
		r := e.ok(t, c.path)
		if *r.Remaining != c.remaining || *r.Shuffled != c.shuffled || len(r.DeckID) != 12 {
			t.Errorf("%s : %+v", c.path, r)
		}
	}

	r := e.ok(t, "/api/deck/new/draw/?count=2")
	if len(r.Cards) != 2 || *r.Remaining != 50 {
		t.Errorf("new/draw : %+v", r)
	}

	// Deck non mélangé avec jokers : les jokers sont les deux dernières cartes.
	d := "/api/deck/" + e.ok(t, "/api/deck/new/?jokers_enabled=true").DeckID
	r = e.ok(t, d+"/draw/?count=54")
	if got := codes(r.Cards[52:]); !slices.Equal(got, []string{"X1", "X2"}) || r.Cards[52].Value != "JOKER" {
		t.Errorf("jokers : %+v", r.Cards[52:])
	}
}

func TestPiles(t *testing.T) {
	e := newEnv(t, 5*time.Second)
	d := "/api/deck/" + e.ok(t, "/api/deck/new/").DeckID

	// Deck non mélangé : la pige prend le dessus, dans l'ordre.
	r := e.ok(t, d+"/draw/?count=5")
	if got := codes(r.Cards); !slices.Equal(got, []string{"AS", "2S", "3S", "4S", "5S"}) || *r.Remaining != 47 {
		t.Fatalf("pige : %+v", r)
	}

	r = e.ok(t, d+"/pile/discard/add/?cards=AS,2S,3S")
	if r.Piles["discard"].Remaining != 3 || *r.Remaining != 47 {
		t.Fatalf("ajout : %+v", r)
	}
	e.ok(t, d+"/pile/joueur1/add/?cards=4S")

	// list : les cartes de la pile demandée, seulement le nombre pour les autres.
	r = e.ok(t, d+"/pile/discard/list/")
	if got := codes(r.Piles["discard"].Cards); !slices.Equal(got, []string{"AS", "2S", "3S"}) {
		t.Fatalf("list : %v", got)
	}
	if p := r.Piles["joueur1"]; p.Remaining != 1 || p.Cards != nil {
		t.Fatalf("list, autre pile : %+v", p)
	}

	// Haut de la pile = dernière carte ajoutée, bas = première.
	if got := codes(e.ok(t, d+"/pile/discard/draw/").Cards); !slices.Equal(got, []string{"3S"}) {
		t.Fatalf("draw : %v", got)
	}
	if got := codes(e.ok(t, d+"/pile/discard/draw/bottom/").Cards); !slices.Equal(got, []string{"AS"}) {
		t.Fatalf("draw/bottom : %v", got)
	}
	r = e.ok(t, d+"/pile/discard/draw/random/")
	if got := codes(r.Cards); !slices.Equal(got, []string{"2S"}) || r.Piles["discard"].Remaining != 0 {
		t.Fatalf("draw/random : %+v", r)
	}

	// Retour d'une pile : la pile reste, avec 0 carte.
	r = e.ok(t, d+"/pile/joueur1/return/")
	if p, ok := r.Piles["joueur1"]; !ok || p.Remaining != 0 || *r.Remaining != 48 {
		t.Fatalf("retour de la pile : %+v", r)
	}
	// Retour des cartes pigées (hors piles) : 5S, puis le reste (AS, 2S, 3S).
	if r = e.ok(t, d+"/return/?cards=5S"); *r.Remaining != 49 {
		t.Fatalf("retour de 5S : %+v", r)
	}
	if r = e.ok(t, d+"/return/"); *r.Remaining != 52 || *r.Shuffled {
		t.Fatalf("retour : %+v", r)
	}

	// Les cartes retournées sont à la fin du deck, dans l'ordre du retour.
	r = e.ok(t, d+"/draw/?count=52")
	if got := codes(r.Cards); got[0] != "6S" || !slices.Equal(got[47:], []string{"4S", "5S", "AS", "2S", "3S"}) {
		t.Fatalf("ordre après les retours : %v", got)
	}
}

func TestShuffle(t *testing.T) {
	e := newEnv(t, 5*time.Second)
	d := "/api/deck/" + e.ok(t, "/api/deck/new/").DeckID
	e.ok(t, d+"/draw/?count=4") // AS 2S 3S 4S
	e.ok(t, d+"/pile/p/add/?cards=AS,2S")
	if r := e.ok(t, d+"/pile/p/shuffle/"); r.Piles["p"].Remaining != 2 {
		t.Fatalf("mélange de la pile : %+v", r)
	}

	// remaining=true : seules les 48 cartes du deck sont mélangées.
	r := e.ok(t, d+"/shuffle/?remaining=true")
	if *r.Remaining != 48 || !*r.Shuffled {
		t.Fatalf("shuffle?remaining=true : %+v", r)
	}
	if r = e.ok(t, d+"/pile/p/list/"); len(r.Piles["p"].Cards) != 2 {
		t.Fatalf("la pile a changé : %+v", r)
	}

	// Sans paramètre : toutes les cartes reviennent dans le deck, puis mélange.
	if r = e.ok(t, d+"/shuffle/"); *r.Remaining != 52 {
		t.Fatalf("shuffle : %+v", r)
	}
	r = e.ok(t, d+"/draw/?count=52")
	got, nouveau := codes(r.Cards), utils.DeckCodes(1, false)
	if slices.Equal(got, nouveau) {
		t.Fatal("le deck n'est pas mélangé")
	}
	slices.Sort(got)
	slices.Sort(nouveau)
	if !slices.Equal(got, nouveau) {
		t.Fatalf("cartes perdues ou en double : %v", got)
	}
}

// Chaque opération réussie qui modifie des données donne une ligne dans
// l'historique. Rien pour une lecture (list) ou une erreur.
func TestHistory(t *testing.T) {
	e := newEnv(t, 5*time.Second)
	d := "/api/deck/" + e.ok(t, "/api/deck/new/").DeckID
	e.ok(t, d+"/draw/?count=2")
	e.ok(t, d+"/pile/p/add/?cards=AS")
	e.ok(t, d+"/pile/p/list/")
	e.erreur(t, d+"/draw/?count=99", 400, "Not enough cards remaining")

	e.hist.Attendre() // l'historique est écrit après les réponses
	var lignes, ids int
	e.db.Read.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT request_id) FROM historique`).Scan(&lignes, &ids)
	if lignes != 3 || ids != 3 {
		t.Fatalf("%d lignes et %d identifiants de requête, attendu 3 et 3", lignes, ids)
	}
}
