# Local Semantic Gallery

A fully local semantic image gallery built with Go, Wails, React, and Python.

## Architecture
- **Go Backend:** Orchestrates file scanning, directory watching, SQLite metadata, and thumbnail generation.
- **Python Worker:** Semantic embedding generation (OpenCLIP ViT-B-32) and fast vector search (FAISS).
- **Frontend:** React + TypeScript UI for search and folder management.

## Prerequisites
- **Go** (1.20+)
- **Python** (3.8+)
- **Wails CLI** (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)

## How to Run

### 1. Start the Python ML Worker
In a separate terminal:
```bash
cd python
# (Optional) Create and activate a virtual environment
python -m pip install -r requirements.txt
python server.py
```
Wait for the server to log `Starting gRPC server on port 50051...`.

### 2. Run the Wails App
In another terminal:
```bash
wails dev
```
This will build the frontend, compile the Go backend, and launch the application window.

## Usage
1. Click **Add Folder** to select a directory containing images (JPG, PNG, GIF).
2. The app will recursively scan and index the images in the background.
3. Type a semantic query in the search bar (e.g., "mountain", "car", "dog playing") and press **Enter**.
4. The top 1000 matching images will be displayed instantly.

## Data Storage
- **Metadata:** SQLite database in `~/.local-semantic-gallery/gallery.db`
- **Vectors:** FAISS index in `~/.local-semantic-gallery/data/`
- **Thumbnails:** Cached WebP images in `~/.local-semantic-gallery/thumbnails/`
