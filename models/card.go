// Package models contient les structures de données de l'API.
package models

// Card est une carte telle que renvoyée en JSON.
type Card struct {
	Code  string `json:"code"`
	Image string `json:"image"`
	Value string `json:"value"`
	Suit  string `json:"suit"`
}

// Valeurs et couleurs : code -> texte en français.
var (
	Valeurs = map[string]string{
		"A": "AS", "2": "2", "3": "3", "4": "4", "5": "5", "6": "6", "7": "7",
		"8": "8", "9": "9", "10": "10", "J": "VALET", "Q": "REINE", "K": "ROI",
	}
	Couleurs = map[string]string{
		"S": "PIQUE", "H": "COEUR", "D": "CARREAU", "C": "TREFLE",
	}
)

// Ordre d'un paquet neuf : Pique, Carreau, Trèfle, Coeur, de l'As au Roi.
var (
	OrdreCouleurs = []string{"S", "D", "C", "H"}
	OrdreValeurs  = []string{"A", "2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K"}
)

// Les deux jokers.
const (
	Joker1 = "X1"
	Joker2 = "X2"
	Joker  = "JOKER" // valeur et couleur d'un joker
)
