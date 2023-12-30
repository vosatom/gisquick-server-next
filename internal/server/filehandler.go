package server

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func findObjectByID(array []domain.StorageProvider, id string) *domain.StorageProvider {
	for _, obj := range array {
		if obj.ID == id {
			return &obj
		}
	}
	return nil
}

type FileHandler interface {
	SaveImage(io.Reader, int64, string) (MediaFileResult, error)

	LoadSourceImage(src string) (image.Image, error)

	CheckValidSource(parsedUrl url.URL) bool
	HasRemoteSource() bool
	GetExistingThumbnail(filePath string) string
	SaveThumbnail(img image.Image, filePath string, quality int) (string, error)
}

func ProcessPath(directory string, file *multipart.FileHeader) (string, error) {
	pattern, err := ProcessFileName(file)
	if err != nil {
		return "", err
	}

	parsedURL, err := url.Parse(pattern)
	if err != nil {
		return "", fmt.Errorf("error parsing URL: %w", err)
	}
	fileName := filepath.Clean(parsedURL.Path)
	return filepath.Join(directory, fileName), nil
}

// Based on internal/infrastructure/project/disk_storage.go
func ProcessFileName(file *multipart.FileHeader) (string, error) {
	pattern := file.Filename

	if strings.Contains(pattern, "<timestamp>") {
		pattern = strings.Replace(pattern, "<timestamp>", fmt.Sprint(time.Now().Unix()), 1)
	}

	if strings.Contains(pattern, "<random>") {
		// https://github.com/joncrlsn/fileutil/blob/5aac37a6ac963fd712b618b024d2eb14aa190958/fileutil.go#L98-L103
		randBytes := make([]byte, 16)
		rand.Read(randBytes)
		pattern = strings.Replace(pattern, "<random>", hex.EncodeToString(randBytes), 1)
	}

	if strings.Contains(pattern, "<hash>") {
		src, err := file.Open()
		if err != nil {
			return "", err
		}
		defer src.Close()

		h := sha1.New()
		if _, err := io.Copy(h, src); err != nil {
			return "", err
		}

		hash := fmt.Sprintf("%x", h.Sum(nil))
		pattern = strings.Replace(pattern, "<hash>", hash, 1)
	}
	return pattern, nil
}

func GetFileHandler(providersConfig []domain.StorageProvider, providerId string, projectPath string, thumbnailsPath string) (FileHandler, error) {
	provider := findObjectByID(providersConfig, providerId)
	if provider == nil {
		provider = &domain.StorageProvider{
			ID:   "local",
			Type: "local",
		}
	}

	if provider.Type == "s3" {
		parsedStoreUrl, _ := url.Parse(provider.StoreUrl)
		client, err := minio.New(parsedStoreUrl.Host, &minio.Options{
			Creds:  credentials.NewStaticV4(provider.AccessKey, provider.SecretKey, ""),
			Secure: parsedStoreUrl.Scheme == "https",
		})

		if err != nil {
			return nil, fmt.Errorf("minio: %w", err)
		}

		return S3FileHandler{*provider, *parsedStoreUrl, client}, nil
	}

	return LocalFileHandler{*provider, projectPath, thumbnailsPath}, nil
}

type MediaFileResult struct {
	domain.ProjectFile
	Filename string `json:"filename"`
}

type S3FileHandler struct {
	Provider domain.StorageProvider
	StoreUrl url.URL
	client   *minio.Client
}

func (handler S3FileHandler) calculateEtag(file io.Reader) (string, error) {
	h := md5.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (handler S3FileHandler) SaveImage(file io.Reader, fileSize int64, filePath string) (MediaFileResult, error) {
	ctx := context.Background()
	newPath := filePath

	var buf bytes.Buffer
	tee := io.TeeReader(file, &buf)
	hash, err := handler.calculateEtag(tee)
	if err != nil {
		return MediaFileResult{}, err
	}

	miniStat, err := handler.client.StatObject(ctx, handler.Provider.Bucket, newPath, minio.StatObjectOptions{})
	if err == nil {
		// If there is file with the same hash, add hash to the filename
		if hash != miniStat.ETag {
			extension := filepath.Ext(newPath)
			newPath = fmt.Sprintf("%s_%s%s", strings.TrimSuffix(newPath, extension), hash, extension)
		}
	}

	miniInfo, err := handler.client.PutObject(ctx, handler.Provider.Bucket, newPath, &buf, fileSize, minio.PutObjectOptions{})
	if err != nil {
		return MediaFileResult{}, err
	}

	newFilePath := filepath.Join(handler.Provider.Bucket, miniInfo.Key)
	fileName := filepath.Base(newFilePath)
	return MediaFileResult{domain.ProjectFile{Path: newFilePath, Size: fileSize, Hash: hash, Mtime: miniInfo.LastModified.Unix()}, fileName}, nil
}

func (handler S3FileHandler) CheckValidSource(parsedUrl url.URL) bool {
	return true
}

func (handler S3FileHandler) HasRemoteSource() bool {
	return true
}

func (handler S3FileHandler) GetThumbnailPath(filePath string) string {
	return filepath.Join(filepath.Dir(filePath), "thumbs", filepath.Base(filePath))
}

func (handler S3FileHandler) LoadSourceImage(filePath string) (image.Image, error) {
	sourceUrl := handler.StoreUrl
	// TODO: this can be removed when storeUrl for loading is the same as storeUrl for uploads
	if !strings.Contains(filePath, "/web/") {
		sourceUrl.Host = "cyklomapa-media.s3-eu-west-1.amazonaws.com"
	}
	sourceUrl.Path = filePath

	var client = http.Client{}
	res, err := client.Get(sourceUrl.String())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	img, err := imaging.Decode(res.Body, imaging.AutoOrientation(true))
	if err != nil {
		return nil, err
	}
	return img, err
}

func (handler S3FileHandler) GetExistingThumbnail(filePath string) string {
	newSource := handler.StoreUrl
	newSource.Path = filepath.Join(handler.Provider.Bucket, handler.GetThumbnailPath(filePath))

	res, err := http.Head(newSource.String())
	if err != nil || res.StatusCode != 200 {
		return ""
	}

	return newSource.String()
}

func (handler S3FileHandler) SaveThumbnail(img image.Image, filePath string, quality int) (string, error) {
	fileName := filepath.Base(filePath)
	format, err := imaging.FormatFromFilename(fileName)
	if err != nil {
		format = imaging.JPEG
	}

	if err != nil {
		return "", err
	}
	ctx := context.Background()

	f := new(bytes.Buffer)
	err = imaging.Encode(f, img, format, imaging.JPEGQuality(quality))
	if err != nil {
		return "", err
	}

	thumbnailFilePath := handler.GetThumbnailPath(filePath)
	fileSize := int64(f.Len())
	miniinfo, err := handler.client.PutObject(ctx, handler.Provider.Bucket, thumbnailFilePath, f, fileSize, minio.PutObjectOptions{})
	if err != nil {
		return "", err
	}

	newSource := handler.StoreUrl
	newSource.Path = filepath.Join(miniinfo.Bucket, miniinfo.Key)

	fmt.Println(newSource)

	return newSource.String(), nil
}

type LocalFileHandler struct {
	Provider       domain.StorageProvider
	ProjectPath    string
	ThumbnailsPath string
}

func (handler LocalFileHandler) SaveImage(file io.Reader, fileSize int64, filePath string) (MediaFileResult, error) {
	return MediaFileResult{}, nil
}

func (handler LocalFileHandler) CheckValidSource(parsedUrl url.URL) bool {
	folder := filepath.Dir(parsedUrl.Path)

	if folder != "web" && !strings.HasPrefix(folder, "web/") {
		return false
	}

	return true
}

func (handler LocalFileHandler) HasRemoteSource() bool {
	return false
}

func (handler LocalFileHandler) GetExistingThumbnail(filePath string) string {
	sourceAbsPath := filepath.Join(handler.ProjectPath, filePath)
	thumbAbsPath := filepath.Join(handler.ThumbnailsPath, filePath)
	srcFinfo, err := os.Stat(sourceAbsPath)
	if err != nil {
		return ""
	}

	finfo, err := os.Stat(thumbAbsPath)
	if err == nil {
		if finfo.ModTime().Unix() > srcFinfo.ModTime().Unix() {
			// valid thumbnail image
			return thumbAbsPath
		}
	}
	return ""
}

func (handler LocalFileHandler) LoadSourceImage(src string) (image.Image, error) {
	sourceAbsPath := filepath.Join(handler.ProjectPath, src)
	img, err := imaging.Open(sourceAbsPath, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("reading media image file: %w", err)
	}
	return img, err
}

func (handler LocalFileHandler) SaveThumbnail(img image.Image, filePath string, quality int) (string, error) {
	thumbAbsPath := filepath.Join(handler.ThumbnailsPath, filePath)
	err := os.MkdirAll(filepath.Dir(thumbAbsPath), 0777)
	if err != nil {
		return "", err
	}
	fileName := filepath.Base(thumbAbsPath)
	format, err := imaging.FormatFromFilename(fileName)
	if err != nil {
		format = imaging.JPEG
	}

	f, err := os.Create(thumbAbsPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	err = imaging.Encode(f, img, format, imaging.JPEGQuality(quality))
	return thumbAbsPath, err
}
