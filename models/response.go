package models

// PileInfo décrit une pile dans la section "piles" d'une réponse.
// Cards n'est rempli que pour la pile demandée dans /list/.
type PileInfo struct {
	Cards     []Card `json:"cards,omitempty"`
	Remaining int    `json:"remaining"`
}

// Response sert pour toutes les réponses JSON.
// Shuffled et Remaining sont des pointeurs : avec omitempty, false et 0
// disparaîtraient du JSON, alors qu'il faut les afficher.
type Response struct {
	Success   bool                `json:"success"`
	DeckID    string              `json:"deck_id"`
	Shuffled  *bool               `json:"shuffled,omitempty"`
	Remaining *int                `json:"remaining,omitempty"`
	Cards     []Card              `json:"cards,omitempty"`
	Piles     map[string]PileInfo `json:"piles,omitempty"`
	Error     string              `json:"error,omitempty"`
}

func Bool(b bool) *bool { return &b }
func Int(n int) *int    { return &n }

// ErrorResponse construit une réponse d'erreur.
func ErrorResponse(deckID string, err error) Response {
	return Response{Success: false, DeckID: deckID, Error: err.Error()}
}
