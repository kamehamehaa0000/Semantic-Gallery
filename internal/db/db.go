package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Image struct {
	ID           int64  `json:"id"`
	Path         string `json:"path"`
	Hash         string `json:"hash"`
	FolderPath   string `json:"folder_path"`
	Filename     string `json:"filename"`
	Extension    string `json:"extension"`
	FileSize     int64  `json:"file_size"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	CreatedAt    int64  `json:"created_at"`
	LastModified int64  `json:"last_modified"`
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
	// 1. Ensure core tables exist
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

	// 2. Migration: Add missing columns if they don't exist
	columnsToAdd := map[string]string{
		"folder_path":   "TEXT",
		"filename":      "TEXT",
		"extension":     "TEXT",
		"file_size":     "INTEGER",
		"width":         "INTEGER",
		"height":        "INTEGER",
		"created_at":    "INTEGER",
	}

	for col, colType := range columnsToAdd {
		if !db.columnExists("images", col) {
			log.Printf("Migration: Adding column %s to images table", col)
			query := fmt.Sprintf("ALTER TABLE images ADD COLUMN %s %s", col, colType)
			if _, err := db.conn.Exec(query); err != nil {
				return fmt.Errorf("failed to add column %s: %w", col, err)
			}
		}
	}

	// 3. Create indexes
	indexQueries := []string{
		`CREATE INDEX IF NOT EXISTS idx_images_hash ON images(hash)`,
		`CREATE INDEX IF NOT EXISTS idx_images_folder ON images(folder_path)`,
	}
	for _, q := range indexQueries {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}

	return nil
}

func (db *Database) columnExists(table, column string) bool {
	query := fmt.Sprintf("PRAGMA table_info(%s)", table)
	rows, err := db.conn.Query(query)
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dtype string
		var notnull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &dtype, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
}

func (db *Database) AddImage(img Image) (int64, error) {
	res, err := db.conn.Exec(`
		INSERT OR REPLACE INTO images (
			path, hash, folder_path, filename, extension, 
			file_size, width, height, created_at, last_modified
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		img.Path, img.Hash, img.FolderPath, img.Filename, img.Extension,
		img.FileSize, img.Width, img.Height, img.CreatedAt, img.LastModified,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *Database) GetImageByPath(path string) (*Image, error) {
	var img Image
	err := db.conn.QueryRow(`
		SELECT id, path, hash, 
		       COALESCE(folder_path, ''), COALESCE(filename, ''), COALESCE(extension, ''), 
		       COALESCE(file_size, 0), COALESCE(width, 0), COALESCE(height, 0), 
		       COALESCE(created_at, 0), COALESCE(last_modified, 0) 
		FROM images WHERE path = ?`, path).
		Scan(&img.ID, &img.Path, &img.Hash, &img.FolderPath, &img.Filename, &img.Extension,
			&img.FileSize, &img.Width, &img.Height, &img.CreatedAt, &img.LastModified)
	if err != nil {
		return nil, err
	}
	return &img, nil
}

func (db *Database) GetImageByHash(hash string) (*Image, error) {
	var img Image
	err := db.conn.QueryRow(`
		SELECT id, path, hash, 
		       COALESCE(folder_path, ''), COALESCE(filename, ''), COALESCE(extension, ''), 
		       COALESCE(file_size, 0), COALESCE(width, 0), COALESCE(height, 0), 
		       COALESCE(created_at, 0), COALESCE(last_modified, 0) 
		FROM images WHERE hash = ? LIMIT 1`, hash).
		Scan(&img.ID, &img.Path, &img.Hash, &img.FolderPath, &img.Filename, &img.Extension,
			&img.FileSize, &img.Width, &img.Height, &img.CreatedAt, &img.LastModified)
	if err != nil {
		return nil, err
	}
	return &img, nil
}

func (db *Database) GetImagesByIDs(ids []int64) ([]Image, error) {
	if len(ids) == 0 {
		return []Image{}, nil
	}

	idList := ""
	for i, id := range ids {
		if i > 0 {
			idList += ","
		}
		idList += fmt.Sprintf("%d", id)
	}

	rows, err := db.conn.Query(fmt.Sprintf(`
		SELECT id, path, hash, 
		       COALESCE(folder_path, ''), COALESCE(filename, ''), COALESCE(extension, ''), 
		       COALESCE(file_size, 0), COALESCE(width, 0), COALESCE(height, 0), 
		       COALESCE(created_at, 0), COALESCE(last_modified, 0) 
		FROM images WHERE id IN (%s)`, idList))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	idMap := make(map[int64]Image)
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.Path, &img.Hash, &img.FolderPath, &img.Filename, &img.Extension,
			&img.FileSize, &img.Width, &img.Height, &img.CreatedAt, &img.LastModified); err != nil {
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
	rows, err := db.conn.Query(`
		SELECT id, path, hash, 
		       COALESCE(folder_path, ''), COALESCE(filename, ''), COALESCE(extension, ''), 
		       COALESCE(file_size, 0), COALESCE(width, 0), COALESCE(height, 0), 
		       COALESCE(created_at, 0), COALESCE(last_modified, 0) 
		FROM images ORDER BY created_at DESC, last_modified DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []Image
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.ID, &img.Path, &img.Hash, &img.FolderPath, &img.Filename, &img.Extension,
			&img.FileSize, &img.Width, &img.Height, &img.CreatedAt, &img.LastModified); err != nil {
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

func (db *Database) DeletePath(path string) ([]int64, []string, error) {
	// Normalize path
	path = filepath.Clean(path)
	
	// Get IDs and Paths before deleting
	// We match the exact path OR paths that start with (path + separator)
	sep := string(os.PathSeparator)
	altSep := "/"
	if sep == "/" {
		altSep = "\\"
	}

	rows, err := db.conn.Query(`
		SELECT id, path FROM images 
		WHERE path = ? 
		OR (instr(path, ?) = 1 AND length(path) > length(?) AND substr(path, length(?) + 1, 1) = ?)
		OR (instr(path, ?) = 1 AND length(path) > length(?) AND substr(path, length(?) + 1, 1) = ?)`,
		path, 
		path, path, path, sep,
		path, path, path, altSep)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var ids []int64
	var paths []string
	for rows.Next() {
		var id int64
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		paths = append(paths, p)
	}

	// Delete from images table using the same logic
	_, err = db.conn.Exec(`
		DELETE FROM images 
		WHERE path = ? 
		OR (instr(path, ?) = 1 AND length(path) > length(?) AND substr(path, length(?) + 1, 1) = ?)
		OR (instr(path, ?) = 1 AND length(path) > length(?) AND substr(path, length(?) + 1, 1) = ?)`,
		path, 
		path, path, path, sep,
		path, path, path, altSep)
	if err != nil {
		return nil, nil, err
	}

	return ids, paths, nil
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
