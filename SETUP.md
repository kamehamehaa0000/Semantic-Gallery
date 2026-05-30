# Setup Instructions for Gallery (Native ML Branch)

This project uses Go, Wails (for the frontend), and ONNX Runtime for native machine learning capabilities.

## Prerequisites

1.  **Go**: Install Go (1.21 or later recommended) from [go.dev](https://go.dev/doc/install).
2.  **Node.js**: Install Node.js and NPM from [nodejs.org](https://nodejs.org/).
3.  **Wails**: Install the Wails CLI:
    ```bash
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
    ```
4.  **C++ Build Tools** (Windows only): You may need the Visual Studio Build Tools (C++) for Wails.

## ONNX Runtime Dependency

The project relies on the ONNX Runtime shared library.

-   **Windows**: The `onnxruntime.dll` file is included in the project root. It should be automatically picked up when running the application.
-   **Linux/macOS**: You will need to download the appropriate `libonnxruntime.so` or `libonnxruntime.dylib` and ensure it's in your library search path (e.g., `LD_LIBRARY_PATH` or `/usr/local/lib`).

## Running the Project

1.  **Clone the repository** and switch to the `native-ml` branch.
2.  **Download Models**: Ensure the ONNX models are present in `data/models/`:
    - `clip_visual.onnx`
    - `clip_text.onnx`
    - `clip_vocab.json`
3.  **Run in Development Mode**:
    ```bash
    wails dev
    ```
4.  **Build the Production App**:
    ```bash
    wails build
    ```

## Project Structure

- `main.go` & `app.go`: Entry points for the Wails application.
- `internal/ml/`: Go implementation of CLIP (image and text embedding).
- `internal/db/`: Database management for image metadata and embeddings.
- `frontend/`: React + TypeScript frontend.
- `data/models/`: Contains the ML models.
