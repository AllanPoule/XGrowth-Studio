# Changelog

Toutes les évolutions notables de XGrowth Studio. Une version correspond à une pull request fusionnée :
V0.05 est la cinquième. Le numéro affiché dans le studio (Réglages) et par `xgrowth version` est celui de la
dernière version.

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
