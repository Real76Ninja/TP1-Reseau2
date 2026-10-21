package database

import (
	"context"
	"database/sql"
	"errors"

	"TP1/models"
)

// ReturnResult : drapeau shuffled du deck et état après le retour.
type ReturnResult struct {
	Shuffled bool
	State    PileState
}

// ReturnCards remet des cartes à la fin du deck (elles seront pigées en dernier).
//   - pile vide : les cartes pigées qui ne sont dans aucune pile ;
//   - sinon : les cartes de cette pile (la pile continue d'exister, vide).
//
// codes vide = toutes ces cartes, sinon seulement celles demandées.
func ReturnCards(ctx context.Context, tx *sql.Tx, deckID, pile string, codes []string) (ReturnResult, error) {
	var shuffled bool
	err := tx.QueryRowContext(ctx, `SELECT shuffled FROM decks WHERE id = ?`, deckID).Scan(&shuffled)
	if errors.Is(err, sql.ErrNoRows) {
		return ReturnResult{}, models.ErrDeckNotFound
	}
	if err != nil {
		return ReturnResult{}, err
	}

	var cartes []carte
	if pile != "" {
		if err := checkPile(ctx, tx, deckID, pile); err != nil {
			return ReturnResult{}, err
		}
		if cartes, err = pileCards(ctx, tx, deckID, pile); err != nil {
			return ReturnResult{}, err
		}
		if len(cartes) == 0 {
			return ReturnResult{}, models.ErrPileEmpty
		}
		if len(codes) > 0 {
			cartes, err = findCodes(cartes, codes, models.ErrCardNotInPile)
		}
	} else if len(codes) > 0 {
		cartes, err = findDrawn(ctx, tx, deckID, codes)
	} else {
		cartes, err = selectCards(ctx, tx,
			`SELECT id, code FROM cards WHERE deck_id = ? AND state = 'drawn' ORDER BY id`, deckID)
	}
	if err != nil {
		return ReturnResult{}, err
	}

	var derniere int
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position), 0) FROM cards WHERE deck_id = ? AND state = 'deck'`,
		deckID).Scan(&derniere)
	if err != nil {
		return ReturnResult{}, err
	}
	if err := putInDeck(ctx, tx, cartes, derniere+1); err != nil {
		return ReturnResult{}, err
	}
	st, err := pileState(ctx, tx, deckID)
	return ReturnResult{Shuffled: shuffled, State: st}, err
}
