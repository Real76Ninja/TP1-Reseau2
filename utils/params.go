package utils

import (
	"strconv"

	"TP1/models"
)

// ParseIntParam lit un entier optionnel : vide = def, pas un entier = Invalid
// parameter, hors de [minimum, maximum] = Parameter out of range.
func ParseIntParam(raw string, def, minimum, maximum int) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, models.ErrInvalidParameter
	}
	if n < minimum || n > maximum {
		return 0, models.ErrParameterOutOfRange
	}
	return n, nil
}

// ParseBoolParam lit un booléen optionnel (true, false, 1, 0...).
func ParseBoolParam(raw string, def bool) (bool, error) {
	if raw == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, models.ErrInvalidParameter
	}
	return b, nil
}
