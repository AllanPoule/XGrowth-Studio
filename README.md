<div align="center">

<img src="web/favicon.svg" width="88" alt="XGrowth Studio">

# XGrowth Studio

**Le studio de croissance sur X, piloté par l'IA que vous avez déjà.**
Claude Code, Codex (ChatGPT), Gemini CLI ou l'API Claude écrivent dans votre voix.
Vous validez. XGrowth publie.

[Fonctionnalités](#fonctionnalités) · [Démarrage](#démarrage-en-2-minutes) · [IA](#lia-de-votre-choix) · [Connexion à X](#connexion-à-x) · [Déploiement privé](#déploiement-privé-netbird-tailscale) · [CLI et agents](#cli-et-agents) · [Identité visuelle](docs/BRAND.md) · [Changelog](CHANGELOG.md) · [English](#english)

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)
![Licence](https://img.shields.io/badge/licence-AGPL--3.0-8b5cf6)
![Un binaire](https://img.shields.io/badge/d%C3%A9ploiement-un%20binaire-22d3ee)

</div>

![XGrowth Studio — Your voice. Amplified.](web/social-cover.svg)

---

XGrowth Studio est un binaire unique (Go, aucune base de données externe) qui sert un studio web et une CLI :

- **L'IA écrit** : brouillons à partir d'un brief, d'un post repéré ou de rien du tout ; réponses et citations ;
  réécriture à partir de vos retours.
- **Vous gardez la main** : rien ne part sans une validation signée, et des garde-fous bloquent ce qui ne doit
  jamais partir.
- **Le studio publie** au bon moment, surveille votre domaine, trie ce qui mérite un post et mesure ce qui marche.

## Fonctionnalités

| | |
| --- | --- |
| ✍️ **Rédaction** | Brouillons dans votre voix (ton, sujets, consignes), réponses et citations de posts X, réécriture guidée. Chaque retour devient une *leçon* réutilisée pour tous les posts suivants. |
| 🎛️ **IA affinée** | Objectifs, longueur, posts de référence, types de posts avec part visée, consignes par demande (idées, veille, réponses, citations, réécritures, visuels), règles communes, et règles tirées de vos retours par une synthèse que vous validez. |
| 🧠 **IA au choix** | Claude Code et Codex utilisent vos abonnements Claude ou ChatGPT déjà installés sur la machine. Gemini CLI et l'API Claude sont aussi disponibles. Le choix se fait dans l'interface, avec un test en direct. |
| ✅ **Validation** | File « À valider » façon X : aperçu, compteur de caractères, retouche, validation signée, planification au prochain créneau libre ou à l'heure choisie. |
| 🛡️ **Garde-fous** | Concurrents jamais nommés, expressions interdites, limite de longueur, pas de doublon, limite quotidienne, écart minimal entre deux posts. Tout est revérifié juste avant l'envoi, ainsi que l'identité du compte X connecté. |
| 📡 **Veille** | Comptes et recherches X suivis, import des signets, tri par l'IA (score, raison, angle) et brouillons automatiques facultatifs. |
| 📅 **Calendrier** | Vue semaine, file de publication, publication automatique par le planificateur. |
| 📈 **Performances** | Vues, likes, reposts, réponses, taux d'engagement. Les meilleurs posts guident l'IA. |
| 🔐 **Sécurité** | Mot de passe créé avec un code d'installation à usage unique, sessions révocables, noms d'hôte autorisés, CLI d'IA lancées **sans aucun outil**. |
| 🤖 **Pilotable par agent** | Chaque commande CLI renvoie une enveloppe JSON, comme [AgentX](https://github.com/DezXBT/AgentX). `SKILL.md` décrit l'outil à un agent. |

## Démarrage en 2 minutes

```bash
git clone https://github.com/AllanPoule/XGrowth-Studio && cd XGrowth-Studio
make build            # ou : go build -o xgrowth .
./xgrowth serve       # http://127.0.0.1:8787
```

Au premier lancement, le terminal affiche un **code d'installation** (copié aussi dans `~/.xgrowth/setup-code.txt`).
Ouvrez le studio, saisissez ce code, choisissez un mot de passe : l'assistant vous guide ensuite en cinq minutes.

1. **L'IA** : XGrowth détecte celles qui sont installées. Vous en choisissez une et vous la testez en direct.
2. **Le compte X** : session AgentX, API officielle ou simulation.
3. **La voix** : un style de départ, vos mots, vos sujets.
4. **Le rythme et les garde-fous** : créneaux, fuseau, concurrents à ne jamais nommer.
5. **La veille**, puis **un premier post** rédigé sous vos yeux.

## L'IA de votre choix

| Fournisseur | Ce qu'il faut sur la machine | Coût |
| --- | --- | --- |
| **Claude Code** | `claude`, connecté une fois (`claude` puis `/login`) | votre abonnement Claude |
| **Codex (ChatGPT)** | `codex`, connecté (`codex login`) | votre abonnement ChatGPT |
| **Gemini CLI** *(expérimental)* | `gemini`, connecté | votre compte Google |
| **API Claude** | `ANTHROPIC_API_KEY` | à l'usage (`claude-opus-5-5` par défaut) |

Les CLI sont lancées en mode non interactif, **sans aucun outil** (pas de shell, pas de fichiers, pas de web) et dans
un dossier temporaire vide. Les posts lus sur X ne peuvent donc rien leur faire exécuter. Les réponses sont
contraintes par un JSON Schema. Claude Code est lancé sans `ANTHROPIC_API_KEY` pour utiliser l'abonnement et non
l'API.

```bash
./xgrowth providers --pretty                    # ce qui est détecté
./xgrowth ai-test --provider codex --pretty     # un essai réel
./xgrowth settings --provider claude-code       # le choix (aussi dans Réglages)
```

## Affiner l'IA

Tout ce qui oriente l'écriture se règle dans **Réglages › Affiner l'IA** (pour tous les comptes) et dans la fiche de
chaque compte (**Modifier › Affiner l'IA pour ce compte**). Une consigne vide reprend celle du niveau au-dessus :
compte, puis commun, puis une valeur par défaut neutre, affichée en grisé.

| Réglage | Niveau | Effet |
| --- | --- | --- |
| Objectifs | compte | rappelés à chaque demande : chaque post doit les servir |
| Longueur visée | compte | courte (défaut), moyenne ou longue ; la limite de 280 caractères reste sans X Premium |
| Posts de référence | compte | 10 posts au plus dont l'IA imite le ton et le rythme, sans les recopier |
| Règles d'écriture | commun | une par ligne, ajoutées à chaque demande |
| Consignes par demande | commun et compte | idées originales (sans sujet), posts tirés de la veille, réponses, citations, réécritures |
| Idée de visuel | commun et compte | renseignée, l'IA propose un visuel avec chaque brouillon (le studio ne génère pas d'image) |
| Types de posts | commun et compte | libellé, effet recherché, part visée et consigne ; l'IA classe chaque brouillon et privilégie le type le plus en retard sur 30 jours (ou celui que vous imposez) |
| Règles apprises | commun ou compte | la synthèse relit vos rejets, réécritures et leçons et propose des règles ; vous les acceptez (texte modifiable), refusez ou retirez. Option : les appliquer d'office, synthèse hebdomadaire automatique (désactivées par défaut) |
| Vos retours | compte | chaque correction faite à la main sur un texte de l'IA devient un retour « Correction » (avant → après), à côté des réécritures, rejets et leçons. Chaque retour a une catégorie (fond, ton, longueur, erreur factuelle, timing, visuel, autre) et une case « Ne pas apprendre ». L'IA relit les retours récents avant d'écrire, et la synthèse s'en nourrit |
| Repères à compléter | — | l'IA n'invente aucun vécu ni chiffre : elle écrit « [À COMPLÉTER : situation réelle vécue côté créateur] ». Un brouillon qui en contient encore ne peut pas être validé |
| Profil | compte | rôle (personnel : « je », marque : « nous », automatisé : neutre), compte ouvertement automatisé, nom affiché, une langue par compte |
| IA du compte | compte | fournisseur et modèle propres au compte (vide : ceux des réglages) |
| Banque d'idées | compte | l'IA propose des idées (titre, angle, pourquoi) que vous retenez ou écartez, puis rédige le post d'une idée retenue. Réglages : activation, sujets, idées par demande (5), rédaction directe (non), consignes de suggestion et de rédaction |
| Programmation | compte | créneaux fixes, ou plage horaire (08:00–22:00) avec un écart tiré au hasard entre deux posts (30 à 60 min), un aléa sur le premier post du jour (± 15 min) et un maximum par jour |
| Voix | compte | texte libre et long, avec un modèle structuré à insérer (ton, phrases types, ce qu'on ne dit jamais, exemples) |

```bash
./xgrowth account set moncompte --goals "50 clients d'ici mars" --length moyen --post-types defaut
./xgrowth account set moncompte --examples-file mes-posts.txt   # posts séparés par une ligne ---
./xgrowth settings --rules-file regles.txt --reply-prompt "Une phrase, un fait vérifiable."
./xgrowth draft moncompte --type coulisses
./xgrowth rules synthesize --pretty && ./xgrowth rules accept <id>
./xgrowth account set moncompte --role marque --ai-provider codex --schedule window --window 08:00-22:00 --gap 30-60
./xgrowth account set moncompte --ideas-enabled --idea-themes $'audit\nproduit' && ./xgrowth ideas suggest moncompte
./xgrowth ideas draft <id>                         # le post d'une idée retenue, à valider
./xgrowth learnings set <id> --category ton --ignore
```

## Campagnes de réponses

L'onglet **Commentaires** répond aux posts qui expriment une douleur que vous savez résoudre (prospection). Une
campagne appartient à un compte et décrit des **douleurs** :

- la douleur, avec les mots du lecteur ;
- les recherches X qui la trouvent, une par ligne ;
- des exemples qui correspondent ou non ;
- la **solution** concrète et l'endroit où elle a été **vérifiée** (obligatoires pour une douleur active), plus une preuve facultative.

Ses réglages :

| Réglage | Défaut |
| --- | --- |
| Réponses par jour | 10 |
| Minutes entre deux réponses | 15 |
| Jours avant de répondre de nouveau au même auteur | 7 |
| Budget de recherche (recherches X par jour) | 12 |
| Fenêtre horaire (`HH-HH`) | toute la journée |
| Plafond d'abonnés de l'auteur | aucun |
| Notes | — |

Le planificateur fait tourner chaque campagne active au plus une fois par heure, dans sa fenêtre. Il écarte les
reposts, vos propres posts, les auteurs trop suivis et ceux à qui une réponse a été rédigée récemment. L'IA qualifie
ensuite chaque post selon la **Consigne des commentaires** (réglable, avec une valeur par défaut prudente : aider
d'abord, transparence, pas d'URL ni de concurrent) et rédige la réponse.

Les réponses arrivent dans **À valider**, puis passent dans **Envoyés** ou **Écartés**, avec le compteur
« N / 10 envoyée(s) aujourd'hui ». Validée, une réponse part au plus tôt en respectant l'écart, le maximum du jour et la
fenêtre de sa campagne, sans occuper les créneaux des posts du compte. **Rien ne part sans validation.**

```bash
./xgrowth campaign save campagne.json      # crée (sans id) ou met à jour
./xgrowth campaign run <id> --pretty       # cherche et rédige maintenant
./xgrowth comments --status draft          # réponses à valider
./xgrowth settings --comments-prompt "…"   # consigne des commentaires
```

## Connexion à X

| Mode | Ce qu'il faut | Remarques |
| --- | --- | --- |
| **Session X (AgentX)** — recommandé | le binaire [`agentx`](https://github.com/DezXBT/AgentX) (dans le `PATH`, à côté de `xgrowth`, dans `bin/` ou via `AGENTX_BIN`) | publication, visuels, veille, signets. Les cookies `auth_token` et `ct0` se collent dans l'assistant : ils sont transmis à agentx par l'environnement et restent sur le serveur. |
| **API X officielle** | un jeton OAuth 2.0 utilisateur dans une variable d'environnement | lecture et recherche payantes côté X, pas de visuels |
| **Simulation** | rien | pour essayer : rien n'est publié |

> ⚠️ L'automatisation par session web n'est pas autorisée par les conditions d'utilisation de X : un compte utilisé
> avec AgentX s'expose à une suspension. L'API officielle est la voie conforme.

Avant chaque envoi, XGrowth vérifie que la session connectée correspond bien au handle du compte. Un post ne part
jamais depuis le mauvais compte.

## Sources

Dans la fiche de chaque compte, **Sources de ce compte** ajoute des sources à la veille. Les réglages communs des
actualités sont dans **Réglages › Sources et actualités**.

| Source | Effet |
| --- | --- |
| Changelog du produit | URL http(s) ou chemin absolu sur le serveur. Les nouvelles lignes arrivent dans la veille (*build in public*) ; le premier relevé sert de référence |
| Dépôts GitHub étoilés | identifiant GitHub. Chaque nouveau dépôt étoilé devient un contenu de veille (API publique de GitHub) |
| Actualités des sources primaires | cochez des comptes suivis comme sources primaires. Un nouveau post de l'un d'eux est rédigé en brouillon d'actualité quand son fil s'est calmé (4 min sans nouveau post), s'il a moins de 12 h, dans la limite de 8 actualités par jour et par compte. Le fil entier est donné à l'IA |
| Consigne des brouillons de news | commune et par compte, avec une valeur par défaut prudente (ce qui change concrètement, sans extrapoler) |
| Méthode de récupération | consignes libres, transmises à l'IA à chaque tri de la veille |

Le planificateur relève le changelog et les étoiles toutes les 60 minutes (réglable) et rédige les actualités
prêtes. Les brouillons d'actualité attendent la validation, comme le reste.

```bash
./xgrowth account set moncompte --changelog https://exemple.fr/CHANGELOG.md --github-user moi --news on
./xgrowth watch primary <id> on            # un compte suivi devient une source primaire
./xgrowth sources --account moncompte      # relève maintenant
./xgrowth settings --news-quiet 4 --news-max-age 12 --news-per-day 8
```

## Déploiement privé (NetBird, Tailscale…)

Le studio peut publier sur votre compte : exposez-le uniquement sur un réseau privé. Exemple sur un serveur Linux
relié à NetBird, derrière nginx :

```bash
make release                                   # binaires linux/amd64, linux/arm64, darwin dans dist/
sudo install -m 755 dist/xgrowth-linux-arm64 /opt/xgrowth/xgrowth
sudo install -m 755 agentx /opt/xgrowth/bin/agentx   # facultatif
sudo cp deploy/xgrowth.service /etc/systemd/system/  # puis adaptez User= et --host
sudo systemctl enable --now xgrowth
journalctl -u xgrowth | grep "Code d'installation"
```

- `deploy/xgrowth.service` : le service systemd. Il tourne sous l'utilisateur qui a connecté Claude Code ou Codex.
- `deploy/nginx-private.conf` : nginx qui n'écoute **que** sur l'IP du réseau privé, en TLS.
- Le DNS public peut pointer vers l'IP privée (enregistrement A sans proxy) : le nom se résout partout, mais le
  service ne répond qu'aux machines du réseau. Le certificat s'obtient par défi DNS
  (`certbot --dns-cloudflare`, par exemple).
- `xgrowth serve --host studio.exemple.fr` autorise ce nom d'hôte (tout autre nom est refusé).

## CLI et agents

Chaque commande imprime une enveloppe JSON : `{"ok":true,"schema_version":"1","data":…}`.

```bash
./xgrowth draft moncompte --topic "3 leçons sur les agents IA" -n 3
./xgrowth reply moncompte https://x.com/quelquun/status/123
./xgrowth posts --status draft --pretty
./xgrowth rewrite <id> --feedback "plus court, une seule idée"
./xgrowth rules synthesize                        # règles proposées à partir des retours
./xgrowth validate <id> --by Prénom              # prochain créneau libre
./xgrowth watch add moncompte --search "agents IA lang:fr -is:retweet"
./xgrowth run                                     # planificateur seul (publication, veille, tri, stats)
./xgrowth help                                    # tout le reste
```

Un agent (Claude Code ou autre) peut veiller, trier, rédiger et réécrire. **Il ne valide jamais** : voir
[`SKILL.md`](SKILL.md).

## Architecture

```
main.go        CLI (enveloppe JSON)              server.go   API HTTP + studio web embarqué (web/)
studio.go      brouillons, validation,           auth.go     mot de passe PBKDF2, sessions, code d'installation
               planification, publication,       providers.go Claude Code / Codex / Gemini CLI / API
               veille, tri, statistiques         ai.go       prompts et schémas JSON
xbackend.go    agentx, API X officielle, simulation
store.go       un fichier JSON (0600), verrou de fichier partagé entre processus
```

Données : `$XGROWTH_HOME/data.json` (par défaut `~/.xgrowth`). Aucun secret (clé, jeton, cookie) n'y est écrit ;
le mot de passe n'y figure que haché.

```bash
make test        # go vet + tests
```

Les évolutions de chaque version sont listées dans [`CHANGELOG.md`](CHANGELOG.md) (une version par pull request).

## Identité visuelle

**Editorial Intelligence.** Un studio graphite, lime et menthe, avec un symbole vectoriel de croissance, des thèmes clair et sombre et des composants unifiés. La charte détaille les couleurs, la typographie, les règles de logo, l’accessibilité et les écrans : [découvrir la charte graphique](docs/BRAND.md).

## Crédits et licence

XGrowth Studio reprend les idées de [Growth Studio](https://github.com/ai-timon-admin/growth-studio) (file de
validation, veille, leçons) et pilote X à travers [AgentX](https://github.com/DezXBT/AgentX). Leurs licences MIT sont
reproduites dans [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).

XGrowth Studio est distribué sous **GNU AGPL-3.0** ([`LICENSE`](LICENSE)). Si vous proposez une version modifiée
comme service en ligne, vous devez en publier le code source. Les contributions passent par un CLA
([`CONTRIBUTING.md`](CONTRIBUTING.md)).

---

## English

**XGrowth Studio** is a single Go binary that runs an AI-powered growth studio for X (Twitter). It writes posts,
replies and quotes in your voice with the AI you already have: **Claude Code** (your Claude subscription),
**Codex** (your ChatGPT subscription), **Gemini CLI** or the **Claude API**. Nothing is published without your signed
validation. Guardrails (competitor names, banned phrases, length, duplicates, daily limits, connected-account
identity) are checked again right before sending. It also watches accounts and searches, triages what deserves a
post, schedules on time slots and tracks performance. The AI can be tuned per account and globally: goals, target
length, reference posts, post types with target shares, per-request instructions (original ideas, watch items,
replies, quotes, rewrites, visual ideas), common writing rules, and rules distilled from your feedback that you
accept, refuse or revoke.

```bash
make build && ./xgrowth serve   # then open http://127.0.0.1:8787 and follow the onboarding
```

AI CLIs run non-interactively with **all tools disabled** in an empty temp directory, with JSON-schema-constrained
output. X is driven through [AgentX](https://github.com/DezXBT/AgentX) (browser session, which X's terms do not
allow), the official X API, or a dry-run mode. Expose the studio on a private network only (NetBird, Tailscale);
see `deploy/`. Licensed under AGPL-3.0.
