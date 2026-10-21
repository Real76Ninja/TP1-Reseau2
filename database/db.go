// Package database gère SQLite : ouverture de la base, worker pool et
// opérations sur les decks, les piles et l'historique.
package database

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// DB regroupe deux handles sur le même fichier : Write n'a qu'une seule
// connexion (un seul écrivain), Read en a une par worker lecteur.
type DB struct {
	Write *sql.DB
	Read  *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS decks (
    id         TEXT PRIMARY KEY,
    deck_count INTEGER NOT NULL,
    shuffled   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS cards (
    id            INTEGER PRIMARY KEY,
    deck_id       TEXT    NOT NULL REFERENCES decks(id),
    code          TEXT    NOT NULL,
    state         TEXT    NOT NULL CHECK (state IN ('deck', 'drawn', 'pile')),
    position      INTEGER,
    pile_name     TEXT,
    pile_position INTEGER
);

CREATE TABLE IF NOT EXISTS piles (
    deck_id TEXT NOT NULL REFERENCES decks(id),
    name    TEXT NOT NULL,
    PRIMARY KEY (deck_id, name)
);

CREATE TABLE IF NOT EXISTS historique (
    id         INTEGER PRIMARY KEY,
    horodatage TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    request_id TEXT NOT NULL,
    operation  TEXT NOT NULL,
    deck_id    TEXT
);

CREATE INDEX IF NOT EXISTS idx_cards_deck_state_pos ON cards (deck_id, state, position);
CREATE INDEX IF NOT EXISTS idx_cards_pile ON cards (deck_id, pile_name, pile_position);
CREATE INDEX IF NOT EXISTS idx_cards_deck_code ON cards (deck_id, code);
`

// OpenDB ouvre (ou crée) la base, active le mode WAL et crée les tables.
func OpenDB(path string, lecteurs int) (*DB, error) {
	// synchronous=NORMAL : en mode WAL, un commit n'attend plus l'écriture
	// sur le disque, ce qui rend les transactions d'écriture plus courtes.
	dsn := path + "?_foreign_keys=on&_synchronous=NORMAL"

	write, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	write.SetMaxOpenConns(1)

	// Le mode WAL est enregistré dans le fichier : une fois au démarrage suffit.
	if _, err := write.Exec("PRAGMA journal_mode=WAL"); err != nil {
		write.Close()
		return nil, err
	}
	if _, err := write.Exec(schema); err != nil {
		write.Close()
		return nil, err
	}

	read, err := sql.Open("sqlite3", dsn)
	if err != nil {
		write.Close()
		return nil, err
	}
	read.SetMaxOpenConns(lecteurs)

	return &DB{Write: write, Read: read}, nil
}

// Close ferme les deux handles.
func (d *DB) Close() {
	d.Read.Close()
	d.Write.Close()
}

// InsertHistory ajoute une ligne à l'historique. Comme toute écriture, elle
// passe par un job d'écriture du pool.
func InsertHistory(ctx context.Context, tx *sql.Tx, requestID, operation, deckID string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO historique (request_id, operation, deck_id) VALUES (?, ?, ?)`,
		requestID, operation, deckID)
	return err
}
