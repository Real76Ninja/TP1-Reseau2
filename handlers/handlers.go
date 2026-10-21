// Package handlers contient les handlers HTTP, les middlewares, le cache des
// decks et l'historique des opérations.
package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"TP1/database"
	"TP1/models"
	"TP1/utils"
)

// Server regroupe ce dont les handlers ont besoin.
type Server struct {
	pool    *database.WorkerPool
	cache   *DeckCache
	hist    *Historique
	baseURL string
}

func New(pool *database.WorkerPool, cache *DeckCache, hist *Historique, baseURL string) *Server {
	return &Server{pool: pool, cache: cache, hist: hist, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// Register ajoute les routes au mux. Pour /api/deck/new/shuffle/ et
// /api/deck/new/draw/, la route avec « new » est plus précise que celle avec
// {deck_id} : c'est donc elle que le ServeMux choisit.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/deck/new/{$}", s.newDeck(false))
	mux.HandleFunc("GET /api/deck/new/shuffle/{$}", s.newDeck(true))
	mux.HandleFunc("GET /api/deck/new/draw/{$}", s.newAndDraw)
	mux.HandleFunc("GET /api/deck/{deck_id}/shuffle/{$}", s.shuffle)
	mux.HandleFunc("GET /api/deck/{deck_id}/draw/{$}", s.draw)
	mux.HandleFunc("GET /api/deck/{deck_id}/return/{$}", s.returnCards)

	mux.HandleFunc("GET /api/deck/{deck_id}/pile/{pile_name}/add/{$}", s.pileAdd)
	mux.HandleFunc("GET /api/deck/{deck_id}/pile/{pile_name}/list/{$}", s.pileList)
	mux.HandleFunc("GET /api/deck/{deck_id}/pile/{pile_name}/shuffle/{$}", s.pileShuffle)
	mux.HandleFunc("GET /api/deck/{deck_id}/pile/{pile_name}/return/{$}", s.returnCards)
	mux.HandleFunc("GET /api/deck/{deck_id}/pile/{pile_name}/draw/{$}", s.pileDraw(database.ModeTop))
	mux.HandleFunc("GET /api/deck/{deck_id}/pile/{pile_name}/draw/top/{$}", s.pileDraw(database.ModeTop))
	mux.HandleFunc("GET /api/deck/{deck_id}/pile/{pile_name}/draw/bottom/{$}", s.pileDraw(database.ModeBottom))
	mux.HandleFunc("GET /api/deck/{deck_id}/pile/{pile_name}/draw/random/{$}", s.pileDraw(database.ModeRandom))
}

// checkDeck utilise seulement le cache (pas la base) pour rejeter un deck
// inconnu, ou une opération de pile sur un deck multiple.
func (s *Server) checkDeck(deckID string, pile bool) error {
	n, ok := s.cache.Get(deckID)
	if !ok {
		return models.ErrDeckNotFound
	}
	if pile && n > 1 {
		return models.ErrPileMultipleDecks
	}
	return nil
}

// cards convertit des codes en cartes JSON.
func (s *Server) cards(codes []string) []models.Card {
	cards := make([]models.Card, len(codes))
	for i, code := range codes {
		cards[i] = utils.CardFromCode(code, s.baseURL)
	}
	return cards
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, deckID string, err error) {
	status := errorStatus(err)
	if status == http.StatusInternalServerError {
		log.Printf("erreur : %v", err) // erreur SQL : le client voit seulement "Database error"
		err = models.ErrDatabase
	}
	writeJSON(w, status, models.ErrorResponse(deckID, err))
}

func errorStatus(err error) int {
	switch {
	case errors.Is(err, models.ErrDeckNotFound),
		errors.Is(err, models.ErrDeckEmpty),
		errors.Is(err, models.ErrCardNotInPile),
		errors.Is(err, models.ErrCardNotInDeck),
		errors.Is(err, models.ErrPileNotFound),
		errors.Is(err, models.ErrPileEmpty):
		return http.StatusNotFound
	case errors.Is(err, models.ErrNotEnoughCards),
		errors.Is(err, models.ErrInvalidCardCode),
		errors.Is(err, models.ErrDuplicateCards),
		errors.Is(err, models.ErrPileMultipleDecks),
		errors.Is(err, models.ErrInvalidParameter),
		errors.Is(err, models.ErrParameterOutOfRange):
		return http.StatusBadRequest
	case errors.Is(err, models.ErrTimeout):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
