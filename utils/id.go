package utils

import "math/rand/v2"

const alphabetID = "abcdefghijklmnopqrstuvwxyz0123456789"

// NewDeckID renvoie un identifiant de 12 caractères (ex. 3p40paa87x90).
// L'unicité est vérifiée dans la base au moment de la création du deck.
func NewDeckID() string {
	b := make([]byte, 12)
	for i := range b {
		b[i] = alphabetID[rand.IntN(len(alphabetID))]
	}
	return string(b)
}
