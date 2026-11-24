# Changelog

Toutes les évolutions notables de XGrowth Studio. Une version correspond à une pull request fusionnée :
V0.05 est la cinquième. Le numéro affiché dans le studio (Réglages) et par `xgrowth version` est celui de la
dernière version.

## Non publié

### Sécurité
- **Isolation des CLI d'IA (Codex, Gemini, Claude Code)** : Codex reçoit un `CODEX_HOME` temporaire avec un
  `config.toml` sans outil ni MCP (et les mêmes réglages en `-c`), `web_search` désactivé, sandbox lecture seule,
  règles utilisateur ignorées ; Gemini reçoit un `GEMINI_CLI_HOME` temporaire avec une politique qui refuse tous les
  outils (aussi via `--admin-policy`) et aucun MCP. Les trois CLI tournent avec un environnement en liste blanche
  (plus aucune clé ni jeton hors connexion propre à la CLI) dans un répertoire de travail vide. Tests sur les
  arguments, l'environnement et la configuration, dont un contenu de veille malveillant. README aligné sur la
  protection réelle et ses limites (Gemini non exécuté pour valider la politique).

### Ajouté
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
