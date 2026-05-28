package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Image struct {
	ID           int64
	Path         string
	Hash         string
	LastModified int64
}

type Database struct {
	conn *sql.DB
}

func NewDatabase(dataDir string) (*Database, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "gallery.db")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	db := &Database{conn: conn}
	if err := db.init(); err != nil {
		return nil, err
	}

	return db, nil
}

func (db *Database) init() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS images (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT UNIQUE,
			hash TEXT,
			last_modified INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS watched_folders (
			path TEXT PRIMARY KEY
		)`,
	}

	for _, q := range queries {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func (db *Database) AddImage(path string, hash string, lastModified int64) (int64, error) {
	res, err := db.conn.Exec("INSERT OR REPLACE INTO images (path, hash, last_modified) VALUES (?, ?, ?)", path, hash, lastModified)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *Database) GetImageByPath(path string) (*Image, error) {
	var img Image
	err := db.conn.QueryRow("SELECT id, path, hash, last_modified FROM images WHERE path = ?", path).
		Scan(&img.ID, &img.Path, &img.Hash, &img.LastModified)
	if err != nil {
		return nil, err
	}
	return &img, nil
}

func (db *Database) GetImagesByIDs(ids []int64) ([]Image, error) {
	if len(ids) == 0 {
		return []Image{}, nil
	}

	// Simple way to handle variable number of IDs
	idList := ""
	for i, id := range ids {
		if i > 0 {
			idList += ","
		}
		idList += fmt.Sprintf("%d", id)
	}

	rows, err := db.conn.Query(fmt.Sprintf("SELECT id, path, hash, last_modified FROM images WHERE id IN (%s)", idList))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Map to keep order of input IDs
	idMap := make(map[int64]Image)
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.Path, &img.Hash, &img.LastModified); err != nil {
			return nil, err
		}
		idMap[img.ID] = img
	}

	results := make([]Image, 0, len(ids))
	for _, id := range ids {
		if img, ok := idMap[id]; ok {
			results = append(results, img)
		}
	}

	return results, nil
}

func (db *Database) GetAllImages(limit int) ([]Image, error) {
	rows, err := db.conn.Query("SELECT id, path, hash, last_modified FROM images ORDER BY last_modified DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []Image
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.Path, &img.Hash, &img.LastModified); err != nil {
			return nil, err
		}
		images = append(images, img)
	}
	return images, nil
}

func (db *Database) AddWatchedFolder(path string) error {
	_, err := db.conn.Exec("INSERT OR IGNORE INTO watched_folders (path) VALUES (?)", path)
	return err
}

func (db *Database) RemoveWatchedFolder(path string) error {
	_, err := db.conn.Exec("DELETE FROM watched_folders WHERE path = ?", path)
	return err
}

func (db *Database) DeleteImagesByFolder(folderPath string) error {
	// Normalize path
	folderPath = filepath.Clean(folderPath)
	
	// Delete the exact folder if it's in the images table (unlikely but safe)
	_, err := db.conn.Exec("DELETE FROM images WHERE path = ?", folderPath)
	if err != nil {
		return err
	}

	// Delete everything inside the folder using LIKE
	// We handle both \ and / just in case
	pattern1 := folderPath + string(os.PathSeparator) + "%"
	pattern2 := strings.ReplaceAll(folderPath, "\\", "/") + "/%"
	
	_, err = db.conn.Exec("DELETE FROM images WHERE path LIKE ? OR path LIKE ?", pattern1, pattern2)
	return err
}

func (db *Database) ClearAllData() error {
	_, err := db.conn.Exec("DELETE FROM images")
	if err != nil {
		return err
	}
	_, err = db.conn.Exec("DELETE FROM watched_folders")
	return err
}

func (db *Database) GetWatchedFolders() ([]string, error) {
	rows, err := db.conn.Query("SELECT path FROM watched_folders")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		folders = append(folders, path)
	}
	return folders, nil
}

func (db *Database) Close() error {
	return db.conn.Close()
}
