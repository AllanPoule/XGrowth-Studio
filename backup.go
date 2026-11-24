package main

// Écritures durables, sauvegardes tournantes et restauration de data.json.
//
// Les sauvegardes vivent dans $XGROWTH_HOME/backups (dossier 0700, fichiers
// 0600). Elles contiennent les mêmes secrets que data.json (cookies, jetons) :
// elles ne doivent jamais quitter le dossier de données en clair.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	backupDirName     = "backups"
	backupPrefix      = "data-"
	backupSuffix      = ".json"
	defaultBackupKeep = 7
	// Les copies de l'état courant faites avant une restauration ont leur
	// propre préfixe et ne sont jamais purgées par la rotation quotidienne.
	preRestorePrefix = "pre-restore-"
)

// backupKeep : nombre de sauvegardes quotidiennes gardées (XGROWTH_BACKUP_KEEP,
// 0 = sauvegardes désactivées).
func backupKeep() int {
	if v := os.Getenv("XGROWTH_BACKUP_KEEP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return defaultBackupKeep
}

// writeFileDurable écrit path de façon atomique et durable : fichier
// temporaire (0600), fsync du fichier, renommage, puis fsync du dossier.
func writeFileDurable(path string, b []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func backupDir(dataDir string) string { return filepath.Join(dataDir, backupDirName) }

// listBackups renvoie les sauvegardes quotidiennes, de la plus ancienne à la plus récente.
func listBackups(dataDir string) []string {
	ents, err := os.ReadDir(backupDir(dataDir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, backupPrefix) && strings.HasSuffix(n, backupSuffix) {
			out = append(out, filepath.Join(backupDir(dataDir), n))
		}
	}
	sort.Strings(out) // l'horodatage est dans le nom
	return out
}

// backupIfDue crée la sauvegarde du jour si elle n'existe pas encore, puis
// applique la rotation. L'état à sauvegarder est celui de data.json.
func backupIfDue(dataDir string, now time.Time) error {
	keep := backupKeep()
	if keep == 0 {
		return nil
	}
	day := backupPrefix + now.Format("20060102")
	for _, p := range listBackups(dataDir) {
		if strings.HasPrefix(filepath.Base(p), day) {
			return nil
		}
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "data.json"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(backupDir(dataDir), 0o700); err != nil {
		return err
	}
	name := backupPrefix + now.Format("20060102-150405") + backupSuffix
	if err := writeFileDurable(filepath.Join(backupDir(dataDir), name), b); err != nil {
		return err
	}
	all := listBackups(dataDir)
	for len(all) > keep {
		if err := os.Remove(all[0]); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		all = all[1:]
	}
	return nil
}

// validateBackup vérifie qu'un fichier est un data.json exploitable : JSON
// lisible, objet racine avec une version de schéma connue.
func validateBackup(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("JSON illisible : %w", err)
	}
	if _, ok := raw["version"]; !ok {
		return errors.New("schéma inattendu : champ « version » absent")
	}
	d := &Data{}
	if err := json.Unmarshal(b, d); err != nil {
		return fmt.Errorf("schéma inattendu : %w", err)
	}
	if d.Version < 1 || d.Version > dataVersion {
		return fmt.Errorf("version de schéma %d non prise en charge (attendu 1 à %d)", d.Version, dataVersion)
	}
	for _, k := range []string{"accounts", "posts", "items"} {
		if v, ok := raw[k]; ok && string(v) != "null" && (len(v) == 0 || v[0] != '[') {
			return fmt.Errorf("schéma inattendu : « %s » n'est pas une liste", k)
		}
	}
	return nil
}

// restoreBackup remplace data.json par src après validation. L'état courant
// est copié dans backups/pre-restore-<horodatage>.json avant le remplacement.
// Renvoie le chemin de cette copie ("" s'il n'y avait pas de data.json).
func restoreBackup(dataDir, src string) (string, error) {
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := validateBackup(b); err != nil {
		return "", fmt.Errorf("restauration refusée (%s) : %w", filepath.Base(src), err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	lf, err := os.OpenFile(filepath.Join(dataDir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return "", err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	dst := filepath.Join(dataDir, "data.json")
	var saved string
	if cur, err := os.Open(dst); err == nil {
		old, rerr := io.ReadAll(cur)
		cur.Close()
		if rerr != nil {
			return "", rerr
		}
		if err := os.MkdirAll(backupDir(dataDir), 0o700); err != nil {
			return "", err
		}
		saved = filepath.Join(backupDir(dataDir), preRestorePrefix+time.Now().Format("20060102-150405.000")+backupSuffix)
		if err := writeFileDurable(saved, old); err != nil {
			return "", fmt.Errorf("copie de l'état courant impossible : %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := writeFileDurable(dst, b); err != nil {
		return saved, err
	}
	return saved, nil
}
