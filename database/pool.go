package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"TP1/models"
)

// ReadJob est exécuté par un worker lecteur.
type ReadJob func(ctx context.Context, db *sql.DB) (any, error)

// WriteJob est exécuté par le worker écrivain, dans une transaction. C'est le
// worker qui fait le Commit, ou le Rollback si le job renvoie une erreur.
type WriteJob func(ctx context.Context, tx *sql.Tx) (any, error)

// DBRequest est un job envoyé au pool (Read ou Write est rempli).
type DBRequest struct {
	Ctx      context.Context
	Read     ReadJob
	Write    WriteJob
	Response chan DBResponse
}

// DBResponse est le résultat d'un job.
type DBResponse struct {
	Data  any
	Error error
}

// WorkerPool : un channel de lectures servi par 1 à 3 workers, et un channel
// d'écritures servi par un seul worker.
type WorkerPool struct {
	lectures   chan DBRequest
	ecritures  chan DBRequest
	dbLecture  *sql.DB
	dbEcriture *sql.DB
	wg         sync.WaitGroup
	fermeture  sync.Once
}

// tailleFile : nombre de jobs qui peuvent attendre dans chaque channel.
const tailleFile = 100

// NewWorkerPool démarre le worker écrivain et les workers lecteurs.
func NewWorkerPool(dbLecture, dbEcriture *sql.DB, lecteurs int) (*WorkerPool, error) {
	if lecteurs < 1 || lecteurs > 3 {
		return nil, fmt.Errorf("nombre de lecteurs invalide : %d (1 à 3)", lecteurs)
	}
	p := &WorkerPool{
		lectures:   make(chan DBRequest, tailleFile),
		ecritures:  make(chan DBRequest, tailleFile),
		dbLecture:  dbLecture,
		dbEcriture: dbEcriture,
	}

	// Un seul écrivain : les écritures sont faites une à la fois, dans l'ordre
	// d'arrivée. Deux piges simultanées ne peuvent donc pas prendre la même carte.
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for req := range p.ecritures {
			req.Response <- p.executerEcriture(req)
		}
	}()

	for i := 0; i < lecteurs; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for req := range p.lectures {
				req.Response <- p.executerLecture(req)
			}
		}()
	}
	return p, nil
}

func (p *WorkerPool) executerLecture(req DBRequest) DBResponse {
	// Client parti ou délai dépassé : on n'exécute pas le job.
	if err := req.Ctx.Err(); err != nil {
		return DBResponse{Error: err}
	}
	data, err := req.Read(req.Ctx, p.dbLecture)
	return DBResponse{Data: data, Error: err}
}

func (p *WorkerPool) executerEcriture(req DBRequest) DBResponse {
	if err := req.Ctx.Err(); err != nil {
		return DBResponse{Error: err}
	}
	tx, err := p.dbEcriture.BeginTx(req.Ctx, nil)
	if err != nil {
		return DBResponse{Error: err}
	}
	defer tx.Rollback() // sans effet si le Commit a réussi

	data, err := req.Write(req.Ctx, tx)
	if err != nil {
		return DBResponse{Error: err}
	}
	if err := tx.Commit(); err != nil {
		return DBResponse{Error: err}
	}
	return DBResponse{Data: data}
}

// SubmitRead envoie un job de lecture et attend sa réponse.
func (p *WorkerPool) SubmitRead(ctx context.Context, job ReadJob) (any, error) {
	return p.submit(ctx, p.lectures, DBRequest{Read: job})
}

// SubmitWrite envoie un job d'écriture et attend sa réponse.
func (p *WorkerPool) SubmitWrite(ctx context.Context, job WriteJob) (any, error) {
	return p.submit(ctx, p.ecritures, DBRequest{Write: job})
}

func (p *WorkerPool) submit(ctx context.Context, file chan DBRequest, req DBRequest) (any, error) {
	req.Ctx = ctx
	// Channel de réponse propre à cette requête, de capacité 1 : le worker peut
	// toujours y déposer sa réponse, même si le handler est parti (timeout).
	req.Response = make(chan DBResponse, 1)

	select {
	case file <- req:
	case <-ctx.Done(): // file pleine jusqu'à la fin du délai
		return nil, models.ErrTimeout
	}

	select {
	case resp := <-req.Response:
		if resp.Error != nil && ctx.Err() != nil {
			return nil, models.ErrTimeout // job pas exécuté ou interrompu par le délai
		}
		return resp.Data, resp.Error
	case <-ctx.Done():
		return nil, models.ErrTimeout
	}
}

// Close ferme les channels et attend la fin des workers (les jobs déjà en file
// sont traités avant). Aucun job ne doit être soumis après Close : c'est pour
// ça que main attend la fin de l'historique avant d'appeler Close.
func (p *WorkerPool) Close() {
	p.fermeture.Do(func() {
		close(p.lectures)
		close(p.ecritures)
	})
	p.wg.Wait()
}
