# TP1 – API de cartes à jouer en Go

Implémentation en Go de l'API [Deck of Cards](https://deckofcardsapi.com), avec les valeurs des cartes en français (AS, VALET, REINE, ROI, PIQUE, COEUR, CARREAU, TREFLE).

Les données sont dans une base SQLite. Tous les accès à la base passent par un worker pool : un seul worker pour les écritures, 1 à 3 workers pour les lectures. Les choix de conception sont expliqués dans [ARCHITECTURE.md](ARCHITECTURE.md).

## Prérequis

- Go 1.22 ou plus
- gcc, parce que le pilote `github.com/mattn/go-sqlite3` utilise CGO. Sous Windows : installer MSYS2 ou WinLibs, puis `go env -w CGO_ENABLED=1`.

## Lancer le serveur

À partir du dossier du projet (les images sont cherchées dans `static/` à partir du dossier courant) :

```bash
go run .
```

Le serveur écoute sur http://localhost:8080 et la base `cards.db` est créée au premier lancement. `Ctrl+C` arrête le serveur proprement.

Options :

- `-addr` : adresse d'écoute (`:8080`)
- `-base-url` : début des URL d'images dans les réponses (`http://localhost:8080`)
- `-db` : fichier SQLite (`cards.db`)
- `-readers` : nombre de workers lecteurs, de 1 à 3 (`2`)
- `-timeout` : délai maximal d'une requête (`2s`)

## Exemples

Toutes les routes sont en GET. Remplacer `ID` par le `deck_id` reçu à la création. Sous PowerShell, écrire `curl.exe` au lieu de `curl`.

```bash
# Créer un deck : normal, 6 paquets, avec jokers, mélangé, partiel
curl "http://localhost:8080/api/deck/new/"
curl "http://localhost:8080/api/deck/new/?deck_count=6"
curl "http://localhost:8080/api/deck/new/?jokers_enabled=true"
curl "http://localhost:8080/api/deck/new/shuffle/"
curl "http://localhost:8080/api/deck/new/shuffle/?cards=AS,2S,KS,AD,2D,KD,AC,2C,KC,AH,2H,KH"

# Mélanger un deck existant (remaining=true : seulement les cartes encore dans le deck)
curl "http://localhost:8080/api/deck/ID/shuffle/"
curl "http://localhost:8080/api/deck/ID/shuffle/?remaining=true"

# Piger (avec new : crée un deck mélangé et pige dans la même requête)
curl "http://localhost:8080/api/deck/ID/draw/?count=2"
curl "http://localhost:8080/api/deck/new/draw/?count=2"

# Piles
curl "http://localhost:8080/api/deck/ID/pile/discard/add/?cards=AS,2S"
curl "http://localhost:8080/api/deck/ID/pile/discard/list/"
curl "http://localhost:8080/api/deck/ID/pile/discard/shuffle/"
curl "http://localhost:8080/api/deck/ID/pile/discard/draw/?count=2"
curl "http://localhost:8080/api/deck/ID/pile/discard/draw/?cards=AS"
curl "http://localhost:8080/api/deck/ID/pile/discard/draw/bottom/"
curl "http://localhost:8080/api/deck/ID/pile/discard/draw/random/"

# Retourner des cartes au deck
curl "http://localhost:8080/api/deck/ID/return/"
curl "http://localhost:8080/api/deck/ID/return/?cards=AS,2S"
curl "http://localhost:8080/api/deck/ID/pile/discard/return/"
curl "http://localhost:8080/api/deck/ID/pile/discard/return/?cards=AS"

# Images
curl "http://localhost:8080/static/img/AS.svg"
curl "http://localhost:8080/static/img/back.svg"
```

Exemple de réponse pour une pige :

```json
{"success":true,"deck_id":"kxozasf3edqu","remaining":50,"cards":[{"code":"AS","image":"http://localhost:8080/static/img/AS.svg","value":"AS","suit":"PIQUE"},{"code":"2S","image":"http://localhost:8080/static/img/2S.svg","value":"2","suit":"PIQUE"}]}
```

## Erreurs

Format : `{"success": false, "deck_id": "...", "error": "message"}`

| Code | Messages |
|---|---|
| 400 | `Not enough cards remaining`, `Invalid card code`, `Duplicate cards in request`, `Pile operations not supported with multiple decks`, `Invalid parameter`, `Parameter out of range` |
| 404 | `Deck not found`, `Deck is empty`, `Card not found in pile`, `Card not found in deck`, `Pile not found`, `Pile is empty` |
| 500 | `Database error`, `Internal server error` (panique rattrapée) |
| 503 | `Request timeout` |

## Choix faits

- Jokers : codes `X1` et `X2`, valeur et couleur `JOKER`, à la fin du paquet.
- Deck partiel (`cards=`) : il remplace `deck_count` et `jokers_enabled`. `deck_count` va de 1 à 100.
- Les cartes retournées au deck sont placées en dessous : elles seront pigées en dernier.
- Pile : le haut est la dernière carte ajoutée (`draw/` = `draw/top/`). Avec `cards=`, `count` est ignoré.
- Ajouter à une pile une carte qui n'a pas été pigée donne `Invalid parameter` ; une carte qui n'existe pas dans le deck (deck partiel) donne `Card not found in deck`.
- Une pile vidée existe encore (`"remaining": 0`). `Pile not found` : la pile n'a jamais été créée. `Pile is empty` : elle existe mais elle est vide.

## Tests

```bash
go test ./...
go test -race ./...
go test -race -v -run TestLoad ./handlers/          # tests de charge, affiche les requêtes/s
go test -bench . -benchmem -run "^$" ./database/    # benchmarks
```

Résultats sur mon PC (Intel i7-13700HX, Windows) :

- `go test -race ./...` : tous les paquetages sont `ok`, aucune alerte `DATA RACE`.
- 52 piges simultanées sur le même deck : 25.9841ms : 2001 requêtes/s (avec `-race`, qui ralentit l'exécution).
- 140 requêtes simultanées sur le même deck (120 piges et 20 mélanges) : 52.7916ms : 2652 requêtes/s (avec `-race`).
- `BenchmarkDrawCard` (piger une carte) : 153488 ns/op
- `BenchmarkCreateDeck` (créer un deck de 52 cartes) : 542525 ns/op

Le script `concurrence.sh` sert à la démonstration : avec le serveur lancé (`go run -race .`), `bash concurrence.sh` envoie 60 piges en même temps sur un deck neuf et affiche le nombre de cartes reçues, différentes et en double.

## Structure

```
main.go       démarrage, options, arrêt propre
database/     SQLite, worker pool, opérations sur les decks et les piles
handlers/     handlers HTTP, middlewares, cache des decks, historique
models/       cartes, réponses JSON, erreurs
utils/        codes de cartes, paramètres, identifiants
static/img/   images SVG (52 cartes, 2 jokers, dos)
```

Les images des cartes et du dos ont été converties en SVG avec VTracer. Les deux jokers sont de simples SVG (un rectangle et le texte JOKER).
