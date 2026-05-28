package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/gallery/internal/service"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type ImageResult struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	ThumbPath string `json:"thumb_path"`
}

// App struct
type App struct {
	ctx     context.Context
	gallery *service.GalleryService
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	
	userHome, _ := os.UserHomeDir()
	dataDir := filepath.Join(userHome, ".local-semantic-gallery")
	
	gs, err := service.NewGalleryService(dataDir, "localhost:50051")
	if err != nil {
		fmt.Printf("FATAL: Error starting gallery service: %v\n", err)
		fmt.Printf("Make sure the Python gRPC server is running on localhost:50051\n")
		fmt.Printf("Run: cd python && python server.py\n")
		return
	}
	gs.SetContext(ctx)
	a.gallery = gs
	fmt.Printf("Gallery service started successfully\n")
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	if a.gallery != nil {
		a.gallery.Close()
	}
}

// Search for images
func (a *App) Search(query string, limit int) []ImageResult {
	if a.gallery == nil {
		return nil
	}
	imgs, err := a.gallery.Search(query, limit)
	if err != nil {
		fmt.Printf("Search error: %v\n", err)
		return nil
	}

	results := make([]ImageResult, 0, len(imgs))
	for _, img := range imgs {
		thumb, _ := a.gallery.GetThumbnail(img.Path)
		results = append(results, ImageResult{
			ID:        img.ID,
			Path:      img.Path,
			ThumbPath: thumb,
		})
	}
	return results
}

// AddFolder adds a new folder to scan
func (a *App) AddFolder(path string) string {
	if a.gallery == nil {
		return "Service not initialized"
	}
	err := a.gallery.AddFolder(path)
	if err != nil {
		return err.Error()
	}
	return "Success"
}

// SelectFolder opens a directory dialog and returns the selected path
func (a *App) SelectFolder() string {
	path, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select Folder to Add",
	})
	if err != nil {
		return ""
	}
	return path
}

// RemoveFolder removes a folder from watched folders
func (a *App) RemoveFolder(path string) string {
	if a.gallery == nil {
		return "Service not initialized"
	}
	err := a.gallery.RemoveFolder(path)
	if err != nil {
		return err.Error()
	}
	return "Success"
}

// Reindex triggers a scan for all watched folders
func (a *App) Reindex() {
	if a.gallery == nil {
		return
	}
	folders, _ := a.gallery.GetWatchedFolders()
	for _, f := range folders {
		go a.gallery.ScanFolder(f)
	}
}

// ClearAllData wipes all indexed images and folders
func (a *App) ClearAllData() string {
	if a.gallery == nil {
		return "Service not initialized"
	}
	err := a.gallery.ClearAllData()
	if err != nil {
		return err.Error()
	}
	return "Success"
}

// GetWatchedFolders returns the list of watched folders
func (a *App) GetWatchedFolders() []string {
	if a.gallery == nil {
		return nil
	}
	folders, _ := a.gallery.GetWatchedFolders()
	return folders
}

// OpenImage opens the image in the system default viewer
func (a *App) OpenImage(path string) {
	if runtime.GOOS == "windows" {
		// Use explorer.exe to open the file with the default associated application
		exec.Command("explorer", path).Run()
	} else {
		wailsruntime.BrowserOpenURL(a.ctx, path)
	}
}

// GetImages returns all indexed images
func (a *App) GetImages(limit int) []ImageResult {
	if a.gallery == nil {
		return nil
	}
	imgs, err := a.gallery.GetAllImages(limit)
	if err != nil {
		fmt.Printf("GetImages error: %v\n", err)
		return nil
	}

	results := make([]ImageResult, 0, len(imgs))
	for _, img := range imgs {
		thumb, _ := a.gallery.GetThumbnail(img.Path)
		results = append(results, ImageResult{
			ID:        img.ID,
			Path:      img.Path,
			ThumbPath: thumb,
		})
	}
	return results
}
