package handlers

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"sync"
	"time"

	"TP1/database"
)

// Historique écrit chaque opération réussie qui modifie des données dans la
// table historique, après la réponse au client.
type Historique struct {
	pool *database.WorkerPool
	wg   sync.WaitGroup // goroutines d'historique en cours
}

func NewHistorique(pool *database.WorkerPool) *Historique {
	return &Historique{pool: pool}
}

// Ajouter est appelée par le handler après avoir écrit sa réponse : l'écriture
// se fait dans une goroutine, le client n'a pas à l'attendre.
func (h *Historique) Ajouter(r *http.Request, operation, deckID string) {
	// À la fin du handler, r.Context() est annulé. WithoutCancel garde ses
	// valeurs (l'identifiant de requête) sans être annulé avec lui.
	ctx := context.WithoutCancel(r.Context())
	requestID := RequestIDFrom(ctx)

	h.wg.Add(1) // avant le go, sinon Attendre() pourrait finir avant le Add
	go func() {
		defer h.wg.Done()
		// Une panique ici n'est pas rattrapée par le middleware Recover et
		// arrêterait tout le serveur.
		defer func() {
			if p := recover(); p != nil {
				log.Printf("historique : panique rattrapée : %v", p)
			}
		}()

		// Délai propre à l'historique, pour ne jamais rester bloqué.
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, err := h.pool.SubmitWrite(ctx, func(ctx context.Context, tx *sql.Tx) (any, error) {
			return nil, database.InsertHistory(ctx, tx, requestID, operation, deckID)
		})
		if err != nil {
			log.Printf("historique (%s, %s) : %v", operation, requestID, err)
		}
	}()
}

// Attendre attend la fin des goroutines d'historique. À appeler après l'arrêt
// du serveur HTTP et avant pool.Close() : une goroutine qui enverrait un job
// sur un channel déjà fermé provoquerait une panique.
func (h *Historique) Attendre() {
	h.wg.Wait()
}
