package thumbnail

import (
	"crypto/md5"
	"encoding/hex"
	"image"
	"image/jpeg"
	_ "image/gif"
	_ "image/png"
	"os"
	"path/filepath"

	"github.com/nfnt/resize"
)

type Service struct {
	cacheDir string
}

func NewService(dataDir string) (*Service, error) {
	cacheDir := filepath.Join(dataDir, "thumbnails")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}
	return &Service{cacheDir: cacheDir}, nil
}

func (s *Service) GetThumbnailPath(imagePath string) string {
	hash := md5.Sum([]byte(imagePath))
	fileName := hex.EncodeToString(hash[:]) + ".jpg"
	return filepath.Join(s.cacheDir, fileName)
}

func (s *Service) GetCacheDir() string {
	return s.cacheDir
}

func (s *Service) RemoveThumbnail(imagePath string) error {
	thumbPath := s.GetThumbnailPath(imagePath)
	if _, err := os.Stat(thumbPath); err == nil {
		return os.Remove(thumbPath)
	}
	return nil
}

func (s *Service) EnsureThumbnail(imagePath string) (string, error) {
	thumbPath := s.GetThumbnailPath(imagePath)
	if _, err := os.Stat(thumbPath); err == nil {
		return thumbPath, nil
	}

	// Generate thumbnail
	file, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return "", err
	}

	// Resize to max 300x300 while maintaining aspect ratio
	thumb := resize.Thumbnail(300, 300, img, resize.Lanczos3)

	out, err := os.Create(thumbPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if err := jpeg.Encode(out, thumb, &jpeg.Options{Quality: 75}); err != nil {
		return "", err
	}

	return thumbPath, nil
}
