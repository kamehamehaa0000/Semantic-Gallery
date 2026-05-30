# Local Semantic Gallery Architecture

This document outlines the High-Level Design (HLD) and Low-Level Design (LLD) for the Local Semantic Gallery application, aiming for a production-ready, distributable `.exe` setup with advanced local ML capabilities.

---

## 1. High-Level Design (HLD)

The application follows a Local Client-Server architecture, bundled into a desktop application wrapper.

### 1.1 Core Components

1.  **Presentation Layer (Frontend)**
    *   **Tech:** React, TypeScript, Vite.
    *   **Role:** User interface, rendering virtualized image grids, search inputs, settings management, and filtering/sorting controls.
    *   **Performance:** Uses virtual scrolling (`react-virtuoso` or similar) to render tens of thousands of thumbnails without DOM lag.

2.  **Application Controller (Go Backend / Wails)**
    *   **Tech:** Go, Wails Framework.
    *   **Role:** The core orchestrator. Manages OS-level interactions (file dialogs, opening files), manages the SQLite database, handles filesystem watching (`fsnotify`), and serves thumbnails to the frontend via Wails' local web server capabilities.
    *   **Lifecycle Management:** Starts and monitors the ML Inference Engine (if running as a separate local process).

3.  **ML Inference Engine (The "AI Backend")**
    *   **Current Tech:** Python, gRPC, PyTorch, OpenCLIP.
    *   **Proposed Target Tech:** 
        *   *Option A (Bundled):* PyInstaller-compiled Python executable communicating over gRPC/local HTTP or stdout/stdin.
        *   *Option B (Native):* Go ONNX Runtime (`onnxruntime-go`). This allows running the CLIP model entirely inside the Go process, removing the need for Python completely.
    *   **Role:** Generates embeddings for images and text queries. Can also run lightweight zero-shot classification for automatic categorization.

4.  **Data Storage Layer**
    *   **Relational DB:** SQLite (`modernc.org/sqlite`). Stores file paths, hashes, metadata (EXIF, resolution, size), and user-defined tags/categories.
    *   **Vector DB:** 
        *   *Current:* FAISS (requires manual index rebuilds).
        *   *Proposed Target:* `sqlite-vec` or `sqlite-vss` extension for SQLite, OR LanceDB. This allows ACID-compliant vector operations (Insert, Update, Delete) natively, without rebuilding entire arrays.

### 1.2 Deployment & Packaging Architecture
To deliver an `.exe` setup:
*   **Installer:** Inno Setup or NSIS.
*   **Payload:**
    1.  `Gallery.exe` (Wails compiled binary containing Go + React frontend).
    2.  `ML_Engine.exe` (PyInstaller compiled Python backend - *if Option A*).
    3.  Model weights (`.bin` or `.onnx` files) stored in the AppData/Local folder to keep the installer size reasonable (or downloaded on first launch).

---

## 2. Low-Level Design (LLD)

### 2.1 Database Schema (Proposed SQLite)

To support filtering, sorting, and categorization, the schema must evolve beyond just `path` and `hash`.

```sql
-- Core Image Metadata
CREATE TABLE images (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT UNIQUE,
    hash TEXT,
    folder_path TEXT,          -- For quick folder-based filtering
    filename TEXT,
    extension TEXT,
    file_size_bytes INTEGER,
    width INTEGER,
    height INTEGER,
    created_at INTEGER,        -- EXIF original date or filesystem creation
    last_modified INTEGER,
    is_favorite BOOLEAN DEFAULT 0
);

-- Semantic Vector Storage (using sqlite-vec extension)
CREATE VIRTUAL TABLE image_embeddings USING vec0(
    embedding float[512]
);

-- For Many-to-Many Tags/Categories
CREATE TABLE categories (
    id INTEGER PRIMARY KEY,
    name TEXT UNIQUE COLLATE NOCASE,
    is_auto_generated BOOLEAN  -- Distinguish user tags vs AI tags
);

CREATE TABLE image_categories (
    image_id INTEGER,
    category_id INTEGER,
    confidence FLOAT,          -- Useful if AI categorizes it (e.g., 0.95 confidence it's a "dog")
    PRIMARY KEY (image_id, category_id),
    FOREIGN KEY(image_id) REFERENCES images(id) ON DELETE CASCADE,
    FOREIGN KEY(category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE TABLE watched_folders (
    path TEXT PRIMARY KEY,
    last_scanned INTEGER       -- To support background reconciliation
);
```

### 2.2 File System Management Flow

Relying purely on `fsnotify` events can lead to race conditions or missed events when the app is closed.

1.  **Real-time Watcher (`fsnotify`):**
    *   Captures live `CREATE`, `REMOVE`, `RENAME`, `WRITE` (modified) events.
    *   **Debouncer:** Events are pushed to a channel and debounced (e.g., 500ms wait) to prevent hammering the DB/ML engine when 1000 files are dropped in at once.
2.  **Background Reconciliation (The "Catch-up" Sync):**
    *   Runs on application startup and periodically (e.g., every hour).
    *   Walks `watched_folders`.
    *   Compares actual filesystem state against the `images` table.
    *   Identifies files added/deleted *while the app was closed* and syncs them.
3.  **Rename/Move Tracking via Hashes:**
    *   When a file is "deleted", its hash is temporarily kept in a LRU cache.
    *   If a "create" event occurs shortly after with the same hash, the system registers it as a Move/Rename, preserving metadata, favorites, and user tags, rather than treating it as a brand new image.

### 2.3 ML Pipeline (Indexing & Searching)

**Indexing:**
1.  Go watcher detects a new file.
2.  Go extracts EXIF/basic metadata and stores it in SQLite.
3.  Go sends the file path to the ML Engine (via gRPC or CGO if ONNX).
4.  ML Engine resizes/pre-processes the image, runs it through the CLIP model.
5.  ML Engine returns the 512-dimensional float vector.
6.  Go stores the vector in the Vector DB (e.g., `sqlite-vec`).

**Searching (Semantic + Filters):**
1.  User types "dark snowy forest" and selects a filter (e.g., `width > 1920`).
2.  Go sends text to ML Engine.
3.  ML Engine returns the text embedding vector.
4.  Go queries the Vector DB using a Hybrid Search:
    ```sql
    SELECT i.path
    FROM image_embeddings e
    JOIN images i ON e.rowid = i.id
    WHERE vec_distance_cosine(e.embedding, ?) < 0.8
      AND i.width > 1920
    ORDER BY vec_distance_cosine(e.embedding, ?) ASC
    LIMIT 50;
    ```

### 2.4 Auto-Categorization

To implement categories (like Apple/Google Photos):
*   Maintain a predefined list of text queries (e.g., "screenshots", "memes", "documents", "nature", "animals", "people").
*   Pre-calculate the text embeddings for these categories once.
*   When a new image is indexed and its embedding is generated, calculate the dot product/cosine similarity against the predefined category embeddings.
*   If similarity > threshold (e.g., > 0.85), insert a record into the `image_categories` table.

---

## 3. Recommended Phased Implementation

**Phase 1: Stabilization & Packaging**
*   Replace FAISS with a dynamic vector DB (like `sqlite-vec` or Qdrant/LanceDB) to fix the $O(N)$ deletion bottleneck.
*   Bundle the Python server into an executable using PyInstaller.
*   Create an Inno Setup installer that drops both the Go app and the Python ML executable, managing them together.

**Phase 2: Metadata & UI Performance**
*   Implement `react-virtuoso` on the frontend for infinite scrolling.
*   Extend SQLite schema to parse and store EXIF data (width, height, date).
*   Add Filter sidebars to the UI.

**Phase 3: Native ML Integration (Optional but highly recommended)**
*   Port the ML inference to Go using `onnxruntime-go`.
*   Convert the PyTorch CLIP model to ONNX format.
*   This drops the Python dependency entirely, reducing memory footprint and making the `.exe` distribution trivial.
