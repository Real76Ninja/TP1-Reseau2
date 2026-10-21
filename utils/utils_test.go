package utils

import (
	"errors"
	"slices"
	"testing"

	"TP1/models"
)

func TestDeckCodes(t *testing.T) {
	codes := DeckCodes(1, false)
	// Pique, Carreau, Trèfle, Coeur ; de l'As au Roi.
	if len(codes) != 52 || codes[0] != "AS" || codes[13] != "AD" || codes[26] != "AC" || codes[51] != "KH" {
		t.Fatalf("ordre inattendu : %v", codes)
	}
	if n := len(DeckCodes(6, true)); n != 6*54 {
		t.Fatalf("6 paquets avec jokers : %d cartes", n)
	}
}

func TestParseCards(t *testing.T) {
	got, err := ParseCards("AS,2s, kd ,10h")
	if err != nil || !slices.Equal(got, []string{"AS", "2S", "KD", "10H"}) {
		t.Fatalf("%v, %v", got, err)
	}
	for entree, attendue := range map[string]error{
		"":       models.ErrInvalidParameter,
		"AS,99Z": models.ErrInvalidCardCode,
		"AS,,2S": models.ErrInvalidCardCode,
		"AS,as":  models.ErrDuplicateCards,
	} {
		if _, err := ParseCards(entree); !errors.Is(err, attendue) {
			t.Errorf("ParseCards(%q) : %v, attendu %v", entree, err, attendue)
		}
	}
}

func TestCardFromCode(t *testing.T) {
	for code, attendue := range map[string]models.Card{
		"10H": {Code: "10H", Image: "http://h/static/img/10H.svg", Value: "10", Suit: "COEUR"},
		"KD":  {Code: "KD", Image: "http://h/static/img/KD.svg", Value: "ROI", Suit: "CARREAU"},
		"X1":  {Code: "X1", Image: "http://h/static/img/X1.svg", Value: "JOKER", Suit: "JOKER"},
	} {
		if got := CardFromCode(code, "http://h"); got != attendue {
			t.Errorf("CardFromCode(%q) = %+v", code, got)
		}
	}
}

func TestParseIntParam(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want int
		err  error
	}{
		{"", 1, nil},
		{"6", 6, nil},
		{"0", 0, models.ErrParameterOutOfRange},
		{"101", 0, models.ErrParameterOutOfRange},
		{"abc", 0, models.ErrInvalidParameter},
	} {
		if got, err := ParseIntParam(c.raw, 1, 1, 100); got != c.want || !errors.Is(err, c.err) {
			t.Errorf("ParseIntParam(%q) = %d, %v", c.raw, got, err)
		}
	}
}
