package ml

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/image/draw"
)

type Service struct {
	visualSession *ort.DynamicAdvancedSession
	textSession   *ort.DynamicAdvancedSession
	vocab         map[string]int
	dataDir       string
}

func NewService(dataDir string, ortLibPath string) (*Service, error) {
	// Initialize ONNX Runtime
	if !ort.IsInitialized() {
		log.Printf("Initializing ONNX Runtime with library at: %s", ortLibPath)
		ort.SetSharedLibraryPath(ortLibPath)
		err := ort.InitializeEnvironment()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize ONNX runtime: %w", err)
		}
	}

	modelDir := filepath.Join(dataDir, "models")
	
	// Load Visual Model
	visualPath := filepath.Join(modelDir, "clip_visual.onnx")
	visualSession, err := ort.NewDynamicAdvancedSession(visualPath, 
		[]string{"input"}, []string{"output"}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load visual model: %w", err)
	}

	// Load Text Model
	textPath := filepath.Join(modelDir, "clip_text.onnx")
	textSession, err := ort.NewDynamicAdvancedSession(textPath, 
		[]string{"input"}, []string{"output"}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load text model: %w", err)
	}

	// Load Vocab
	vocabPath := filepath.Join(modelDir, "clip_vocab.json")
	vocabData, err := os.ReadFile(vocabPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read vocab: %w", err)
	}

	var vocab map[string]int
	if err := json.Unmarshal(vocabData, &vocab); err != nil {
		return nil, fmt.Errorf("failed to parse vocab: %w", err)
	}

	return &Service{
		visualSession: visualSession,
		textSession:   textSession,
		vocab:         vocab,
		dataDir:       dataDir,
	}, nil
}

func (s *Service) GetImageEmbedding(imagePath string) ([]float32, error) {
	img, err := s.loadImage(imagePath)
	if err != nil {
		return nil, err
	}

	// Preprocess
	inputData := s.preprocessImage(img)

	// Run Inference
	inputShape := ort.NewShape(1, 3, 224, 224)
	inputTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		return nil, err
	}
	defer inputTensor.Destroy()

	outputValues := make([]ort.Value, 1)

	err = s.visualSession.Run([]ort.Value{inputTensor}, outputValues)
	if err != nil {
		return nil, err
	}
	
	outputTensor := outputValues[0].(*ort.Tensor[float32])
	defer outputTensor.Destroy()

	// Normalize
	output := make([]float32, len(outputTensor.GetData()))
	copy(output, outputTensor.GetData())
	s.normalize(output)

	return output, nil
}

func (s *Service) GetTextEmbedding(text string) ([]float32, error) {
	tokens := s.tokenize(text)
	
	// Run Inference
	inputShape := ort.NewShape(1, 77)
	inputTensor, err := ort.NewTensor(inputShape, tokens)
	if err != nil {
		return nil, err
	}
	defer inputTensor.Destroy()

	outputValues := make([]ort.Value, 1)

	err = s.textSession.Run([]ort.Value{inputTensor}, outputValues)
	if err != nil {
		return nil, err
	}

	outputTensor := outputValues[0].(*ort.Tensor[float32])
	defer outputTensor.Destroy()

	// Normalize
	output := make([]float32, len(outputTensor.GetData()))
	copy(output, outputTensor.GetData())
	s.normalize(output)

	return output, nil
}

func (s *Service) loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func (s *Service) preprocessImage(img image.Image) []float32 {
	// 1. Resize to 224x224
	resized := image.NewRGBA(image.Rect(0, 0, 224, 224))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	// 2. Normalize
	// CLIP normalization: mean=[0.48145466, 0.4578275, 0.40821073], std=[0.26862954, 0.26130258, 0.27577711]
	mean := []float32{0.48145466, 0.4578275, 0.40821073}
	std := []float32{0.26862954, 0.26130258, 0.27577711}

	data := make([]float32, 3*224*224)
	for y := 0; y < 224; y++ {
		for x := 0; x < 224; x++ {
			r, g, b, _ := resized.At(x, y).RGBA()
			// RGBA returns values in [0, 65535]
			fr := float32(r) / 65535.0
			fg := float32(g) / 65535.0
			fb := float32(b) / 65535.0

			// NCHW format
			data[0*224*224 + y*224 + x] = (fr - mean[0]) / std[0]
			data[1*224*224 + y*224 + x] = (fg - mean[1]) / std[1]
			data[2*224*224 + y*224 + x] = (fb - mean[2]) / std[2]
		}
	}
	return data
}

func (s *Service) tokenize(text string) []int64 {
	// Very basic CLIP Tokenizer implementation
	// Real implementation needs BPE. For now, we'll do simple word splitting
	// to get the app running, but we should improve this.
	// CLIP start token: 49406, end token: 49407
	
	words := strings.Fields(strings.ToLower(text))
	tokens := make([]int64, 77)
	tokens[0] = 49406
	
	curr := 1
	for _, word := range words {
		if id, ok := s.vocab[word]; ok {
			tokens[curr] = int64(id)
			curr++
		} else if id, ok := s.vocab[word + "</w>"]; ok {
			tokens[curr] = int64(id)
			curr++
		}
		if curr >= 76 {
			break
		}
	}
	
	tokens[curr] = 49407
	return tokens
}

func (s *Service) normalize(v []float32) {
	var norm float32
	for _, x := range v {
		norm += x * x
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for i := range v {
			v[i] /= norm
		}
	}
}

func (s *Service) Close() {
	if s.visualSession != nil {
		s.visualSession.Destroy()
	}
	if s.textSession != nil {
		s.textSession.Destroy()
	}
	ort.DestroyEnvironment()
}
