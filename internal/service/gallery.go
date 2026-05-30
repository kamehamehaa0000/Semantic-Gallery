package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/gallery/internal/db"
	"github.com/gallery/internal/proto/pb"
	"github.com/gallery/internal/thumbnail"
)

type GalleryService struct {
	db             *db.Database
	thumbs         *thumbnail.Service
	grpcClient     pb.GalleryServiceClient
	watcher        *fsnotify.Watcher
	indexingCh     chan string
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	wailsCtx       context.Context
	watchedFolders map[string]bool
	mu             sync.Mutex
}

func NewGalleryService(dataDir string, grpcAddr string) (*GalleryService, error) {
	database, err := db.NewDatabase(dataDir)
	if err != nil {
		return nil, err
	}

	thumbs, err := thumbnail.NewService(dataDir)
	if err != nil {
		return nil, err
	}

	// Dial with a context and timeout
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	
	conn, err := grpc.DialContext(dialCtx, grpcAddr, 
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock()) // Wait for connection to be ready
	if err != nil {
		// If it fails, we still want the app to start, so we'll dial in background
		log.Printf("Warning: Initial gRPC connection failed: %v. Will retry in background.", err)
		conn, _ = grpc.Dial(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	
	client := pb.NewGalleryServiceClient(conn)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	gs := &GalleryService{
		db:             database,
		thumbs:         thumbs,
		grpcClient:     client,
		watcher:        watcher,
		indexingCh:     make(chan string, 1000),
		ctx:            ctx,
		cancel:         cancel,
		watchedFolders: make(map[string]bool),
	}

	gs.startWorker()
	gs.startWatcher()

	// Run reconciliation in background
	go gs.Reconcile()

	return gs, nil
}

func (s *GalleryService) Reconcile() {
	log.Println("Starting background reconciliation...")
	
	folders, err := s.db.GetWatchedFolders()
	if err != nil {
		log.Printf("Reconcile: Error getting watched folders: %v", err)
		return
	}

	// 1. Scan for new or changed files
	for _, folder := range folders {
		s.ScanFolder(folder)
	}

	// 2. Clean up orphaned entries (files that were deleted while app was closed)
	// We'll do this in chunks to avoid locking the DB for too long
	s.cleanupOrphanedEntries()
	
	log.Println("Reconciliation complete.")
}

func (s *GalleryService) cleanupOrphanedEntries() {
	// Simple approach: Get all paths from DB and check existence
	// In a massive gallery, we'd do this more efficiently with sub-queries
	imgs, err := s.db.GetAllImages(1000000) // Get all
	if err != nil {
		return
	}

	for _, img := range imgs {
		if _, err := os.Stat(img.Path); os.IsNotExist(err) {
			log.Printf("Reconcile: Cleaning up orphaned entry: %s", img.Path)
			s.handleDeletion(img.Path)
		}
	}
}

func (s *GalleryService) SetContext(ctx context.Context) {
	s.wailsCtx = ctx
}

func (s *GalleryService) startWorker() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		batch := make([]string, 0, 10)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case path := <-s.indexingCh:
				batch = append(batch, path)
				if len(batch) >= 10 {
					s.processBatch(batch)
					batch = batch[:0]
				}
			case <-ticker.C:
				if len(batch) > 0 {
					s.processBatch(batch)
					batch = batch[:0]
				}
			case <-s.ctx.Done():
				if len(batch) > 0 {
					s.processBatch(batch)
				}
				return
			}
		}
	}()
}

func (s *GalleryService) processBatch(paths []string) {
	if s.wailsCtx != nil {
		runtime.EventsEmit(s.wailsCtx, "indexing_start", len(paths))
	}

	entries := make([]*pb.ImageEntry, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		hash, _ := computeHash(path)
		
		// Move/Rename detection: Check if we already have this hash
		existing, _ := s.db.GetImageByHash(hash)
		if existing != nil && existing.Path != path {
			log.Printf("Rename/Move detected: %s -> %s", existing.Path, path)
			// We can delete the old path from the index if we want, but usually 
			// the watcher will have already triggered a deletion for the old path.
			// The important part is that we reuse the metadata if possible, 
			// or just overwrite with new path.
		}

		meta := s.extractMetadata(path, info, hash)
		id, err := s.db.AddImage(meta)
		if err != nil {
			log.Printf("DB error for %s: %v", path, err)
			continue
		}

		entries = append(entries, &pb.ImageEntry{
			Id:   id,
			Path: path,
		})

		// Pre-generate thumbnail
		s.thumbs.EnsureThumbnail(path)

		if s.wailsCtx != nil {
			runtime.EventsEmit(s.wailsCtx, "indexing_progress", path)
		}
	}

	if len(entries) > 0 {
		_, err := s.grpcClient.IndexImages(s.ctx, &pb.IndexRequest{Entries: entries})
		if err != nil {
			log.Printf("gRPC Index error: %v", err)
		}
	}

	if s.wailsCtx != nil {
		runtime.EventsEmit(s.wailsCtx, "indexing_end")
	}
}

func (s *GalleryService) extractMetadata(path string, info os.FileInfo, hash string) db.Image {
	img := db.Image{
		Path:         path,
		Hash:         hash,
		FolderPath:   filepath.Dir(path),
		Filename:     info.Name(),
		Extension:    strings.ToLower(filepath.Ext(path)),
		FileSize:     info.Size(),
		LastModified: info.ModTime().Unix(),
		CreatedAt:    info.ModTime().Unix(), // Default to modTime
	}

	// Try to get EXIF
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		
		// Extract dimensions
		conf, _, err := image.DecodeConfig(f)
		if err == nil {
			img.Width = conf.Width
			img.Height = conf.Height
		}

		// Reset file pointer for EXIF
		f.Seek(0, 0)
		x, err := exif.Decode(f)
		if err == nil {
			tm, err := x.DateTime()
			if err == nil {
				img.CreatedAt = tm.Unix()
			}
		}
	}

	return img
}

func computeHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *GalleryService) startWatcher() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case event, ok := <-s.watcher.Events:
				if !ok {
					return
				}

				log.Printf("Watcher: %s on %s", event.Op, event.Name)

				// Handle removals/moves-out
				if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					// Check if it really doesn't exist anymore
					_, err := os.Stat(event.Name)
					if os.IsNotExist(err) {
						log.Printf("Watcher: File/Dir removed or moved out: %s", event.Name)
						s.handleDeletion(event.Name)

						// Check if it was a root watched folder
						folders, _ := s.db.GetWatchedFolders()
						for _, f := range folders {
							if f == event.Name {
								log.Printf("Watcher: Root watched folder removed: %s", event.Name)
								s.db.RemoveWatchedFolder(f)
								if s.wailsCtx != nil {
									runtime.EventsEmit(s.wailsCtx, "folders_updated")
								}
								break
							}
						}
					}
				}

				// Handle new files and directories or updates
				if event.Op&(fsnotify.Create|fsnotify.Rename|fsnotify.Write) != 0 {
					info, err := os.Stat(event.Name)
					if err == nil {
						if info.IsDir() {
							if event.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
								log.Printf("Watcher: New directory detected: %s", event.Name)
								s.addWatch(event.Name)
								go s.ScanFolder(event.Name)
							}
						} else if isImage(event.Name) {
							log.Printf("Watcher: Image added/updated: %s", event.Name)
							s.indexingCh <- event.Name
						}
					}
				}

			case err, ok := <-s.watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Watcher error: %v", err)
			case <-s.ctx.Done():
				return
			}
		}
	}()

	// Load existing watched folders
	folders, _ := s.db.GetWatchedFolders()
	for _, f := range folders {
		s.addWatch(f)
	}
}

func (s *GalleryService) addWatch(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.watchedFolders[path] {
		s.watcher.Add(path)
		s.watchedFolders[path] = true
	}
}

func (s *GalleryService) removeWatch(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Remove from watcher and map
	// We also need to remove all subdirectories from the map and watcher
	sep := string(os.PathSeparator)
	for p := range s.watchedFolders {
		if p == path || strings.HasPrefix(p, path+sep) || (sep == "\\" && strings.HasPrefix(p, strings.ReplaceAll(path, "\\", "/")+ "/")) {
			s.watcher.Remove(p)
			delete(s.watchedFolders, p)
		}
	}
}

func (s *GalleryService) handleDeletion(path string) {
	ids, paths, err := s.db.DeletePath(path)
	if err != nil {
		log.Printf("Error deleting path %s from DB: %v", path, err)
		return
	}

	// Remove from gRPC index
	if len(ids) > 0 {
		_, err := s.grpcClient.DeleteImages(s.ctx, &pb.DeleteRequest{Ids: ids})
		if err != nil {
			log.Printf("gRPC Delete error: %v", err)
		}
	}

	// Remove thumbnails
	for _, p := range paths {
		s.thumbs.RemoveThumbnail(p)
	}

	if s.wailsCtx != nil {
		runtime.EventsEmit(s.wailsCtx, "images_updated")
	}

	// Clean up watches recursively
	s.removeWatch(path)
}

func isImage(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif"
}

func (s *GalleryService) AddFolder(path string) error {
	if err := s.db.AddWatchedFolder(path); err != nil {
		return err
	}
	s.addWatch(path)

	// Initial scan
	go s.ScanFolder(path)

	return nil
}

func (s *GalleryService) RemoveFolder(path string) error {
	if err := s.db.RemoveWatchedFolder(path); err != nil {
		return err
	}
	// Also delete images belonging to this folder from DB and gRPC
	s.handleDeletion(path)
	
	return nil
}

func (s *GalleryService) ScanFolder(path string) {
	filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && isImage(p) {
			s.indexingCh <- p
		} else if info.IsDir() && p != path {
			// Add subfolders to watcher too
			s.addWatch(p)
		}
		return nil
	})
}

func (s *GalleryService) Search(query string, limit int) ([]db.Image, error) {
	resp, err := s.grpcClient.Search(s.ctx, &pb.SearchRequest{
		Query: query,
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}

	return s.db.GetImagesByIDs(resp.Ids)
}

func (s *GalleryService) GetWatchedFolders() ([]string, error) {
	return s.db.GetWatchedFolders()
}

func (s *GalleryService) GetAllImages(limit int) ([]db.Image, error) {
	return s.db.GetAllImages(limit)
}

func (s *GalleryService) ClearAllData() error {
	// Clear DB
	if err := s.db.ClearAllData(); err != nil {
		return err
	}
	
	// Clear thumbnails
	thumbs, _ := filepath.Glob(filepath.Join(s.thumbs.GetCacheDir(), "*"))
	for _, f := range thumbs {
		os.Remove(f)
	}
	
	// Reset watcher
	s.watcher.Close()
	
	s.mu.Lock()
	s.watchedFolders = make(map[string]bool)
	s.mu.Unlock()

	w, _ := fsnotify.NewWatcher()
	s.watcher = w
	s.startWatcher()
	
	return nil
}

func (s *GalleryService) GetThumbnail(imagePath string) (string, error) {
	return s.thumbs.EnsureThumbnail(imagePath)
}

func (s *GalleryService) Close() {
	s.cancel()
	s.wg.Wait()
	s.db.Close()
	s.watcher.Close()
}
