package models

import "errors"

// Erreurs de l'API. Le message est celui renvoyé au client.
var (
	ErrDeckNotFound        = errors.New("Deck not found")
	ErrNotEnoughCards      = errors.New("Not enough cards remaining")
	ErrDeckEmpty           = errors.New("Deck is empty")
	ErrInvalidCardCode     = errors.New("Invalid card code")
	ErrCardNotInPile       = errors.New("Card not found in pile")
	ErrDuplicateCards      = errors.New("Duplicate cards in request")
	ErrCardNotInDeck       = errors.New("Card not found in deck")
	ErrPileNotFound        = errors.New("Pile not found")
	ErrPileEmpty           = errors.New("Pile is empty")
	ErrPileMultipleDecks   = errors.New("Pile operations not supported with multiple decks")
	ErrDatabase            = errors.New("Database error")
	ErrInternal            = errors.New("Internal server error")
	ErrTimeout             = errors.New("Request timeout")
	ErrInvalidParameter    = errors.New("Invalid parameter")
	ErrParameterOutOfRange = errors.New("Parameter out of range")
)
