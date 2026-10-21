package database

import (
	"context"
	"database/sql"
	"math/rand/v2"
	"slices"

	"TP1/models"
)

// PileState : cartes restantes dans le deck et nombre de cartes de chaque pile.
type PileState struct {
	Remaining int
	Piles     map[string]int
}

// PileView ajoute le contenu de la pile demandée, du bas vers le haut.
type PileView struct {
	PileState
	Codes []string
}

// PileDrawResult : cartes pigées dans une pile et état après la pige.
type PileDrawResult struct {
	Codes []string
	State PileState
}

// DrawMode : d'où on pige dans une pile.
type DrawMode string

const (
	ModeTop    DrawMode = "top"    // dernière carte ajoutée (LIFO)
	ModeBottom DrawMode = "bottom" // première carte ajoutée (FIFO)
	ModeRandom DrawMode = "random"
)

// AddToPile déplace des cartes pigées vers une pile. La pile est créée si
// elle n'existe pas.
func AddToPile(ctx context.Context, tx *sql.Tx, deckID, pile string, codes []string) (PileState, error) {
	if err := checkSingleDeck(ctx, tx, deckID); err != nil {
		return PileState{}, err
	}
	// Toutes les cartes sont vérifiées avant de modifier quoi que ce soit.
	cartes, err := findDrawn(ctx, tx, deckID, codes)
	if err != nil {
		return PileState{}, err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO piles (deck_id, name) VALUES (?, ?)`, deckID, pile); err != nil {
		return PileState{}, err
	}
	var derniere int
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(pile_position), 0) FROM cards
		WHERE deck_id = ? AND state = 'pile' AND pile_name = ?`, deckID, pile).Scan(&derniere)
	if err != nil {
		return PileState{}, err
	}
	if err := putInPile(ctx, tx, cartes, pile, derniere+1); err != nil {
		return PileState{}, err
	}
	return pileState(ctx, tx, deckID)
}

// ListPile renvoie l'état de toutes les piles et le contenu de la pile demandée.
// C'est un job de lecture. La transaction sert à ce que les trois lectures
// voient la base au même moment.
func ListPile(ctx context.Context, db *sql.DB, deckID, pile string) (PileView, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return PileView{}, err
	}
	defer tx.Rollback()

	if err := checkPile(ctx, tx, deckID, pile); err != nil {
		return PileView{}, err
	}
	st, err := pileState(ctx, tx, deckID)
	if err != nil {
		return PileView{}, err
	}
	cartes, err := pileCards(ctx, tx, deckID, pile)
	if err != nil {
		return PileView{}, err
	}
	return PileView{PileState: st, Codes: codesOf(cartes)}, nil
}

// ShufflePile mélange une pile sans toucher au deck ni aux autres piles.
func ShufflePile(ctx context.Context, tx *sql.Tx, deckID, pile string) (PileState, error) {
	if err := checkPile(ctx, tx, deckID, pile); err != nil {
		return PileState{}, err
	}
	cartes, err := pileCards(ctx, tx, deckID, pile)
	if err != nil {
		return PileState{}, err
	}
	if len(cartes) == 0 {
		return PileState{}, models.ErrPileEmpty
	}
	rand.Shuffle(len(cartes), func(i, j int) { cartes[i], cartes[j] = cartes[j], cartes[i] })
	if err := putInPile(ctx, tx, cartes, pile, 1); err != nil {
		return PileState{}, err
	}
	return pileState(ctx, tx, deckID)
}

// DrawFromPile retire des cartes d'une pile : elles redeviennent « pigées ».
// codes non vide : ces cartes précises (prioritaire sur count et le mode).
// Sinon : count cartes selon le mode (top, bottom ou random).
func DrawFromPile(ctx context.Context, tx *sql.Tx, deckID, pile string,
	mode DrawMode, codes []string, count int) (PileDrawResult, error) {

	if err := checkPile(ctx, tx, deckID, pile); err != nil {
		return PileDrawResult{}, err
	}
	cartes, err := pileCards(ctx, tx, deckID, pile)
	if err != nil {
		return PileDrawResult{}, err
	}
	if len(cartes) == 0 {
		return PileDrawResult{}, models.ErrPileEmpty
	}

	var choisies []carte
	switch {
	case len(codes) > 0:
		choisies, err = findCodes(cartes, codes, models.ErrCardNotInPile)
		if err != nil {
			return PileDrawResult{}, err
		}
	case count > len(cartes):
		return PileDrawResult{}, models.ErrNotEnoughCards
	case mode == ModeBottom:
		choisies = cartes[:count]
	case mode == ModeRandom:
		// Seule pige au hasard de l'API (le deck, lui, ne pige que sur le dessus).
		rand.Shuffle(len(cartes), func(i, j int) { cartes[i], cartes[j] = cartes[j], cartes[i] })
		choisies = cartes[:count]
	default: // top : on part de la fin (le haut de la pile)
		for i := len(cartes) - 1; i >= len(cartes)-count; i-- {
			choisies = append(choisies, cartes[i])
		}
	}

	if err := markDrawn(ctx, tx, choisies); err != nil {
		return PileDrawResult{}, err
	}
	st, err := pileState(ctx, tx, deckID)
	return PileDrawResult{Codes: codesOf(choisies), State: st}, err
}

// checkSingleDeck : le deck doit exister et ne pas être un deck multiple.
func checkSingleDeck(ctx context.Context, tx *sql.Tx, deckID string) error {
	n, err := deckCount(ctx, tx, deckID)
	if err != nil {
		return err
	}
	if n > 1 {
		return models.ErrPileMultipleDecks
	}
	return nil
}

// checkPile : comme checkSingleDeck, et la pile doit exister.
func checkPile(ctx context.Context, tx *sql.Tx, deckID, pile string) error {
	if err := checkSingleDeck(ctx, tx, deckID); err != nil {
		return err
	}
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM piles WHERE deck_id = ? AND name = ?`, deckID, pile).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		return models.ErrPileNotFound
	}
	return nil
}

// pileCards renvoie les cartes d'une pile, du bas vers le haut.
func pileCards(ctx context.Context, tx *sql.Tx, deckID, pile string) ([]carte, error) {
	return selectCards(ctx, tx, `SELECT id, code FROM cards
		WHERE deck_id = ? AND state = 'pile' AND pile_name = ? ORDER BY pile_position`, deckID, pile)
}

// pileState compte les cartes du deck et de chaque pile (piles vides comprises).
func pileState(ctx context.Context, tx *sql.Tx, deckID string) (PileState, error) {
	st := PileState{Piles: map[string]int{}}
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cards WHERE deck_id = ? AND state = 'deck'`, deckID).Scan(&st.Remaining)
	if err != nil {
		return PileState{}, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT p.name, COUNT(c.id) FROM piles p
		LEFT JOIN cards c ON c.deck_id = p.deck_id AND c.state = 'pile' AND c.pile_name = p.name
		WHERE p.deck_id = ? GROUP BY p.name`, deckID)
	if err != nil {
		return PileState{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var nom string
		var n int
		if err := rows.Scan(&nom, &n); err != nil {
			return PileState{}, err
		}
		st.Piles[nom] = n
	}
	return st, rows.Err()
}

// findCodes retrouve dans cartes la carte de chaque code demandé.
func findCodes(cartes []carte, codes []string, absente error) ([]carte, error) {
	var choisies []carte
	for _, code := range codes {
		i := slices.IndexFunc(cartes, func(c carte) bool { return c.code == code })
		if i < 0 {
			return nil, absente
		}
		choisies = append(choisies, cartes[i])
	}
	return choisies, nil
}

// findDrawn retrouve les cartes pigées (hors piles) qui ont ces codes. Une carte
// qui n'existe pas dans le deck donne Card not found in deck ; une carte du
// deck qui n'est pas pigée (encore dans le deck ou dans une pile) donne
// Invalid parameter.
func findDrawn(ctx context.Context, tx *sql.Tx, deckID string, codes []string) ([]carte, error) {
	pigees, err := selectCards(ctx, tx,
		`SELECT id, code FROM cards WHERE deck_id = ? AND state = 'drawn' ORDER BY id`, deckID)
	if err != nil {
		return nil, err
	}
	for _, code := range codes {
		if slices.ContainsFunc(pigees, func(c carte) bool { return c.code == code }) {
			continue
		}
		var n int
		err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM cards WHERE deck_id = ? AND code = ?`, deckID, code).Scan(&n)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, models.ErrCardNotInDeck
		}
		return nil, models.ErrInvalidParameter
	}
	return findCodes(pigees, codes, models.ErrInvalidParameter)
}
