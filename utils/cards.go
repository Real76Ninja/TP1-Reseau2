// Package utils contient les fonctions utilitaires : codes de cartes,
// validation des paramètres et identifiants de deck.
package utils

import (
	"strings"

	"TP1/models"
)

// DeckCodes renvoie les codes de n paquets neufs mis bout à bout. Un paquet est
// dans l'ordre Pique, Carreau, Trèfle, Coeur, de l'As au Roi, puis les jokers.
func DeckCodes(n int, jokers bool) []string {
	var codes []string
	for i := 0; i < n; i++ {
		for _, couleur := range models.OrdreCouleurs {
			for _, valeur := range models.OrdreValeurs {
				codes = append(codes, valeur+couleur)
			}
		}
		if jokers {
			codes = append(codes, models.Joker1, models.Joker2)
		}
	}
	return codes
}

// IsValidCode indique si le code (en majuscules) est une carte valide.
func IsValidCode(code string) bool {
	if code == models.Joker1 || code == models.Joker2 {
		return true
	}
	if len(code) < 2 || len(code) > 3 {
		return false
	}
	_, valeurOK := models.Valeurs[code[:len(code)-1]]
	_, couleurOK := models.Couleurs[code[len(code)-1:]]
	return valeurOK && couleurOK
}

// ParseCards lit une liste de codes comme "AS,2s,kd" (insensible à la casse).
func ParseCards(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, models.ErrInvalidParameter
	}
	vus := map[string]bool{}
	var codes []string
	for _, morceau := range strings.Split(raw, ",") {
		code := strings.ToUpper(strings.TrimSpace(morceau))
		if !IsValidCode(code) {
			return nil, models.ErrInvalidCardCode
		}
		if vus[code] {
			return nil, models.ErrDuplicateCards
		}
		vus[code] = true
		codes = append(codes, code)
	}
	return codes, nil
}

// CardFromCode construit la carte JSON à partir de son code.
func CardFromCode(code, baseURL string) models.Card {
	card := models.Card{Code: code, Image: baseURL + "/static/img/" + code + ".svg"}
	if code == models.Joker1 || code == models.Joker2 {
		card.Value, card.Suit = models.Joker, models.Joker
		return card
	}
	card.Value = models.Valeurs[code[:len(code)-1]]
	card.Suit = models.Couleurs[code[len(code)-1:]]
	return card
}
