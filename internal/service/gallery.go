package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/gallery/internal/db"
	"github.com/gallery/internal/proto/pb"
	"github.com/gallery/internal/thumbnail"
)

type GalleryService struct {
	db         *db.Database
	thumbs     *thumbnail.Service
	grpcClient pb.GalleryServiceClient
	watcher    *fsnotify.Watcher
	indexingCh chan string
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	wailsCtx   context.Context
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

	// Dial without blocking so the app starts immediately
	// gRPC will handle connection/reconnection in the background
	conn, err := grpc.Dial(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client for %s: %w", grpcAddr, err)
	}
	client := pb.NewGalleryServiceClient(conn)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	gs := &GalleryService{
		db:         database,
		thumbs:     thumbs,
		grpcClient: client,
		watcher:    watcher,
		indexingCh: make(chan string, 1000),
		ctx:        ctx,
		cancel:     cancel,
	}

	gs.startWorker()
	gs.startWatcher()

	return gs, nil
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
		id, err := s.db.AddImage(path, hash, info.ModTime().Unix())
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
				
				// Handle new files and directories
				// Create: new file/folder
				// Rename: file/folder moved into a watched path
				if event.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
					info, err := os.Stat(event.Name)
					if err == nil {
						if info.IsDir() {
							// New directory created or moved in, watch it and scan it
							log.Printf("Watcher: New directory detected: %s", event.Name)
							s.watcher.Add(event.Name)
							go s.ScanFolder(event.Name)
						} else if isImage(event.Name) {
							log.Printf("Watcher: New image detected: %s", event.Name)
							s.indexingCh <- event.Name
						}
					}
				} else if event.Op&fsnotify.Write == fsnotify.Write {
					// Sometimes files are created empty then written to
					if isImage(event.Name) {
						s.indexingCh <- event.Name
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
		s.watcher.Add(f)
	}
}

func isImage(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif"
}

func (s *GalleryService) AddFolder(path string) error {
	if err := s.db.AddWatchedFolder(path); err != nil {
		return err
	}
	s.watcher.Add(path)

	// Initial scan
	go s.ScanFolder(path)

	return nil
}

func (s *GalleryService) RemoveFolder(path string) error {
	if err := s.db.RemoveWatchedFolder(path); err != nil {
		return err
	}
	// Also delete images belonging to this folder from DB
	if err := s.db.DeleteImagesByFolder(path); err != nil {
		log.Printf("Error deleting images for folder %s: %v", path, err)
	}
	s.watcher.Remove(path)
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
			s.watcher.Add(p)
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
