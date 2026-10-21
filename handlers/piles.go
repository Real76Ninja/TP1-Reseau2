package handlers

import (
	"context"
	"database/sql"
	"net/http"

	"TP1/database"
	"TP1/models"
	"TP1/utils"
)

// pilesJSON donne la section "piles" d'une réponse : le nombre de cartes de
// chaque pile. Une map vide disparaît du JSON (omitempty).
func pilesJSON(piles map[string]int) map[string]models.PileInfo {
	out := map[string]models.PileInfo{}
	for nom, n := range piles {
		out[nom] = models.PileInfo{Remaining: n}
	}
	return out
}

// pileAdd : /api/deck/{deck_id}/pile/{pile_name}/add/?cards=AS,2S
func (s *Server) pileAdd(w http.ResponseWriter, r *http.Request) {
	deckID, pile := r.PathValue("deck_id"), r.PathValue("pile_name")
	if err := s.checkDeck(deckID, true); err != nil {
		writeError(w, deckID, err)
		return
	}
	codes, err := utils.ParseCards(r.URL.Query().Get("cards")) // cards est obligatoire ici
	if err != nil {
		writeError(w, deckID, err)
		return
	}
	res, err := s.pool.SubmitWrite(r.Context(), func(ctx context.Context, tx *sql.Tx) (any, error) {
		return database.AddToPile(ctx, tx, deckID, pile, codes)
	})
	if err != nil {
		writeError(w, deckID, err)
		return
	}
	st := res.(database.PileState)
	writeJSON(w, http.StatusOK, models.Response{
		Success:   true,
		DeckID:    deckID,
		Remaining: models.Int(st.Remaining),
		Piles:     pilesJSON(st.Piles),
	})
	s.hist.Ajouter(r, "pile_add", deckID)
}

// pileList : /api/deck/{deck_id}/pile/{pile_name}/list/ (job de lecture)
func (s *Server) pileList(w http.ResponseWriter, r *http.Request) {
	deckID, pile := r.PathValue("deck_id"), r.PathValue("pile_name")
	if err := s.checkDeck(deckID, true); err != nil {
		writeError(w, deckID, err)
		return
	}
	res, err := s.pool.SubmitRead(r.Context(), func(ctx context.Context, db *sql.DB) (any, error) {
		return database.ListPile(ctx, db, deckID, pile)
	})
	if err != nil {
		writeError(w, deckID, err)
		return
	}
	vue := res.(database.PileView)
	piles := pilesJSON(vue.Piles)
	piles[pile] = models.PileInfo{Cards: s.cards(vue.Codes), Remaining: len(vue.Codes)}
	writeJSON(w, http.StatusOK, models.Response{
		Success:   true,
		DeckID:    deckID,
		Remaining: models.Int(vue.Remaining),
		Piles:     piles,
	})
}

// pileShuffle : /api/deck/{deck_id}/pile/{pile_name}/shuffle/
func (s *Server) pileShuffle(w http.ResponseWriter, r *http.Request) {
	deckID, pile := r.PathValue("deck_id"), r.PathValue("pile_name")
	if err := s.checkDeck(deckID, true); err != nil {
		writeError(w, deckID, err)
		return
	}
	res, err := s.pool.SubmitWrite(r.Context(), func(ctx context.Context, tx *sql.Tx) (any, error) {
		return database.ShufflePile(ctx, tx, deckID, pile)
	})
	if err != nil {
		writeError(w, deckID, err)
		return
	}
	st := res.(database.PileState)
	writeJSON(w, http.StatusOK, models.Response{
		Success:   true,
		DeckID:    deckID,
		Remaining: models.Int(st.Remaining),
		Piles:     pilesJSON(st.Piles),
	})
	s.hist.Ajouter(r, "pile_shuffle", deckID)
}

// pileDraw : /api/deck/{deck_id}/pile/{pile_name}/draw/ (et /top/, /bottom/, /random/)
func (s *Server) pileDraw(mode database.DrawMode) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deckID, pile := r.PathValue("deck_id"), r.PathValue("pile_name")
		if err := s.checkDeck(deckID, true); err != nil {
			writeError(w, deckID, err)
			return
		}
		// cards= est prioritaire ; sinon count (1 par défaut).
		q := r.URL.Query()
		var codes []string
		count := 1
		var err error
		if q.Has("cards") {
			codes, err = utils.ParseCards(q.Get("cards"))
		} else {
			count, err = utils.ParseIntParam(q.Get("count"), 1, 1, maxCount)
		}
		if err != nil {
			writeError(w, deckID, err)
			return
		}

		res, err := s.pool.SubmitWrite(r.Context(), func(ctx context.Context, tx *sql.Tx) (any, error) {
			return database.DrawFromPile(ctx, tx, deckID, pile, mode, codes, count)
		})
		if err != nil {
			writeError(w, deckID, err)
			return
		}
		out := res.(database.PileDrawResult)
		writeJSON(w, http.StatusOK, models.Response{
			Success:   true,
			DeckID:    deckID,
			Remaining: models.Int(out.State.Remaining),
			Piles:     pilesJSON(out.State.Piles),
			Cards:     s.cards(out.Codes),
		})
		s.hist.Ajouter(r, "pile_draw_"+string(mode), deckID)
	}
}

// returnCards : /api/deck/{deck_id}/return/ et /api/deck/{deck_id}/pile/{pile_name}/return/
// (avec ?cards= en option). Sur la première route, pile_name est vide.
func (s *Server) returnCards(w http.ResponseWriter, r *http.Request) {
	deckID, pile := r.PathValue("deck_id"), r.PathValue("pile_name")
	if err := s.checkDeck(deckID, pile != ""); err != nil {
		writeError(w, deckID, err)
		return
	}
	var codes []string
	if r.URL.Query().Has("cards") {
		var err error
		if codes, err = utils.ParseCards(r.URL.Query().Get("cards")); err != nil {
			writeError(w, deckID, err)
			return
		}
	}
	res, err := s.pool.SubmitWrite(r.Context(), func(ctx context.Context, tx *sql.Tx) (any, error) {
		return database.ReturnCards(ctx, tx, deckID, pile, codes)
	})
	if err != nil {
		writeError(w, deckID, err)
		return
	}
	out := res.(database.ReturnResult)
	writeJSON(w, http.StatusOK, models.Response{
		Success:   true,
		DeckID:    deckID,
		Shuffled:  models.Bool(out.Shuffled),
		Remaining: models.Int(out.State.Remaining),
		Piles:     pilesJSON(out.State.Piles),
	})
	s.hist.Ajouter(r, "return", deckID)
}
