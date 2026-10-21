# Architecture

## Paquetages

- `main.go` : options, démarrage du serveur, arrêt propre
- `database/` : ouverture de SQLite, worker pool, opérations (decks, pige, mélange, piles, retour, historique)
- `handlers/` : handlers HTTP, middlewares, cache des decks, historique
- `models/` : cartes, réponses JSON, messages d'erreur
- `utils/` : codes de cartes, lecture des paramètres, identifiants de deck

```
requête HTTP → Recover → Timeout → RequestID → handler (vérifie le cache des decks)
                                                  │
                     ┌─→ channel lectures  → 1 à 3 workers lecteurs ─┐
          handler ───┤                                               ├─→ SQLite (WAL)
                     └─→ channel écritures → 1 seul worker écrivain ─┘
                                   ↓
                     channel de réponse → handler → réponse au client
                                                  → goroutine d'historique (job d'écriture)
```

## Schéma de la base

```sql
CREATE TABLE decks (
    id         TEXT PRIMARY KEY,           -- ex. 3p40paa87x90
    deck_count INTEGER NOT NULL,
    shuffled   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE cards (
    id            INTEGER PRIMARY KEY,     -- une ligne par carte physique
    deck_id       TEXT NOT NULL REFERENCES decks(id),
    code          TEXT NOT NULL,           -- AS, 10H, X1...
    state         TEXT NOT NULL CHECK (state IN ('deck', 'drawn', 'pile')),
    position      INTEGER,                 -- rang dans le deck (1 = dessus)
    pile_name     TEXT,
    pile_position INTEGER                  -- rang dans la pile (1 = bas)
);
CREATE TABLE piles (
    deck_id TEXT NOT NULL REFERENCES decks(id),
    name    TEXT NOT NULL,
    PRIMARY KEY (deck_id, name)
);
CREATE TABLE historique (
    id         INTEGER PRIMARY KEY,
    horodatage TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    request_id TEXT NOT NULL,
    operation  TEXT NOT NULL,
    deck_id    TEXT
);
CREATE INDEX idx_cards_deck_state_pos ON cards (deck_id, state, position);
CREATE INDEX idx_cards_pile ON cards (deck_id, pile_name, pile_position);
CREATE INDEX idx_cards_deck_code ON cards (deck_id, code);
```

Justification :

- Une ligne par carte physique : un deck de 6 paquets contient six `AS` et le code seul ne suffit pas pour les distinguer, donc chaque carte a son `id`. Le même modèle sert pour un deck normal, multiple, avec jokers ou partiel.
- La colonne `state` dit où est la carte : dans le deck (`deck`, avec sa `position`), pigée et en main (`drawn`), ou dans une pile (`pile`, avec `pile_name` et `pile_position`). Une carte ne peut donc être qu'à un seul endroit à la fois. Piger, c'est passer de `deck` à `drawn` ; ajouter à une pile, de `drawn` à `pile` ; retourner, revenir à `deck` avec une position à la fin du paquet.
- L'ordre du deck est dans `position`. Il est fixé à la création et à chaque mélange. Piger prend les plus petites positions (`ORDER BY position LIMIT n`) : aucun hasard à la pige.
- La table `piles` garde le nom d'une pile même quand elle est vide. Sans elle, une pile vidée disparaîtrait : on ne pourrait pas répondre `"discard": {"remaining": 0}` après un retour, ni faire la différence entre `Pile not found` et `Pile is empty`.
- `decks.deck_count` sert au cache (piles interdites si plus d'un paquet) et `decks.shuffled` est renvoyé par `/return/`.
- Index :
  - `(deck_id, state, position)` : piger sur le dessus et compter les cartes restantes, sans parcourir la table ni trier.
  - `(deck_id, pile_name, pile_position)` : lister une pile dans l'ordre, piger en haut ou en bas.
  - `(deck_id, code)` : retrouver une carte par son code (`cards=AS,2S`).
- Réglages SQLite : `PRAGMA journal_mode=WAL` exécuté une fois au démarrage (il reste enregistré dans le fichier), `synchronous=NORMAL` (avec WAL, un commit n'attend plus que les données soient écrites sur le disque ; un crash du système peut perdre les dernières transactions, mais la base n'est jamais corrompue) et `foreign_keys=on`.

## Worker pool

- Deux channels de jobs (`lectures` et `ecritures`, 100 places chacun). Le channel des lectures est servi par 1 à 3 workers (option `-readers`), celui des écritures par un seul worker.
- Deux `*sql.DB` sur le même fichier : celui des écritures a `SetMaxOpenConns(1)`, celui des lectures `SetMaxOpenConns(nombre de lecteurs)`.
- Un job est une fonction. Pour une écriture, c'est le worker qui ouvre la transaction (`BeginTx`), exécute le job, puis fait `Commit` (ou `Rollback` si le job renvoie une erreur).
- Chaque requête crée son propre channel de réponse, de capacité 1. Le worker peut donc toujours y déposer la réponse, même si le handler est déjà parti après un timeout : il ne reste jamais bloqué.
- Avant d'exécuter un job, le worker vérifie `ctx.Err()`. Si le client est parti ou si le délai est dépassé, le job n'est pas exécuté.
- `Close()` ferme les deux channels (protégé par `sync.Once`), les workers terminent les jobs encore dans les channels puis sortent de leur boucle `for range`, et `Close()` attend leur fin avec un `sync.WaitGroup`.

### Un seul job par opération d'écriture

Chaque endpoint qui modifie des données est un seul job d'écriture : il lit et écrit dans la même transaction. Si on séparait « lire la carte du dessus » et « marquer la carte comme pigée » en deux jobs, deux requêtes simultanées pourraient lire toutes les deux `AS` avant que l'une des deux l'ait marquée, et piger la même carte. Avec un seul job et un seul écrivain, la deuxième requête voit forcément le résultat de la première et reçoit la carte suivante. Pour la même raison, `/api/deck/new/draw/` crée le deck et pige dans le même job, et chaque opération vérifie toutes ses cartes avant d'en modifier une (tout ou rien).

### Pourquoi les lectures peuvent être parallèles, mais pas les écritures

SQLite accepte plusieurs lecteurs en même temps, mais un seul écrivain pour toute la base. En mode WAL, les modifications sont ajoutées au fichier `-wal` et les lecteurs continuent de lire la dernière version validée : une lecture et une écriture peuvent donc se faire en même temps. Deux écritures, par contre, se disputent le même verrou : la deuxième attend, puis reçoit `database is locked`. C'est pour ça qu'il n'y a qu'un worker écrivain avec une seule connexion : les écritures attendent leur tour dans le channel au lieu de se bloquer dans SQLite.

### Ce qui a été fait pour traiter le plus de requêtes possible

Presque tous les endpoints modifient des données (piger, ajouter à une pile, retourner...), donc ils passent tous par le même écrivain. Le plus important est que chaque transaction d'écriture soit courte :

- un job ne fait que des requêtes SQL : le mélange est fait en Go avec `rand.Shuffle`, et il n'y a aucune attente dans une transaction ;
- les boucles d'insertion et de mise à jour utilisent une requête préparée (`PrepareContext`) dans la transaction ;
- les index évitent de parcourir et de trier la table `cards` ;
- `synchronous=NORMAL` rend chaque commit plus rapide.

En plus :

- le cache des decks refuse un deck inconnu ou une pile sur un deck multiple sans passer par la base ;
- les lectures (`list`) se font en parallèle sur 1 à 3 workers et n'attendent pas l'écrivain ;
- l'historique est écrit après la réponse, le client ne l'attend pas ;
- aucun verrou global : le seul mutex est celui du cache, gardé juste le temps de lire la map. Deux decks différents ne s'attendent que dans la file de l'écrivain.

## Concurrence et race conditions

| Donnée partagée | Problème sans protection | Protection |
|---|---|---|
| map du cache des decks | lecture et ajout en même temps (`concurrent map read and map write`) | `sync.RWMutex` |
| compteur des identifiants de requête | deux requêtes lisent la même valeur et ont le même identifiant | `atomic.Uint64` |
| base SQLite | deux écritures en même temps | un seul worker écrivain |
| channels du pool | `send on closed channel` si on soumet un job après `Close()` | ordre d'arrêt : l'historique est attendu avant `Close()` |
| goroutines d'historique | le pool est fermé pendant qu'elles écrivent | `sync.WaitGroup` dédié |
| réponse d'un job | worker bloqué si le handler est parti | channel de réponse de capacité 1 |

Ces endroits sont aussi commentés dans le code. Les tests et le serveur passent avec le détecteur : `go test -race ./...` et `go run -race .`.

### Pourquoi un `sync.RWMutex` pour le cache

Le cache (`deck_id` → nombre de paquets) est lu par presque toutes les requêtes et modifié seulement quand un deck est créé. Avec un `sync.Mutex`, deux lectures devraient s'attendre l'une l'autre pour rien. Avec un `RWMutex`, plusieurs requêtes peuvent lire en même temps (`RLock`) et seul l'ajout d'un deck est exclusif (`Lock`). Le verrou n'est jamais gardé pendant un accès à la base. Le cache est chargé depuis la table `decks` au démarrage, et un deck y est ajouté seulement après le commit de sa création.

## Cheminement du contexte

1. Middleware `Timeout` : `context.WithTimeout(r.Context(), 2s)` (option `-timeout`).
2. Middleware `RequestID` : `context.WithValue` ajoute l'identifiant de requête (clé d'un type défini, pas une `string`).
3. Le handler donne `r.Context()` au pool : `pool.SubmitWrite(ctx, job)`.
4. Le pool met le job dans le channel, puis attend avec un `select` entre le channel de réponse et `ctx.Done()`. Si le délai expire avant, le handler répond `Request timeout` (503).
5. Le worker vérifie `ctx.Err()` avant de commencer : un job expiré n'est pas exécuté.
6. Le job utilise `BeginTx(ctx)`, `QueryContext` et `ExecContext` : si le délai expire pendant le job, la requête SQL est interrompue et la transaction annulée.

Les middlewares sont dans l'ordre demandé : récupération de panique (répond `Internal server error` en 500 au lieu de couper la connexion), délai, identifiant de requête.

## Historique

Chaque opération réussie qui modifie des données (création, pige, mélange, piles, retour) ajoute une ligne à la table `historique`. Le handler écrit d'abord sa réponse, puis lance une goroutine qui soumet un job d'écriture au pool :

- `context.WithoutCancel(r.Context())` : le contexte de la requête est annulé à la fin du handler, mais on garde ses valeurs (l'identifiant de requête) ;
- un `context.WithTimeout` de 5 secondes pour ne jamais rester bloqué ;
- un `recover` dans la goroutine, car le middleware ne la protège pas ;
- un `sync.WaitGroup` dédié (`Add` avant le `go`, `Done` en `defer`).

## Arrêt propre

Avec `Ctrl+C`, `main` fait dans l'ordre :

1. `srv.Shutdown` : plus de nouvelles requêtes, celles en cours se terminent ;
2. `hist.Attendre()` : les goroutines d'historique finissent de soumettre leurs jobs ;
3. `pool.Close()` : fermeture des channels et attente des workers ;
4. `db.Close()`.

Si on fermait le pool avant l'étape 2, une goroutine d'historique pourrait encore envoyer un job sur un channel fermé, ce qui fait paniquer le programme.
