import {useState, useEffect} from 'react';
import './App.css';
import {Search, AddFolder, GetWatchedFolders, GetImages, OpenImage, SelectFolder, RemoveFolder, Reindex, ClearAllData} from "../wailsjs/go/main/App";
import {EventsOn} from "../wailsjs/runtime";

interface ImageResult {
    id: number;
    path: string;
    hash: string;
    folder_path: string;
    filename: string;
    extension: string;
    file_size: number;
    width: number;
    height: number;
    created_at: number;
    last_modified: number;
    thumb_path: string;
}

function App() {
    const [view, setView] = useState<'gallery' | 'settings'>('gallery');
    const [query, setQuery] = useState('');
    const [results, setResults] = useState<ImageResult[]>([]);
    const [folders, setFolders] = useState<string[]>([]);
    const [loading, setLoading] = useState(false);
    const [indexing, setIndexing] = useState({ active: false, total: 0, current: 0 });

    const [backendReady, setBackendReady] = useState(false);

    useEffect(() => {
        console.log("Starting connection check...");
        const check = () => {
            const hasGo = !!(window as any).go;
            const hasMain = hasGo && !!(window as any).go.main;
            const hasApp = hasMain && !!(window as any).go.main.App;
            
            console.log("Connection state:", { hasGo, hasMain, hasApp });

            if (hasApp) {
                console.log("Backend connected!");
                setBackendReady(true);
                loadFolders();
                loadInitialImages();
                setupEvents();
                return true;
            }
            return false;
        };

        if (check()) return;

        const interval = setInterval(() => {
            if (check()) {
                clearInterval(interval);
            }
        }, 500);

        return () => clearInterval(interval);
    }, []);

    const setupEvents = () => {
        EventsOn("indexing_start", (count: number) => {
            setIndexing({ active: true, total: count, current: 0 });
        });
        EventsOn("indexing_progress", () => {
            setIndexing(prev => ({ ...prev, current: prev.current + 1 }));
        });
        EventsOn("indexing_end", () => {
            setIndexing({ active: false, total: 0, current: 0 });
            loadInitialImages();
        });
        EventsOn("images_updated", () => {
            console.log("Images updated event received, refreshing...");
            loadInitialImages();
        });
        EventsOn("folders_updated", () => {
            console.log("Folders updated event received, refreshing...");
            loadFolders();
        });
    };

    const loadFolders = () => {
        GetWatchedFolders().then(res => setFolders(res || [])).catch(err => console.error("Failed to load folders:", err));
    };

    const loadInitialImages = () => {
        GetImages(100).then(res => {
            setResults(res || []);
        }).catch(err => {
            console.error("Failed to load initial images:", err);
        });
    };

    const handleSearch = () => {
        if (!query.trim()) return;
        if (!backendReady) {
            alert("Application backend is still connecting. Please wait a moment.");
            return;
        }
        setLoading(true);
        Search(query, 1000).then(res => {
            setResults(res || []);
            setLoading(false);
        }).catch(err => {
            console.error("Search error:", err);
            setLoading(false);
        });
    };

    const handleAddFolder = () => {
        if (!backendReady) return;
        SelectFolder().then(path => {
            if (path) {
                AddFolder(path).then(res => {
                    if (res === "Success") {
                        loadFolders();
                    } else {
                        alert("Error: " + res);
                    }
                });
            }
        });
    };

    const handleRemoveFolder = (path: string) => {
        if (window.confirm(`Are you sure you want to remove ${path}?`)) {
            RemoveFolder(path).then(() => {
                loadFolders();
                loadInitialImages();
            });
        }
    };

    const handleReindex = () => {
        Reindex();
    };

    const handleClearData = () => {
        if (window.confirm("Are you sure you want to clear ALL indexed data and watched folders? This cannot be undone.")) {
            ClearAllData().then(res => {
                if (res === "Success") {
                    setFolders([]);
                    setResults([]);
                    alert("All data cleared successfully.");
                } else {
                    alert("Error: " + res);
                }
            });
        }
    };

    const handleRefresh = () => {
        loadInitialImages();
        loadFolders();
    };

    const handleOpenImage = (path: string) => {
        OpenImage(path);
    };

    const getLocalFileUrl = (path: string) => {
        // Normalize path for the custom handler
        const normalizedPath = path.replace(/\\/g, '/');
        // Use a relative path so Wails intercepts it correctly
        return `/local-file/${normalizedPath}`;
    };

    return (
        <div className="app-layout">
            <aside className="sidebar">
                <div className="sidebar-top">
                    <div className={`nav-item ${view === 'gallery' ? 'active' : ''}`} onClick={() => setView('gallery')} title="Gallery">
                        🖼️
                    </div>
                </div>
                <div className="sidebar-bottom">
                    <div className={`nav-item ${view === 'settings' ? 'active' : ''}`} onClick={() => setView('settings')} title="Settings">
                        ⚙️
                    </div>
                </div>
            </aside>

            <div className="main-container">
                <header className="header">
                    {view === 'gallery' ? (
                        <div className="search-container">
                            <input
                                type="text"
                                className="search-input"
                                placeholder="Search your photos..."
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
                            />
                            <button className="btn" onClick={handleSearch}>Search</button>
                            <button className="btn btn-secondary" onClick={handleRefresh} title="Refresh gallery">🔄</button>
                            <div className={`status-indicator ${backendReady ? 'ready' : 'waiting'}`}>
                                {backendReady ? '● Connected' : '○ Connecting...'}
                            </div>
                        </div>
                    ) : (
                        <div className="settings-header">
                            <h2>Settings</h2>
                            <div style={{display: 'flex', gap: '10px'}}>
                                <button className="btn" onClick={handleAddFolder}>Add Folder</button>
                                <button className="btn btn-secondary" onClick={handleReindex}>Update Index</button>
                                <button className="btn btn-danger" onClick={handleClearData}>Clear All Data</button>
                            </div>
                        </div>
                    )}
                    {indexing.active && (
                        <div className="indexing-bar">
                            <div className="indexing-progress" style={{ width: `${(indexing.current / indexing.total) * 100}%` }}></div>
                            <span className="indexing-text">Indexing {indexing.current} / {indexing.total} images...</span>
                        </div>
                    )}
                </header>

                <main className="main-content">
                    {view === 'gallery' ? (
                        loading ? (
                            <div className="empty-state">Searching...</div>
                        ) : results && results.length > 0 ? (
                            <div className="gallery-grid">
                                {results.map((img) => (
                                    <div key={img.id} className="image-card" title={`Click to open original: ${img.path}`} onClick={() => handleOpenImage(img.path)}>
                                        <img src={getLocalFileUrl(img.thumb_path)} alt="" loading="lazy" />
                                    </div>
                                ))}
                            </div>
                        ) : (
                            <div className="empty-state">
                                {query ? "No results found" : "Start by adding a folder in Settings!"}
                            </div>
                        )
                    ) : (
                        <div className="settings-content">
                            <h3>Watched Folders</h3>
                            <div className="folder-management-list">
                                {folders.map((f, i) => (
                                    <div key={i} className="folder-management-item">
                                        <span className="folder-path">📁 {f}</span>
                                        <button className="btn-icon" onClick={() => handleRemoveFolder(f)} title="Remove folder">🗑️</button>
                                    </div>
                                ))}
                                {folders.length === 0 && <p className="empty-text">No folders added yet.</p>}
                            </div>
                        </div>
                    )}
                </main>
            </div>
        </div>
    );
}

export default App;
