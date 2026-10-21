package handlers

import (
	"context"
	"database/sql"
	"net/http"

	"TP1/database"
	"TP1/models"
	"TP1/utils"
)

const (
	maxDeckCount = 100
	maxCount     = maxDeckCount * 54 // nombre de cartes du plus gros deck possible
)

// parseNewDeck lit deck_count, jokers_enabled et cards (deck partiel).
func parseNewDeck(r *http.Request, shuffle bool) (database.NewDeckParams, error) {
	q := r.URL.Query()
	p := database.NewDeckParams{Shuffle: shuffle}
	var err error
	if p.DeckCount, err = utils.ParseIntParam(q.Get("deck_count"), 1, 1, maxDeckCount); err != nil {
		return p, err
	}
	if p.Jokers, err = utils.ParseBoolParam(q.Get("jokers_enabled"), false); err != nil {
		return p, err
	}
	if q.Has("cards") {
		p.Cards, err = utils.ParseCards(q.Get("cards"))
	}
	return p, err
}

// newDeck : /api/deck/new/ et /api/deck/new/shuffle/
func (s *Server) newDeck(shuffle bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := parseNewDeck(r, shuffle)
		if err != nil {
			writeError(w, "", err)
			return
		}
		res, err := s.pool.SubmitWrite(r.Context(), func(ctx context.Context, tx *sql.Tx) (any, error) {
			return database.CreateDeck(ctx, tx, p)
		})
		if err != nil {
			writeError(w, "", err)
			return
		}
		deck := res.(database.DeckInfo)
		s.cache.Add(deck.ID, deck.DeckCount) // après le commit : le deck existe en base
		writeJSON(w, http.StatusOK, models.Response{
			Success:   true,
			DeckID:    deck.ID,
			Shuffled:  models.Bool(deck.Shuffled),
			Remaining: models.Int(deck.Remaining),
		})
		s.hist.Ajouter(r, "create_deck", deck.ID) // après la réponse
	}
}

// newAndDraw : /api/deck/new/draw/ crée un deck mélangé et pige dans le même job.
func (s *Server) newAndDraw(w http.ResponseWriter, r *http.Request) {
	p, err := parseNewDeck(r, true)
	if err != nil {
		writeError(w, "", err)
		return
	}
	count, err := utils.ParseIntParam(r.URL.Query().Get("count"), 1, 1, maxCount)
	if err != nil {
		writeError(w, "", err)
		return
	}

	type resultat struct {
		deck database.DeckInfo
		pige database.DrawResult
	}
	res, err := s.pool.SubmitWrite(r.Context(), func(ctx context.Context, tx *sql.Tx) (any, error) {
		deck, pige, err := database.CreateAndDraw(ctx, tx, p, count)
		return resultat{deck, pige}, err
	})
	if err != nil {
		writeError(w, "", err)
		return
	}
	out := res.(resultat)
	s.cache.Add(out.deck.ID, out.deck.DeckCount)
	writeJSON(w, http.StatusOK, models.Response{
		Success:   true,
		DeckID:    out.deck.ID,
		Cards:     s.cards(out.pige.Codes),
		Remaining: models.Int(out.pige.Remaining),
	})
	s.hist.Ajouter(r, "create_and_draw", out.deck.ID)
}

// shuffle : /api/deck/{deck_id}/shuffle/ (?remaining=true)
func (s *Server) shuffle(w http.ResponseWriter, r *http.Request) {
	deckID := r.PathValue("deck_id")
	if err := s.checkDeck(deckID, false); err != nil {
		writeError(w, deckID, err)
		return
	}
	onlyRemaining, err := utils.ParseBoolParam(r.URL.Query().Get("remaining"), false)
	if err != nil {
		writeError(w, deckID, err)
		return
	}
	res, err := s.pool.SubmitWrite(r.Context(), func(ctx context.Context, tx *sql.Tx) (any, error) {
		return database.ShuffleDeck(ctx, tx, deckID, onlyRemaining)
	})
	if err != nil {
		writeError(w, deckID, err)
		return
	}
	writeJSON(w, http.StatusOK, models.Response{
		Success:   true,
		DeckID:    deckID,
		Shuffled:  models.Bool(true),
		Remaining: models.Int(res.(int)),
	})
	s.hist.Ajouter(r, "shuffle", deckID)
}

// draw : /api/deck/{deck_id}/draw/?count=2
func (s *Server) draw(w http.ResponseWriter, r *http.Request) {
	deckID := r.PathValue("deck_id")
	if err := s.checkDeck(deckID, false); err != nil {
		writeError(w, deckID, err)
		return
	}
	count, err := utils.ParseIntParam(r.URL.Query().Get("count"), 1, 1, maxCount)
	if err != nil {
		writeError(w, deckID, err)
		return
	}
	res, err := s.pool.SubmitWrite(r.Context(), func(ctx context.Context, tx *sql.Tx) (any, error) {
		return database.DrawCards(ctx, tx, deckID, count)
	})
	if err != nil {
		writeError(w, deckID, err)
		return
	}
	pige := res.(database.DrawResult)
	writeJSON(w, http.StatusOK, models.Response{
		Success:   true,
		DeckID:    deckID,
		Cards:     s.cards(pige.Codes),
		Remaining: models.Int(pige.Remaining),
	})
	s.hist.Ajouter(r, "draw", deckID)
}
