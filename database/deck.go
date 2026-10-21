package database

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"slices"

	"TP1/models"
	"TP1/utils"
)

// NewDeckParams décrit le deck à créer (paramètres déjà validés par le handler).
type NewDeckParams struct {
	DeckCount int
	Jokers    bool
	Shuffle   bool
	Cards     []string // deck partiel : remplace deck_count et jokers
}

// DeckInfo est le résultat d'une création de deck.
type DeckInfo struct {
	ID        string
	DeckCount int
	Shuffled  bool
	Remaining int
}

// DrawResult est le résultat d'une pige.
type DrawResult struct {
	Codes     []string
	Remaining int
}

// CreateDeck crée un deck et ses cartes. La position fixe l'ordre du deck :
// la carte en position 1 est sur le dessus.
func CreateDeck(ctx context.Context, tx *sql.Tx, p NewDeckParams) (DeckInfo, error) {
	deckCount := p.DeckCount
	var codes []string
	if len(p.Cards) > 0 {
		codes = slices.Clone(p.Cards)
		deckCount = 1
	} else {
		codes = utils.DeckCodes(p.DeckCount, p.Jokers)
	}
	if p.Shuffle {
		rand.Shuffle(len(codes), func(i, j int) { codes[i], codes[j] = codes[j], codes[i] })
	}

	id, err := newDeckID(ctx, tx)
	if err != nil {
		return DeckInfo{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO decks (id, deck_count, shuffled) VALUES (?, ?, ?)`,
		id, deckCount, p.Shuffle)
	if err != nil {
		return DeckInfo{}, err
	}

	// Requête préparée une seule fois, puis exécutée pour chaque carte.
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO cards (deck_id, code, state, position) VALUES (?, ?, 'deck', ?)`)
	if err != nil {
		return DeckInfo{}, err
	}
	defer stmt.Close()
	for i, code := range codes {
		if _, err := stmt.ExecContext(ctx, id, code, i+1); err != nil {
			return DeckInfo{}, err
		}
	}
	return DeckInfo{ID: id, DeckCount: deckCount, Shuffled: p.Shuffle, Remaining: len(codes)}, nil
}

// newDeckID tire un identifiant au hasard jusqu'à en trouver un qui n'existe
// pas. Comme il n'y a qu'un écrivain, personne ne peut le prendre entre-temps.
func newDeckID(ctx context.Context, tx *sql.Tx) (string, error) {
	for {
		id := utils.NewDeckID()
		var n int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM decks WHERE id = ?`, id).Scan(&n)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return id, nil
		}
	}
}

// DrawCards pige count cartes sur le dessus du deck. Pas de hasard ici :
// l'ordre a été fixé à la création ou au dernier mélange.
func DrawCards(ctx context.Context, tx *sql.Tx, deckID string, count int) (DrawResult, error) {
	if _, err := deckCount(ctx, tx, deckID); err != nil {
		return DrawResult{}, err
	}
	var restantes int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cards WHERE deck_id = ? AND state = 'deck'`, deckID).Scan(&restantes)
	if err != nil {
		return DrawResult{}, err
	}
	if restantes == 0 {
		return DrawResult{}, models.ErrDeckEmpty
	}
	if restantes < count {
		return DrawResult{}, models.ErrNotEnoughCards
	}

	cartes, err := selectCards(ctx, tx,
		`SELECT id, code FROM cards WHERE deck_id = ? AND state = 'deck' ORDER BY position LIMIT ?`,
		deckID, count)
	if err != nil {
		return DrawResult{}, err
	}
	if err := markDrawn(ctx, tx, cartes); err != nil {
		return DrawResult{}, err
	}
	return DrawResult{Codes: codesOf(cartes), Remaining: restantes - count}, nil
}

// CreateAndDraw sert /api/deck/new/draw/ : création d'un deck mélangé et
// pige dans le même job d'écriture.
func CreateAndDraw(ctx context.Context, tx *sql.Tx, p NewDeckParams, count int) (DeckInfo, DrawResult, error) {
	p.Shuffle = true
	deck, err := CreateDeck(ctx, tx, p)
	if err != nil {
		return DeckInfo{}, DrawResult{}, err
	}
	pige, err := DrawCards(ctx, tx, deck.ID, count)
	return deck, pige, err
}

// ShuffleDeck mélange le deck. Sans onlyRemaining, toutes les cartes (pigées
// ou dans des piles) reviennent d'abord dans le deck. Avec onlyRemaining, seules
// les cartes encore dans le deck sont mélangées. Renvoie le nombre de cartes
// dans le deck.
func ShuffleDeck(ctx context.Context, tx *sql.Tx, deckID string, onlyRemaining bool) (int, error) {
	if _, err := deckCount(ctx, tx, deckID); err != nil {
		return 0, err
	}
	query := `SELECT id, code FROM cards WHERE deck_id = ?`
	if onlyRemaining {
		query += ` AND state = 'deck'`
	}
	cartes, err := selectCards(ctx, tx, query, deckID)
	if err != nil {
		return 0, err
	}

	rand.Shuffle(len(cartes), func(i, j int) { cartes[i], cartes[j] = cartes[j], cartes[i] })
	if err := putInDeck(ctx, tx, cartes, 1); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE decks SET shuffled = 1 WHERE id = ?`, deckID); err != nil {
		return 0, err
	}
	return len(cartes), nil
}

// carte est une carte physique en base. Un deck multiple contient plusieurs
// cartes avec le même code, d'où l'id.
type carte struct {
	id   int64
	code string
}

// deckCount renvoie le nombre de paquets du deck, ou ErrDeckNotFound.
func deckCount(ctx context.Context, tx *sql.Tx, deckID string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT deck_count FROM decks WHERE id = ?`, deckID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, models.ErrDeckNotFound
	}
	return n, err
}

// selectCards exécute une requête qui renvoie des colonnes (id, code).
func selectCards(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]carte, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cartes []carte
	for rows.Next() {
		var c carte
		if err := rows.Scan(&c.id, &c.code); err != nil {
			return nil, err
		}
		cartes = append(cartes, c)
	}
	return cartes, rows.Err()
}

func codesOf(cartes []carte) []string {
	codes := make([]string, len(cartes))
	for i, c := range cartes {
		codes[i] = c.code
	}
	return codes
}

// markDrawn passe des cartes à l'état « pigée » (en main : ni dans le deck,
// ni dans une pile).
func markDrawn(ctx context.Context, tx *sql.Tx, cartes []carte) error {
	stmt, err := tx.PrepareContext(ctx, `UPDATE cards
		SET state = 'drawn', position = NULL, pile_name = NULL, pile_position = NULL WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, c := range cartes {
		if _, err := stmt.ExecContext(ctx, c.id); err != nil {
			return err
		}
	}
	return nil
}

// putInDeck met des cartes dans le deck, aux positions premiere, premiere+1, etc.
func putInDeck(ctx context.Context, tx *sql.Tx, cartes []carte, premiere int) error {
	stmt, err := tx.PrepareContext(ctx, `UPDATE cards
		SET state = 'deck', position = ?, pile_name = NULL, pile_position = NULL WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, c := range cartes {
		if _, err := stmt.ExecContext(ctx, premiere+i, c.id); err != nil {
			return err
		}
	}
	return nil
}

// putInPile met des cartes dans une pile, aux positions premiere, premiere+1, etc.
func putInPile(ctx context.Context, tx *sql.Tx, cartes []carte, pile string, premiere int) error {
	stmt, err := tx.PrepareContext(ctx, `UPDATE cards
		SET state = 'pile', position = NULL, pile_name = ?, pile_position = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, c := range cartes {
		if _, err := stmt.ExecContext(ctx, pile, premiere+i, c.id); err != nil {
			return err
		}
	}
	return nil
}
