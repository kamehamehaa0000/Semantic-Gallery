package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

type FileLoader struct {
	http.Handler
}

func NewFileLoader() *FileLoader {
	return &FileLoader{}
}

func (h *FileLoader) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	if strings.HasPrefix(path, "/local-file/") {
		filePath := strings.TrimPrefix(path, "/local-file/")
		
		// In wails dev, we might get paths like /C:/... or C:/...
		filePath = strings.TrimPrefix(filePath, "/")
		
		// If it's a Windows path like C:/... but coming in as C/..., fix it
		if len(filePath) > 1 && filePath[1] != ':' && (filePath[0] >= 'a' && filePath[0] <= 'z' || filePath[0] >= 'A' && filePath[0] <= 'Z') {
			// Check if it's followed by a slash, e.g., "C/Users" -> "C:/Users"
			if len(filePath) == 1 || filePath[1] == '/' {
				filePath = string(filePath[0]) + ":" + filePath[1:]
			}
		}
		
		filePath = filepath.FromSlash(filePath)
		
		// Basic security check to prevent directory traversal
		if strings.Contains(filePath, "..") {
			res.WriteHeader(http.StatusForbidden)
			return
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			// Try one more thing: sometimes drive letters are just C:/
			fmt.Printf("FileLoader Error: %v for path %s\n", err, filePath)
			res.WriteHeader(http.StatusNotFound)
			return
		}
		
		// Set content type based on file extension
		ext := strings.ToLower(filepath.Ext(filePath))
		switch ext {
		case ".jpg", ".jpeg":
			res.Header().Set("Content-Type", "image/jpeg")
		case ".png":
			res.Header().Set("Content-Type", "image/png")
		case ".gif":
			res.Header().Set("Content-Type", "image/gif")
		}
		
		res.Write(data)
		return
	}
	// For all other requests, do nothing and let Wails handle it via Assets
}

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Use fs.Sub to serve from frontend/dist as the root
	assetsFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		println("FATAL: Could not access embedded assets:", err.Error())
		return
	}

	// Create application with options
	err = wails.Run(&options.App{
		Title:  "Semantic Gallery",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets:  assetsFS,
			Handler: NewFileLoader(),
		},
		BackgroundColour: &options.RGBA{R: 15, G: 15, B: 20, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		// Enable debugging for dev builds
		Debug: options.Debug{
			OpenInspectorOnStartup: false,
		},
	})


	if err != nil {
		println("Error:", err.Error())
	}
}
