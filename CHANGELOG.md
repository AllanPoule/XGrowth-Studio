# Changelog

Toutes les évolutions notables de XGrowth Studio. Une version correspond à une pull request fusionnée :
V0.05 est la cinquième. Le numéro affiché dans le studio (Réglages) et par `xgrowth version` est celui de la
dernière version.

## Non publié

### Sécurité
- **Isolation des CLI d'IA (Codex, Gemini, Claude Code)** : Codex reçoit un `CODEX_HOME` temporaire avec un
  `config.toml` sans shell, sans MCP (et les mêmes réglages en `-c`), `web_search` désactivé, sandbox lecture seule,
  règles utilisateur ignorées (Codex 0.171.0 garde cependant des outils internes sans effet réel : `exec` isolé,
  `request_user_input`, `sleep`, collaboration ; voir le README) ; Gemini reçoit un `GEMINI_CLI_HOME` temporaire avec une politique qui refuse tous les
  outils (aussi via `--admin-policy`) et aucun MCP. Les trois CLI tournent avec un environnement en liste blanche
  (plus aucune clé ni jeton hors connexion propre à la CLI) dans un répertoire de travail vide. Tests sur les
  arguments, l'environnement et la configuration, dont un contenu de veille malveillant. README aligné sur la
  protection réelle et ses limites (Gemini non exécuté pour valider la politique). Les identifiants copiés ne sont
  recopiés que si la source n'a pas changé pendant l'appel ; les variables Vertex AI de Gemini (hors chemin de clés) sont transmises.

### Ajouté
- **Centre de contrôle des publications** (onglet « Contrôle », pastille du nombre de points à traiter) : une page
  qui répond à « est-ce que tout tourne, et qu'est-ce qui demande mon intervention ? ».
  - **Pause par compte** : le planificateur ne publie rien pour un compte en pause (les posts restent en file, la
    publication reprend à la reprise). L'état est dans `data.json` : il survit au redémarrage, et enregistrer le
    formulaire du compte ne lève pas la pause. Routes : `POST /api/accounts/{nom}/pause`.
  - **Dernier passage de chaque boucle** (publication, puis veille/tri/statistiques), noté dans `scheduler.json`
    (dossier de données) avec l'intervalle du planificateur ; alerte si un passage a plus de 3 intervalles
    (5 minutes au moins) ou n'a jamais eu lieu.
  - **Publications en retard** : posts planifiés ou validés dont l'heure est passée de plus de 5 minutes, et posts
    LinkedIn « à publier à la main » jamais marqués publiés. Un retard dû à une pause voulue est listé sans alerte.
  - **Connexions à renouveler**, selon ce que chaque backend expose : LinkedIn (jeton expiré, bientôt expiré ou
    absent), xapi (variable du jeton vide). agentx n'expose aucune date d'expiration : le compte est signalé comme
    non suivi.
  - **Envois au résultat incertain** : un envoi parti dont la réponse n'est pas arrivée (coupure réseau, délai
    dépassé après l'envoi, réponse illisible, processus arrêté pendant l'envoi) passe en statut `uncertain` au lieu
    d'échouer ou de rester « en cours ». Il n'est jamais relancé automatiquement et compte dans les plafonds. Pour
    le relancer, il faut d'abord vérifier sur le compte : renseigner le lien du post s'il est en ligne (il devient
    publié), ou confirmer qu'il n'est pas parti (il repart à la prochaine passe). Route :
    `POST /api/posts/{id}/uncertain` (`url` ou `notSent`). Un refus net (HTTP 4xx/5xx, connexion impossible) reste
    un échec.
  - Aucune notification externe (e-mail, push) : la page est le tableau de bord. `GET /api/control` donne le même
    rapport ; `/api/state` l'inclut (`control`).
  - Tests Go : compte en pause non publié (et pause conservée après redémarrage), envoi incertain jamais relancé,
    résolution obligatoire par vérification, détection des retards, des passages anciens et des connexions.
- **Statistiques utiles pour décider** : la page Performances a un filtre par compte et par période (7, 30, 90 jours
  ou tout), des comparaisons par type de post, longueur, visuel et origine (veille, idée, campagne, à la main), et
  la date du dernier relevé de chaque mesure. Une donnée absente s'affiche « indisponible », jamais 0 (vues non
  renvoyées par le backend, post jamais relevé, LinkedIn) ; un vrai zéro mesuré reste 0. Les comparaisons sont
  descriptives et ne disent rien des causes. Le relevé rattrape les posts de plus de 14 jours qui n'ont aucune
  mesure tardive (au moins 48 h après la publication) : au plus 2 tentatives par post, comptées même en cas d'échec et espacées d'au moins 6 h.
  Suivi quotidien des abonnés (un relevé par jour et par compte) pour les backends agentx et xapi ; « indisponible »
  pour les autres et tant que le backend ne renvoie pas le nombre (au plus une tentative par heure et par compte, même en cas d'échec). Nouvelle route : `GET /api/stats?account=&days=`.
- **Visuels déposés depuis le navigateur** : on glisse ou on choisit une image (le sélecteur marche sur mobile), avec
  aperçu, texte alternatif (LinkedIn), remplacement et retrait, au lieu de saisir un chemin de fichier du serveur.
  Le serveur contrôle le contenu réel (en-tête de l'image et type MIME détecté, pas l'extension), la taille et les
  formats (JPG, PNG, GIF ; 5 Mo sur X, 10 Mo sur LinkedIn ; une image par post), n'accepte aucun chemin ni nom de
  fichier du client, et garde une copie stable dans `<données>/media` sous un nom aléatoire (permissions 0600) : la
  publication programmée l'utilise. Les copies qu'aucun post ne référence plus sont supprimées (au remplacement, au
  retrait, puis par le nettoyage horaire des fichiers vieux de plus d'une heure). L'interface affiche ce que permet
  le backend du compte (`mediaLimits` dans `/api/state`) ; le backend xapi n'envoie pas de visuel et le dit.
  Nouvelle route : `POST/GET /api/posts/{id}/media-file`. Les posts qui portent un ancien chemin serveur continuent
  de fonctionner (`/api/posts/{id}/media` avec `path` reste disponible).

### Ajouté
- **Compte actif, partout** : un sélecteur de compte (barre latérale, en-tête sur mobile) mémorisé dans le navigateur.
  Accueil, À valider, Veille, Performances, Commentaires et Réponses ne montrent que le compte choisi, ou
  « Tous les comptes ». Créer et Calendrier travaillent toujours sur un seul compte.
- **Déplacer sans souris** : sur le calendrier, boutons ‹ › (et flèches gauche/droite sur un post sélectionné)
  pour changer de jour, boutons ↑ ↓ (et flèches haut/bas) pour changer l'ordre dans la file. Le glisser-déposer reste.
- Tests : `web/tz.test.js` (lancé par `make test` si Node est présent) et tests Go sur les changements d'heure.

### Corrigé
- **Calendrier dans le fuseau du compte** : les jours et les heures étaient calculés avec le fuseau du navigateur
  alors que le serveur programme dans celui du compte. Le fuseau du compte (et celui du navigateur s'il diffère)
  est affiché au-dessus de la semaine.
- **Semaine du lundi au dimanche** : elle commençait au jour courant.

### Corrigé
- **Brouillons, enregistrement sûr** : « Enregistrer » sans modification n'envoie plus rien (il envoyait une chaîne
  vide qui effaçait le texte). Le serveur refuse un texte vide sur un brouillon non vide, sauf effacement explicite
  (`clear`). Les modifications non enregistrées survivent à un rechargement (navigateur, par brouillon, effacées à
  l'enregistrement). Chaque post porte un numéro de version : une réécriture IA tardive, ou un enregistrement
  fondé sur une version périmée, est refusé au lieu d'écraser une correction plus récente.

### Ajouté
- **Brouillons, historique** : comparaison mot à mot de chaque version avec le texte courant et bouton
  « Restaurer cette version » (`/api/posts/{id}/restore`), qui crée une nouvelle version sans réécrire l'historique.

### Ajouté
- **Écritures durables et sauvegardes** : `fsync` du fichier temporaire puis du dossier (0600), une sauvegarde par jour
  dans `backups/` (`XGROWTH_BACKUP_KEEP`, 7 par défaut), `xgrowth restore <fichier>` avec validation du contenu et copie
  de l'état courant avant remplacement. `data.json` garde son nom et son emplacement. Voir le README (secrets inclus).
- **LinkedIn, lot 2** : connexion OAuth (`/api/linkedin/connect`, `/auth/linkedin/callback`), publication par
  l'API officielle (texte et image) à l'heure programmée après validation, état de connexion du jeton, alertes
  d'expiration (J-14, J-7, J-3, J-1, expiration) dans le journal, `xgrowth linkedin status` et `to-manual <id>`.
  Variables d'environnement : `LINKEDIN_CLIENT_ID`, `LINKEDIN_CLIENT_SECRET`, `LINKEDIN_REDIRECT_URI`,
  `LINKEDIN_API_VERSION` (facultative). Le jeton est stocké dans `linkedin.json` (0600).

- **LinkedIn, lot 3** : méthode d'écriture adaptée de linkedin-skills (MIT, voir `THIRD_PARTY_NOTICES.md`), toujours
  pliée à la voix du compte et lancée sur demande : accroches, humanizer (appliqué seulement sur clic), audit,
  plan 7 jours (des idées, jamais des posts). Contrôle anti-tics d'IA déterministe, obligatoire avant la validation
  d'un post LinkedIn (`tics <id>`, `validate --ack-tics`). Étiquette de ligne modifiable (`line <id> <ligne>`) et
  filtre par ligne. Plafond de posts par semaine réglé par Allan (`--weekly-cap`, 0 = aucun, lundi à dimanche dans le
  fuseau du compte). Visuel vérifié à l'enregistrement (JPG, PNG, GIF, 10 Mo, 36 152 320 pixels) avec texte
  alternatif, publié avec le post ; avertissement si une idée de visuel n'a pas d'image.

### Modifié
- **Publication séparée des traitements IA** ([#27](https://github.com/AllanPoule/XGrowth-Studio/issues/27), C16) :
  le planificateur lance deux boucles indépendantes, l'une pour la publication (et les alertes d'expiration
  LinkedIn), l'autre pour les traitements IA (veille, tri, actualités, campagnes, synthèse, analyse). Un
  fournisseur IA lent ou bloqué ne retarde plus un post programmé. Chaque appel IA du planificateur a un délai
  maximal de 3 minutes, au plus 2 appels IA simultanés, et un passage n'en chevauche jamais un autre de la même
  boucle. Arrêt propre des deux boucles à la fermeture (`run` comme `serve`, qui attend la fin du planificateur, 30 s au plus). `GET /api/scheduler` donne l'heure du dernier passage de
  chaque boucle. Les demandes manuelles ne sont pas coupées par ce délai.

## V0.10 — 19 novembre 2026

[PR n°16](https://github.com/AllanPoule/XGrowth-Studio/pull/16) · Mémoire persistante et auto-amélioration : lot 1

### Ajouté
- **Analyse des performances** : le studio calcule lui-même, sans IA, le taux d'engagement médian du compte et
  celui des groupes de posts (type de post ; post, réponse ou citation ; longueur ; tiré de la veille ou non). Seuls
  comptent les posts réellement publiés (hors simulation), mesurés au moins 48 h après leur publication et vus au moins 50 fois. Il faut 8 de ces posts pour
  lancer une analyse ; un groupe est retenu s'il compte au moins 3 posts et si sa médiane vaut au moins 1,5 fois
  celle du compte, ou au plus deux tiers.
- L'IA propose au plus 3 règles d'écriture par compte et par analyse, chacune appuyée sur un de ces groupes. Le
  studio écrit lui-même la base chiffrée de la règle, affichée sous son texte avec l'étiquette « performances ».
  Une règle déjà proposée, en place, refusée ou retirée pour ce compte (ou pour tous) n'est pas reproposée. Sans
  assez de posts mesurés, sans groupe qui se détache ou au plafond de règles, aucun appel IA.
- **Réglages › Règles apprises** : bouton « Analyser les performances » et case « Analyse hebdomadaire des
  performances ».
- **Analyse hebdomadaire**, facultative et désactivée par défaut : le planificateur la lance quand la précédente
  date d'au moins 7 jours et qu'au moins 5 nouveaux posts ont été publiés depuis.
- CLI : `rules analyze [--account c]` et `settings --auto-perf-analysis[=false]`. API : `POST /api/rules/analyze`.

### Modifié
- Les règles de performance restent toujours « proposées » : elles ne sont jamais acceptées d'office, même avec
  « Appliquer d'office les règles proposées ». Une fois acceptées, elles guident chaque demande comme les autres
  règles apprises.
- **Plafond de 20 règles de performance en place par compte** : au-delà, accepter ou remettre une règle de
  performance est refusé tant qu'on n'en a pas retiré une. Les règles tirées des retours ne sont pas concernées.

## V0.09 — 18 novembre 2026

[PR n°9](https://github.com/AllanPoule/XGrowth-Studio/pull/9) · Glisser-déposer la file de publication

### Ajouté
- **Prochains posts** sur l'accueil : les tuiles se réordonnent par glisser-déposer, et un brouillon déposé dans la
  liste est validé (signé) puis inséré à cet endroit.
- **Calendrier** : une tuile déposée sur un autre jour est calée au premier créneau libre de ce jour ; la file de
  publication se réordonne aussi.
- CLI : `reorder <id> <id>…` et `move <id> <AAAA-MM-JJ>`.

### Modifié
- Les heures suivent toujours la programmation du compte : réordonner redistribue les heures déjà calculées, sans
  en inventer.
- Les réponses de campagne suivent le rythme de leur campagne : elles ne se glissent ni ne se mélangent aux posts.

## V0.08 — 18 novembre 2026

[PR n°8](https://github.com/AllanPoule/XGrowth-Studio/pull/8) · Sources

### Ajouté
- **Changelog du produit** (URL ou chemin absolu) : ses nouvelles lignes arrivent dans la veille.
- **Dépôts GitHub étoilés** : chaque nouveau dépôt étoilé devient un contenu de veille.
- **Actualités des sources primaires** : un nouveau post d'un compte suivi marqué comme source primaire est rédigé
  en brouillon d'actualité une fois son fil calmé (4 min), s'il a moins de 12 h, dans la limite de 8 par jour.
- Consigne des brouillons de news (commune et par compte) et méthode de récupération, transmise au tri.
- Réglages › Sources et actualités, et le volet « Sources de ce compte » dans chaque fiche.
- CLI : `sources`, `sources news`, `sources status`, `watch primary`, options `--changelog --github-user --news`.

## V0.07 — 18 novembre 2026

[PR n°7](https://github.com/AllanPoule/XGrowth-Studio/pull/7) · Campagnes de réponses (prospection)

### Ajouté
- **Onglet Commentaires** : réponses À valider, Envoyées et Écartées, avec « N / 10 envoyée(s) aujourd'hui » par
  campagne. Rien ne part sans validation.
- **Campagnes** par compte : réponses par jour, minutes entre deux réponses, jours avant de répondre de nouveau au
  même auteur, budget de recherches X par jour, fenêtre horaire, plafond d'abonnés de l'auteur, notes.
- **Douleurs** : les mots du lecteur, les recherches X, des exemples qui correspondent ou non, la solution et où
  elle a été vérifiée (obligatoires pour une douleur active), une preuve facultative.
- **Consigne des commentaires**, commune, avec une valeur par défaut prudente (aider d'abord, transparence, ni URL
  ni concurrent).
- Le planificateur fait tourner les campagnes actives au plus une fois par heure ; l'IA qualifie les posts trouvés
  et rédige les réponses.
- CLI : `campaign list|save|run|remove|candidates`, `comments`, `settings --comments-prompt`.

### Modifié
- Les réponses de campagne ont leur propre rythme d'envoi et n'occupent ni les créneaux ni la limite quotidienne
  des posts du compte. Elles ne comptent plus dans « À valider ».

## V0.06 — 18 novembre 2026

[PR n°6](https://github.com/AllanPoule/XGrowth-Studio/pull/6) · Écriture, sur le modèle de Timon Growth Studio

### Ajouté
- **Vos derniers retours** : chaque correction faite à la main sur un texte de l'IA devient un retour « Correction »
  (texte de l'IA, puis le vôtre), à côté des réécritures, rejets et leçons. Chaque retour a une catégorie (fond, ton,
  longueur, erreur factuelle, timing, visuel, autre) et une case « Ne pas apprendre ». L'IA les relit avant
  d'écrire, et la synthèse des retours s'en nourrit.
- **Repères à compléter** : l'IA n'invente plus de vécu ni de chiffre et écrit « [À COMPLÉTER : …] » à la place.
  Un brouillon qui en contient encore ne peut pas être validé, et sa carte le signale.
- **Profil du compte** : rôle (personnel, marque, automatisé), compte ouvertement automatisé, nom affiché, une
  langue par compte. Le rôle change la façon d'écrire (« je », « nous » ou un style neutre).
- **IA par compte** : fournisseur et modèle propres à un compte, sinon ceux des réglages.
- **Banque d'idées** (dans « Créer ») : l'IA propose des idées (titre, angle, pourquoi), vous les retenez ou les
  écartez, puis elle rédige le post d'une idée retenue. Réglages : activation, sujets, idées par demande, rédaction
  directe, consignes de suggestion et de rédaction.
- **Programmation sur plage horaire**, en option des créneaux fixes : début et fin (08:00–22:00), écart tiré au
  hasard entre deux posts (30 à 60 min), aléa du premier post du jour (± 15 min), maximum par jour.
- **Modèle de voix structuré** à insérer (qui parle, ton, phrases types, ce qu'on ne dit jamais, exemples).
- Options CLI : `account set --role --ai-provider --ideas-enabled --schedule window …`, `ideas`, `learnings set`.

### Modifié
- Rejets et réécritures gardent leur origine et le post concerné.

## V0.05 — 18 novembre 2026

[PR n°5](https://github.com/AllanPoule/XGrowth-Studio/pull/5)

### Ajouté
- **Veille en cartes** : 3 par ligne sur ordinateur, 2 en tablette, 1 sur téléphone, avec les actions alignées en
  bas de chaque carte.
- **Badge « New »** sur les contenus récupérés depuis la dernière visite de la veille, et leur nombre à côté du
  titre. La date de visite est enregistrée sur le serveur : elle reste juste d'un navigateur à l'autre.
- **Thème automatique**, activé par défaut, qui suit le réglage clair ou sombre du système, même pendant que la page
  est ouverte. Le bouton passe de Automatique à Clair puis à Sombre.
- « Remettre à trier » sur les contenus ignorés.
- Ce changelog, et le numéro de version dans Réglages, qui renvoie ici.

### Modifié
- **Le tri par l'IA ne fait plus que suggérer.** Un contenu jugé peu pertinent reste « À trier » avec son score,
  la raison et l'angle proposé : seul un clic de l'utilisateur change son statut (ou un brouillon automatique, si
  le compte l'a activé). « À trier » affiche d'abord les suggestions de l'IA, puis les contenus pas encore notés,
  puis ceux jugés peu pertinents.
- Un contenu déjà noté n'est plus renoté à chaque passage du planificateur.
- Numérotation des versions : le studio passe de « 1.0.0 » à « 0.05 ».

### Corrigé
- Les contenus de veille partaient seuls dans « Ignorés » dès qu'on ouvrait le studio. Au premier démarrage, la
  migration des données (version 2) remet dans « À trier » ceux que l'IA y avait placés.
- Un binaire macOS de 20 Mo (`XGrowth-Studio`), commité par erreur en V0.04, est retiré du dépôt et ignoré.
- Un studio configuré mais sans aucun post plantait à l'affichage : l'état renvoie désormais des listes vides au
  lieu de `null`.
- Répondre et Citer n'apparaissent dans la veille que pour un vrai post X (pas pour un changelog, un dépôt GitHub
  ou une news).

## V0.04 — 18 novembre 2026

[PR n°4](https://github.com/AllanPoule/XGrowth-Studio/pull/4) · Affiner l'IA, sur le modèle de Timon Growth Studio

### Ajouté
- **Réglages › Affiner l'IA** : règles d'écriture communes et consignes par type de demande (idées originales,
  posts tirés de la veille, réponses, citations, réécritures, idée de visuel), communes ou propres à un compte.
- **Par compte** : objectifs, longueur visée (court, moyen, long), posts de référence dont l'IA reprend le ton sans
  les recopier.
- **Types de posts** avec part visée : l'IA privilégie le type le plus en retard sur 30 jours, et le compositeur
  permet d'en imposer un.
- **Règles apprises** : l'IA relit les rejets, réécritures et leçons, puis propose des règles que l'on accepte,
  modifie, refuse ou retire. Synthèse hebdomadaire facultative.
- Idée de visuel proposée avec chaque brouillon quand un guide est renseigné.
- Options CLI correspondantes (`account set`, `settings`, `draft --type`, `rules`).

## V0.03 — 18 novembre 2026

[PR n°3](https://github.com/AllanPoule/XGrowth-Studio/pull/3) · Identité visuelle « Editorial Intelligence »

### Ajouté
- Nouvelle identité : palette graphite, lime et menthe, thèmes sombre et clair, monogramme, logos et visuel de
  partage. Signature « Your voice. Amplified. ».
- Charte graphique dans [`docs/BRAND.md`](docs/BRAND.md).

## V0.02 — 18 novembre 2026

[PR n°2](https://github.com/AllanPoule/XGrowth-Studio/pull/2) · Première version du studio

### Ajouté
- Studio web : tableau de bord, file « À valider » façon X, création par l'IA, calendrier, veille, performances,
  réglages, version mobile.
- Onboarding en sept étapes : IA, connexion à X, voix, rythme et garde-fous, veille, premier post.
- IA au choix, détectée sur la machine et testable en direct : Claude Code, Codex (ChatGPT), Gemini CLI, API Claude.
  Les CLI tournent sans aucun outil, avec une sortie contrainte par JSON Schema.
- Connexion à X par AgentX (session navigateur), API X officielle ou simulation ; identité du compte vérifiée avant
  chaque envoi.
- Garde-fous : validation signée obligatoire, concurrents et expressions interdites, longueur, doublons, limite
  quotidienne, écart minimal entre deux posts.
- Sécurité : code d'installation à usage unique, mot de passe, sessions révocables, noms d'hôte autorisés.
- CLI à enveloppe JSON pour les agents (`SKILL.md`), CI GitHub, modèles de déploiement privé.

## V0.01 — 18 novembre 2026

[PR n°1](https://github.com/AllanPoule/XGrowth-Studio/pull/1) · Cadre du projet

### Ajouté
- Licence AGPL-3.0, mentions des licences MIT de Growth Studio et d'AgentX, accord de contribution (CLA) et guide
  de contribution.
