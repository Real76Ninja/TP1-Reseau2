#!/bin/bash
# Démonstration de concurrence : 60 piges simultanées sur un deck neuf de
# 52 cartes, puis vérification des cartes reçues.
# Le serveur doit déjà tourner (go run -race .).
# Utilisation : bash concurrence.sh [nombre de requêtes]

URL="http://localhost:8080"
N="${1:-60}"
DOSSIER=$(mktemp -d)

DECK=$(curl -s "$URL/api/deck/new/" | grep -o '"deck_id":"[a-z0-9]*"' | cut -d'"' -f4)
echo "Deck créé : $DECK"

# Toutes les requêtes partent en même temps (&), chacune écrit sa réponse
# dans son propre fichier.
for i in $(seq 1 "$N"); do
  curl -s "$URL/api/deck/$DECK/draw/" -o "$DOSSIER/$i.json" &
done
wait

cat "$DOSSIER"/*.json | grep -o '"code":"[0-9A-Z]*"' | cut -d'"' -f4 > "$DOSSIER/cartes.txt"
echo "Requêtes envoyées       : $N"
echo "Cartes reçues           : $(wc -l < "$DOSSIER/cartes.txt")"
echo "Cartes différentes      : $(sort -u "$DOSSIER/cartes.txt" | wc -l)"
echo "Cartes en double        : $(sort "$DOSSIER/cartes.txt" | uniq -d | wc -l)"
echo "Réponses Deck is empty  : $(grep -l "Deck is empty" "$DOSSIER"/*.json | wc -l)"
rm -rf "$DOSSIER"
