package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"TP1/models"
)

// Recover rattrape une panique dans un handler et répond proprement
// "Internal server error" (500). Il ne protège pas les goroutines lancées par
// le handler : l'historique a donc son propre recover.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				log.Printf("panique rattrapée (%s) : %v", r.URL.Path, p)
				writeJSON(w, http.StatusInternalServerError, models.ErrorResponse("", models.ErrInternal))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Timeout ajoute un délai au contexte de la requête. C'est ce contexte que les
// handlers donnent au worker pool.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Clé du contexte : un type à nous plutôt qu'une string, pour ne pas entrer en
// collision avec une clé d'un autre paquetage.
type cleContexte int

const cleRequestID cleContexte = 0

// Le compteur est partagé par toutes les requêtes. Avec un simple n++, deux
// requêtes simultanées pourraient lire la même valeur (race condition) et
// avoir le même identifiant : on utilise donc un compteur atomique.
// L'heure de démarrage évite de réutiliser les mêmes identifiants après un
// redémarrage (ils sont gardés dans l'historique).
var (
	compteur  atomic.Uint64
	demarrage = time.Now().Unix()
)

// RequestID donne un identifiant unique à la requête et le met dans le contexte.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := fmt.Sprintf("%d-%d", demarrage, compteur.Add(1))
		ctx := context.WithValue(r.Context(), cleRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom lit l'identifiant de requête du contexte.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(cleRequestID).(string)
	return id
}
