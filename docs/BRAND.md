# XGrowth Studio — Identité visuelle
**Direction : Editorial Intelligence · v1.0 · 18 novembre 2026**

## Promesse et personnalité
Un studio créatif et analytique pour faire grandir une présence sur X, sans renoncer à sa voix ni à son jugement.

- **Signature :** Your voice. Amplified.
- **FR :** Ta voix, plus loin.
- **Territoire :** intelligence éditoriale, lucidité, progression, contrôle humain.
- **Traits :** précis, premium, calme, audacieux par touches, jamais tapageur.
- **À éviter :** branding « hack de croissance », imitation du logo X, gradients violets génériques, écrans saturés de badges.

## Le symbole
Deux trajectoires se croisent pour créer un **X distinctif**. Une diagonale monte et se termine en flèche : les idées se transforment en progression. L'autre indique le signal, les échanges et la multiplicité des voix.

Le symbole est une création pour XGrowth Studio, **pas le logo de X/Twitter**. Ne pas remplacer ce symbole par celui de X et ne pas présenter l'outil comme affilié à X.

### Déclinaisons livrées
| Fichier | Usage |
|---|---|
| `web/favicon.svg` | Icône de l'application, favicon, raccourcis |
| `web/logo-symbol.svg` | Symbole seul, transparent, à placer sur fond sombre |
| `web/logo-lockup.svg` | Signature horizontale pour documentation et présentations sombres |
| `web/social-cover.svg` | Visuel de présentation 1200 × 630, à exporter en PNG pour les réseaux |
| `web/brand.css` | Système de design appliqué à tous les écrans |

**Zone de protection :** au moins 25 % de la largeur de l'icône autour du symbole. **Taille minimale :** icône 24 px dans l'UI (32 px recommandé) ; signature complète au moins 160 px de large. Ne pas étirer, tourner ni superposer des couleurs concurrentes.

## Palette

### Thème sombre (primaire)
| Nom | Hex | Rôle |
|---|---|---|
| Deep Ink | `#0B1015` | Fond principal |
| Studio Slate | `#151D25` | Surfaces principales |
| Slate Elevated | `#1D2832` | Surfaces secondaires |
| Border | `#2A3740` | Contours subtils |
| Paper White | `#F5F8F3` | Texte essentiel |
| Soft Fog | `#9AA9AC` | Texte secondaire |
| Lime Signal | `#C7F586` | Actions, sélection et signature |
| Mint Pulse | `#7DD8BE` | Navigation active et métriques |
| Lavender Note | `#B6AAFF` | Contraste ponctuel réservé aux données |

### Thème clair
| Nom | Hex | Rôle |
|---|---|---|
| Canvas | `#F3F5F1` | Fond |
| Paper | `#FFFFFF` | Cartes |
| Ink | `#152522` | Texte |
| Forest | `#286A48` | Liens et focus |
| Fog | `#536960` | Texte secondaire |

Dégradé signature : `linear-gradient(115deg, #C7F586 0%, #A4ED9F 48%, #76DEC1 100%)`. Réservé aux boutons primaires, marque et mise en valeur. Ne pas l'utiliser sur toutes les cartes.

**Accessibilité :** texte normal ≥ 4,5:1, texte large ≥ 3:1, focus visible et navigation clavier, bouton primaire avec texte `#173023`, animations réduites avec `prefers-reduced-motion`. Tester les cas réels en mode clair et sombre.

## Typographie
Pile système sans dépendance externe : **Avenir Next / SF Pro Text / Inter / system-ui**.

- Titres : gras 790, interlettrage négatif léger, tailles fluides.
- Corps : 14–15 px, interligne 1,55–1,7.
- Métadonnées : 11–12 px, contraste lisible et rigueur.
- Chiffres : tabular lining si disponibles ; hiérarchie marquée dans les KPI.
- Maquette : pas de police externe chargée, donc pas de requête réseau additionnelle.

## Design de l'application
- **Sidebar :** identité claire, séparation du workspace et des préférences, sélection discrète soulignée de citron vert.
- **Accueil :** bannière éditoriale, KPIs resserrés, entrée rapide dans la rédaction, suite du travail visible.
- **Créer :** canvas d'écriture valorisé, action primaire unique, choix du nombre de brouillons compact.
- **À valider :** la lecture passe avant les boutons, actions secondaires neutres, statut toujours lisible.
- **Veille :** cartes orientées signal, score et raison visibles, actions de tri en second niveau.
- **Calendrier :** journées plus lisibles, défilement horizontal sur tablette et téléphone.
- **Performances :** données à forte hiérarchie, courbes et barres aux couleurs XGrowth.
- **Onboarding :** mise en place progressive avec la même identité que l'app.
- **Clair et sombre :** vrai système de tokens, pas un simple filtre inversé.

## Composants et règles
| Élément | Spécification |
|---|---|
| Grille | Écart 15 px, contenant maximal 1180 px |
| Carte | Rayon 18 px, filet discret, ombre faible |
| Bouton primaire | Pill, dégradé signal, texte sombre |
| Bouton secondaire | Slate, contour et état hover |
| Champ | 43 px minimum, rayon 12 px, halo focus |
| Navigation | Ligne 45 px, icône 18 px |
| Favicon | Fond sombre contrasté, monogramme vectoriel |
| Mobile | Navigation inférieure, 2 KPI par ligne, barres adaptées |
| Animation | 150–200 ms maximum, supprimée si préférence système |

## Ton de la marque
Direct, concret, jamais prétentieux. On parle de **brouillons**, de **validation**, de **voix**, de **résultats**. Pas de promesse magique, pas d'autonomie totale prétendue : la validation humaine reste la décision centrale.

Exemples : « Ta voix. Plus loin. », « Crée avec ton IA. Garde le dernier mot. », « De l'idée au bon post. ».

## Mise en production
1. Construire le binaire comme d'habitude : `make test && make build`.
2. Ouvrir sur desktop, tablette, mobile. Vérifier toutes les 7 vues, les formulaires et l'onboarding.
3. Vérifier clair / sombre et zoom navigateur 200 %, puis les états vide / chargé / erreur.
4. Exporter `social-cover.svg` en PNG si une plateforme refuse les images SVG.
5. Fusionner après validation de la PR ; aucune API, aucune donnée, aucune règle de publication modifiées par cette identité.

## Portée du chantier
Ce lot ne change que les ressources de présentation : CSS, SVG, balises HTML, et le rendu textuel du shell dans `app.js`. Il ne touche ni aux règles métiers, ni à l'authentification, ni à la validation humaine, ni aux intégrations de X.
