# Publier une version du fork

Procédure destinée au mainteneur. Elle décrit ce que déclenchent les workflows `.github/workflows/default.yml` (CI) et `.github/workflows/release.yml` (publication de l'image et de la release), et ce qu'il faut contrôler à la main.

## Ce qui déclenche quoi

| Action | Résultat |
|---|---|
| Push sur `main` | CI (formatage, `go vet`, tests, build, lint) puis publication de l'image de test `ghcr.io/superdcat/tesla-ble-http-proxy:edge`. **Ni `latest`, ni tag versionné, ni release.** |
| Push d'un tag `X.Y.Z-tb.N` (par exemple `2.3.0-tb.2`) pointant sur un commit de `main` | CI, puis image `X.Y.Z-tb.N`, `latest` et `sha-<court>`, puis release GitHub avec les quatre binaires Linux. **Seul déclencheur de `latest`.** |

Le tag doit respecter `X.Y.Z-tb.N` (version de base de wimaha, puis numéro de version du fork) et pointer sur un commit déjà présent dans `main`, sinon le workflow s'arrête en erreur.

## Réglage unique du dépôt (première fois seulement)

Dans GitHub, **Settings > Actions > General > Workflow permissions** : choisir **Read and write permissions**, puis enregistrer. Sans cela, la publication de l'image et la création de la release échouent faute de droits d'écriture.

## Publier une version

1. **Pousser `main`** : `git push origin main`.
2. **Contrôler la CI** : onglet **Actions**, le workflow `Publish` (et son job `CI`) doit être vert. Si la CI échoue, corriger, pousser de nouveau, ne pas tagger.
3. **Créer le tag** sur le commit de `main` à publier, par exemple :

   ```
   git tag 2.3.0-tb.2
   ```

4. **Pousser ce tag, et lui seul** :

   ```
   git push origin 2.3.0-tb.2
   ```

   Ne jamais utiliser `git push --tags` : le clone porte aussi les anciens tags `v3.0.x` de Lenart12 et d'autres tags de l'amont, qui ne doivent pas partir sur le dépôt du fork.
5. **Suivre le workflow** `Publish` dans l'onglet **Actions** jusqu'au job `GitHub release`.

## Première publication : rendre le paquet public

Après le premier push, GitHub crée le paquet GHCR en **privé** : `docker pull` échoue pour tout le monde sauf pour vous.

1. Ouvrir le paquet (page du dépôt, colonne **Packages**, ou `https://github.com/users/superdcat/packages`), puis **Package settings**.
2. Dans **Danger Zone**, **Change visibility** : choisir **Public**.
3. Dans **Manage Actions access**, vérifier que le dépôt `superdcat/TeslaBleHttpProxy` a bien accès au paquet avec le rôle **Write** (nécessaire aux publications suivantes).

Ces deux réglages ne sont à faire qu'une fois par paquet.

## Vérifier la publication

Depuis une machine (ou un Raspberry Pi) **non authentifiée** auprès de ghcr.io (aucun `docker login ghcr.io`), pour prouver que le paquet est public :

1. Image versionnée :

   ```
   docker pull ghcr.io/superdcat/tesla-ble-http-proxy:2.3.0-tb.2
   ```

2. Démarrer le proxy avec cette image (compose de la documentation) puis lire sa version :

   ```
   curl http://<ip>:8080/api/proxy/1/version
   ```

   La réponse doit contenir `"version":"2.3.0-tb.2"` et `"flavor":"superdcat"`.
3. Vérifier que `latest` désigne la même image : `docker pull ghcr.io/superdcat/tesla-ble-http-proxy:latest`, puis `docker image ls` (même identifiant).
4. Ouvrir la **release** GitHub du tag : elle doit contenir les quatre binaires `TeslaBleHttpProxy-linux-amd64`, `-arm64`, `-armv7` et `-armv6`. Télécharger celui de la machine de test, le rendre exécutable et l'essayer :

   ```
   chmod +x TeslaBleHttpProxy-linux-arm64
   ./TeslaBleHttpProxy-linux-arm64
   ```

   (adapter le nom au processeur ; un Raspberry Pi Zero 2 W en système 64 bits utilise `arm64`, en 32 bits `armv7`).

Le fork n'a pas de fichier de changelog : les notes de la release sont générées par GitHub à partir des commits. Les relire et les compléter à la main depuis la page de la release si besoin, avant d'annoncer la version.

## Pousser le plugin Jeedom en dernier

La documentation du plugin `JeedomTeslaBLE` recommande un tag précis (`2.3.0-tb.2` pour la version actuelle). Elle ne doit **jamais** pointer vers une image absente :

1. Publier l'image du fork et la vérifier (sections ci-dessus).
2. **Seulement ensuite**, pousser le dépôt du plugin (`git push` dans `JeedomTeslaBLE`).

Si une version plus récente que celle citée par la documentation du plugin est publiée, mettre à jour le tag recommandé dans `docs/fr_FR/installation-proxy.md` et `docs/fr_FR/index.md` du plugin avant de pousser celui-ci.

## En cas de problème

- **Le workflow échoue sur « tag does not point to a commit of main »** : le tag n'est pas sur `main`. Supprimer le tag (`git push origin :refs/tags/2.3.0-tb.2`, puis `git tag -d 2.3.0-tb.2`), le recréer sur le bon commit, le repousser.
- **Le workflow échoue sur « tag ... does not match X.Y.Z-tb.N »** : le nom du tag est mal formé ; même remède.
- **`denied` ou `permission_denied` à la publication de l'image** : le réglage **Workflow permissions** n'est pas en lecture et écriture, ou le dépôt n'a pas accès au paquet (**Manage Actions access**).
- **`docker pull` répond `unauthorized` ou `denied` depuis une autre machine** : le paquet est encore privé.
