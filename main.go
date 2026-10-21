package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"TP1/database"
	"TP1/handlers"
)

func main() {
	addr := flag.String("addr", ":8080", "adresse d'écoute")
	baseURL := flag.String("base-url", "http://localhost:8080", "début des URL d'images dans les réponses")
	dbPath := flag.String("db", "cards.db", "fichier SQLite")
	readers := flag.Int("readers", 2, "nombre de workers lecteurs (1 à 3)")
	timeout := flag.Duration("timeout", 2*time.Second, "délai maximal d'une requête")
	flag.Parse()

	db, err := database.OpenDB(*dbPath, *readers)
	if err != nil {
		log.Fatal(err)
	}
	cache, err := handlers.LoadDeckCache(context.Background(), db.Read)
	if err != nil {
		log.Fatal(err)
	}
	pool, err := database.NewWorkerPool(db.Read, db.Write, *readers)
	if err != nil {
		log.Fatal(err)
	}
	hist := handlers.NewHistorique(pool)

	mux := http.NewServeMux()
	handlers.New(pool, cache, hist, *baseURL).Register(mux)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Middlewares, dans l'ordre : panique, délai, identifiant de requête.
	srv := &http.Server{
		Addr:    *addr,
		Handler: handlers.Recover(handlers.Timeout(*timeout)(handlers.RequestID(mux))),
	}
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	log.Printf("Serveur démarré sur %s", *addr)

	// Ctrl+C : arrêt propre, dans cet ordre.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("Arrêt du serveur...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// 1. Plus de nouvelles requêtes, on attend celles en cours.
	if err := srv.Shutdown(ctx); err != nil {
		log.Println(err)
	}
	// 2. Plus aucun handler ne tourne : on attend les goroutines d'historique.
	hist.Attendre()
	// 3. Plus personne n'envoie de job : on peut fermer le pool.
	pool.Close()
	// 4. Puis la base.
	db.Close()
	log.Println("Serveur arrêté")
}
