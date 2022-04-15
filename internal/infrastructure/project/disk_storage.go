package project

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gisquick/gisquick-server/internal/domain"
	"go.uber.org/zap"
)

type DiskStorage struct {
	ProjectsRoot string
	log          *zap.SugaredLogger
	// cache        *ttlcache.Cache
}

type Info struct {
	Title       string `json:"title"`
	File        string `json:"file"`
	ProjectHash string `json:"project_hash"`
	Projection  string `json:"projection"`
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

// func (s *DiskStorage) GetProject(name string) (*domain.Project, error) {
// 	loadFn := func(key string) (interface{}, time.Duration, error) {
// 		proj, err := s.loadProjectData(key)
// 		return proj, 0, err
// 	}
// 	p, err := s.cache.GetByLoader(project, loadFn)
// 	return p.(*domain.Project), err
// }

func DBHash(path string) (string, error) {
	cmdOut, err := exec.Command("dbhash", path).Output()
	if err != nil {
		return "", fmt.Errorf("executing dbhash command: %w", err)
	}
	hash := strings.Split(string(cmdOut), " ")[0]
	return hash, nil
}

// Checksum computes SHA-1 hash of file
func Sha1(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha1.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func Checksum(path string) (string, error) {
	if strings.ToLower(filepath.Ext(path)) == ".gpkg" {
		dbhash, err := DBHash(path)
		return "dbhash:" + dbhash, err
	}
	return Sha1(path)
}

var excludeExtRegex = regexp.MustCompile(`(?i).*\.(gpkg-wal|gpkg-shm)$`)

func NewDiskStorage(log *zap.SugaredLogger, projectsRoot string) *DiskStorage {
	return &DiskStorage{
		ProjectsRoot: projectsRoot,
		log:          log,
		// cache:        ttlcache.NewCache(),
	}
}

func saveJsonFile(path string, data interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	if err := encoder.Encode(data); err != nil {
		return err
	}
	return nil
}

func (s *DiskStorage) saveConfigFile(projectName, filename string, data interface{}) error {
	indexFilePath := filepath.Join(s.ProjectsRoot, projectName, ".gisquick", filename)
	if err := saveJsonFile(indexFilePath, data); err != nil {
		return fmt.Errorf("creating project file: %w", err)
	}
	return nil
}

func (s *DiskStorage) Create(fullName string, meta json.RawMessage) (*domain.ProjectInfo, error) {
	projDir := filepath.Join(s.ProjectsRoot, fullName)
	internalDir := filepath.Join(projDir, ".gisquick")
	if s.checkProjectExists(fullName) {
		return nil, domain.ErrProjectAlreadyExists
	}
	if err := os.MkdirAll(internalDir, 0777); err != nil {
		return nil, err
	}

	var i Info
	if err := json.Unmarshal(meta, &i); err != nil {
		s.log.Errorw("parsing qgis meta", zap.Error(err))
		return nil, domain.ErrInvalidQgisMeta
	}

	if err := s.saveConfigFile(fullName, "qgis.json", meta); err != nil {
		return nil, fmt.Errorf("creating qgis meta file: %w", err)
	}

	info := domain.ProjectInfo{
		QgisFile:   i.File,
		Projection: i.Projection,
		Title:      i.Title,
		State:      "empty",
		Created:    time.Now().UTC(),
	}
	return &info, s.saveConfigFile(fullName, "project.json", info)
}

func (s *DiskStorage) UserProjects(username string) ([]string, error) {
	projectsNames := make([]string, 0)
	userDir := filepath.Join(s.ProjectsRoot, username)
	entries, err := os.ReadDir(userDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return projectsNames, nil
		}
		return projectsNames, fmt.Errorf("listing projects: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			projectName := filepath.Join(username, entry.Name())
			projPath := filepath.Join(userDir, entry.Name(), ".gisquick", "project.json")
			if fileExists(projPath) {
				projectsNames = append(projectsNames, projectName)
			}
		}
	}
	return projectsNames, nil
}

func (s *DiskStorage) checkProjectExists(name string) bool {
	projPath := filepath.Join(s.ProjectsRoot, name, ".gisquick", "project.json")
	return fileExists(projPath)
}

func (s *DiskStorage) GetProjectInfo(name string) (domain.ProjectInfo, error) {
	var pInfo domain.ProjectInfo
	projPath := filepath.Join(s.ProjectsRoot, name, ".gisquick", "project.json")

	// ver. 1
	/*
		f, err := os.Open(projPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return pInfo, domain.ErrProjectNotExists
			}
		}
		defer f.Close()
		decoder := json.NewDecoder(f)
		if err := decoder.Decode(&pInfo); err != nil {
			s.log.Errorw("parsing project file", zap.Error(err))
			return pInfo, fmt.Errorf("reading project file: %w", err)
		}
	*/
	// ver. 2
	content, err := ioutil.ReadFile(projPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return pInfo, domain.ErrProjectNotExists
		}
		return pInfo, fmt.Errorf("reading project file: %w", err)
	}
	err = json.Unmarshal(content, &pInfo)
	if err != nil {
		s.log.Errorw("parsing project file", zap.Error(err))
		return pInfo, fmt.Errorf("reading project file: %w", err)
	}
	return pInfo, nil
}

// var excludeExtRegex = regexp.MustCompile(`(?i).*\.(gpkg-wal|gpkg-shm)$`)

func (s *DiskStorage) ListProjectFiles(project string, checksum bool) ([]domain.ProjectFile, error) {
	if !s.checkProjectExists(project) {
		return nil, domain.ErrProjectNotExists
	}

	root, err := filepath.Abs(filepath.Join(s.ProjectsRoot, project))
	if err != nil {
		return nil, err
	}
	var files []domain.ProjectFile = []domain.ProjectFile{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			relPath := path[len(root)+1:]
			if !strings.HasPrefix(relPath, ".gisquick/") && !strings.HasSuffix(relPath, "~") && !excludeExtRegex.Match([]byte(relPath)) {
				hash := ""
				if checksum {
					if hash, err = Checksum(path); err != nil {
						return fmt.Errorf("computing checksum: %w", err)
					}
				}
				fInfo, err := entry.Info()
				if err != nil {
					return fmt.Errorf("getting file info: %w", err)
				}
				files = append(files, domain.ProjectFile{Path: relPath, Hash: hash, Size: fInfo.Size(), Mtime: fInfo.ModTime()})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing project files: %w", err)
	}
	return files, nil
}

func (s *DiskStorage) Delete(name string) error {
	dest := filepath.Join(s.ProjectsRoot, name)
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	// delete Mapcache
	return nil
}

type FileWriter struct {
	Filename string
	File     *os.File
	checksum hash.Hash
}

func newFileWritter(filename string) (*FileWriter, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, err
	}
	h := sha1.New()
	return &FileWriter{Filename: filename, File: file, checksum: h}, nil
}

func (w *FileWriter) Write(p []byte) (n int, err error) {
	w.checksum.Write(p)
	return w.File.Write(p)
}

func (w *FileWriter) Close() error {
	return w.File.Close()
}

func (w *FileWriter) Checksum() string {
	return fmt.Sprintf("%x", w.checksum.Sum(nil))
}

func saveToFile(src io.Reader, filename string) (err error) {
	err = os.MkdirAll(filepath.Dir(filename), 0777)
	if err != nil {
		return err
	}
	file, err := os.Create(filename)
	if err != nil {
		return err
	}

	// more verbose but with better errors propagation
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err := io.Copy(file, src); err != nil {
		return err
	}
	return nil
}

func saveToFile2(src io.Reader, filename string) (h string, err error) {
	err = os.MkdirAll(filepath.Dir(filename), 0777)
	if err != nil {
		return
	}
	w, err := newFileWritter(filename)
	if err != nil {
		return
	}

	// more verbose but with better errors propagation
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err := io.Copy(w, src); err != nil {
		return "", err
	}
	// log.Println("saveToFile2 checksum"
	return w.Checksum(), nil
}

func (s *DiskStorage) SaveFile(projectName, filename string, r io.Reader) error {
	if !s.checkProjectExists(projectName) {
		return domain.ErrProjectNotExists
	}
	destPath := filepath.Join(s.ProjectsRoot, projectName, filename)
	return saveToFile(r, destPath)
}

func (s *DiskStorage) GetQgisMetaPath(projectName string) string {
	return filepath.Join(s.ProjectsRoot, projectName, ".gisquick", "qgis.json")
}

func (s *DiskStorage) GetSettingsPath(projectName string) string {
	return filepath.Join(s.ProjectsRoot, projectName, ".gisquick", "settings.json")
}

func (s *DiskStorage) GetThumbnailPath(projectName string) string {
	return filepath.Join(s.ProjectsRoot, projectName, ".gisquick", "thumbnail")
}

func (s *DiskStorage) SaveThumbnail(projectName string, r io.Reader) error {
	project, err := s.GetProjectInfo(projectName)
	if err != nil {
		return err
	}
	if err := saveToFile(r, s.GetThumbnailPath(projectName)); err != nil {
		return fmt.Errorf("saving thumbnail file: %w", err)
	}
	project.Thumbnail = true
	project.LastUpdate = time.Now().UTC()
	if err := s.saveConfigFile(projectName, "project.json", project); err != nil {
		return fmt.Errorf("updating project file: %w", err)
	}
	return nil
}

func (s *DiskStorage) filesIndex(projectName string) ([]domain.ProjectFile, error) {
	var files []domain.ProjectFile
	if !s.checkProjectExists(projectName) {
		return files, domain.ErrProjectNotExists
	}
	indexPath := filepath.Join(s.ProjectsRoot, projectName, ".gisquick", "files.json")
	f, err := os.Open(indexPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return files, nil
		}
		return files, fmt.Errorf("reading index file: %w", err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&files); err != nil {
		// s.log.Errorw("parsing project files index", zap.Error(err))
		return files, fmt.Errorf("parsing index file: %w", err)
	}
	return files, nil
}

func (s *DiskStorage) filesIndex2(projectName string) (map[string]FileInfo, error) {
	var index map[string]FileInfo
	if !s.checkProjectExists(projectName) {
		return make(map[string]FileInfo), domain.ErrProjectNotExists
	}
	indexPath := filepath.Join(s.ProjectsRoot, projectName, ".gisquick", "filesmap.json")
	f, err := os.Open(indexPath)
	if err != nil {
		index = make(map[string]FileInfo)
		if errors.Is(err, os.ErrNotExist) {
			return index, nil
		}
		return index, fmt.Errorf("reading index file: %w", err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&index); err != nil {
		// s.log.Errorw("parsing project files index", zap.Error(err))
		return make(map[string]FileInfo), fmt.Errorf("parsing index file: %w", err)
	}
	return index, nil
}

type FileInfo struct {
	Hash  string `json:"hash"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
	// Mtime time.Time `json:"mtime"`
}

func createFilesIndex(files []domain.ProjectFile) map[string]FileInfo {
	index := make(map[string]FileInfo, len(files))
	for _, f := range files {
		index[f.Path] = FileInfo{Hash: f.Hash, Size: f.Size, Mtime: time.Now().Unix()}
	}
	return index
}

func calcNewSize(index map[string]FileInfo, info domain.FilesChanges) int64 {
	sizeMap := make(map[string]int64, len(index))
	for path, f := range index {
		sizeMap[path] = f.Size
	}
	for _, path := range info.Removes {
		delete(sizeMap, path)
	}
	for _, f := range info.Updates {
		sizeMap[f.Path] = f.Size
	}
	var sum int64 = 0
	for _, size := range sizeMap {
		sum += size
	}
	return sum
}

func (s *DiskStorage) UpdateFiles(projectName string, info domain.FilesChanges, next func() (string, io.ReadCloser, error)) ([]domain.ProjectFile, error) { // ([]domain.ProjectFile, error)
	// index, err := s.filesIndex(projectName)
	project, err := s.GetProjectInfo(projectName)
	if err != nil {
		return nil, err
	}
	index, err := s.filesIndex2(projectName)
	if err != nil {
		return nil, err
	}
	expectedSize := calcNewSize(index, info)
	files := info.Updates

	// i := 0
	// for {
	// 	path, reader, err := next()
	// 	if err != nil {
	// 		if err == io.EOF {
	// 			break
	// 		}
	// 		return nil, err
	// 	}
	// 	if i >= len(files) {
	// 		return nil, fmt.Errorf("missing file change metadata: %s", path)
	// 	}

	s.log.Info("update files count", len(files))
	for i := 0; i <= len(files); i++ {
		path, reader, err := next()
		if err != nil {
			if err != io.EOF {
				return nil, fmt.Errorf("reading file stream: %w", err)
			}
			break
		}
		declaredInfo := files[i]
		if declaredInfo.Path != path {
			return nil, err // TODO: more graceful error handling
		}
		absPath := filepath.Join(s.ProjectsRoot, projectName, path)
		// if err := saveToFile(reader, absPath); err != nil {
		// 	return err
		// }
		calcHash, err := saveToFile2(reader, absPath)
		if err != nil {
			reader.Close() // or move to saveToFile?
			return nil, err
		}
		s.log.Infow("saving file", "path", absPath, "hash", calcHash, "hashMatch", declaredInfo.Hash == calcHash)
		reader.Close()
		fStat, err := os.Stat(absPath)
		if err != nil {
			// ???
			return nil, err
		}
		if declaredInfo.Size != fStat.Size() {
			return nil, fmt.Errorf("declared file info doesn't match: %s", path)
		}
		index[path] = FileInfo{Hash: calcHash, Size: declaredInfo.Size, Mtime: fStat.ModTime().Unix()}
		// i += 1
	}
	for _, path := range info.Removes {
		absPath := filepath.Join(s.ProjectsRoot, projectName, path)
		info, err := os.Lstat(absPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				delete(index, path)
				continue
			}
			return nil, fmt.Errorf("removing file/directory %s: %w", path, err)
		}
		if info.IsDir() {
			if err := os.RemoveAll(absPath); err != nil {
				return nil, fmt.Errorf("removing project directory %s: %w", path, err) // TODO: or allow this kind of error?
			}
			dirPrefix := path + string(filepath.Separator)
			for n := range index {
				if strings.HasPrefix(n, dirPrefix) {
					delete(index, n)
				}
			}
		} else {
			if err := os.Remove(absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("removing project file %s: %w", path, err) // TODO: or allow this kind of error?
			}
			delete(index, path)
		}
	}
	if err := saveJsonFile(filepath.Join(s.ProjectsRoot, projectName, ".gisquick", "filesmap.json"), index); err != nil {
		return nil, fmt.Errorf("updating files index: %w", err)
	}
	// calc new size of project
	size := 0
	for _, info := range index {
		size += int(info.Size)
	}
	s.log.Infof("project size: %d / expected: %d", size, expectedSize)
	project.Size = size
	if project.State == "empty" {
		project.State = "staged"
	}
	project.LastUpdate = time.Now().UTC()
	if err := s.saveConfigFile(projectName, "project.json", project); err != nil {
		return nil, fmt.Errorf("updating project file: %w", err)
	}
	listIndex := make([]domain.ProjectFile, len(index))
	i := 0
	for path, info := range index {
		listIndex[i] = domain.ProjectFile{Path: path, Hash: info.Hash, Size: info.Size, Mtime: time.Unix(info.Mtime, 0)}
		i += 1
	}
	return listIndex, nil
}

func (s *DiskStorage) RemoveFiles(projectName string, files ...string) ([]domain.ProjectFile, error) { // ([]domain.ProjectFile, error)
	// index, err := s.filesIndex(projectName)
	project, err := s.GetProjectInfo(projectName)
	if err != nil {
		return nil, err
	}
	index, err := s.filesIndex2(projectName)
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		absPath := filepath.Join(s.ProjectsRoot, projectName, path)
		info, err := os.Lstat(absPath)
		if err != nil {
			return nil, fmt.Errorf("removing file/directory %s: %w", path, err)
		}
		if info.IsDir() {
			if err := os.RemoveAll(absPath); err != nil {
				return nil, fmt.Errorf("removing project directory %s: %w", path, err) // TODO: or allow this kind of error?
			}
			dirPrefix := path + string(filepath.Separator)
			for n := range index {
				if strings.HasPrefix(n, dirPrefix) {
					delete(index, n)
				}
			}
		} else {
			if err := os.Remove(absPath); err != nil {
				return nil, fmt.Errorf("removing project file %s: %w", path, err) // TODO: or allow this kind of error?
			}
			delete(index, path)
		}
	}
	if err := saveJsonFile(filepath.Join(s.ProjectsRoot, projectName, ".gisquick", "filesmap.json"), index); err != nil {
		return nil, fmt.Errorf("updating files index: %w", err)
	}
	// calc new size of project
	size := 0
	for _, info := range index {
		size += int(info.Size)
	}
	project.Size = size
	project.LastUpdate = time.Now().UTC()
	if err := s.saveConfigFile(projectName, "project.json", project); err != nil {
		return nil, fmt.Errorf("updating project file: %w", err)
	}
	listIndex := make([]domain.ProjectFile, len(index))
	i := 0
	for path, info := range index {
		listIndex[i] = domain.ProjectFile{Path: path, Hash: info.Hash, Size: info.Size, Mtime: time.Unix(info.Mtime, 0)}
		i += 1
	}
	return listIndex, nil
}

type SettingsInfo struct {
	Title string `json:"title"`
	Auth  struct {
		Type string `json:"type"`
	} `json:"auth"`
}

func (s *DiskStorage) UpdateSettings(projectName string, data json.RawMessage) error {
	project, err := s.GetProjectInfo(projectName)
	if err != nil {
		return err
	}
	var sInfo SettingsInfo
	if err := json.Unmarshal(data, &sInfo); err != nil {
		return fmt.Errorf("extracting authentication settings: %w", err)
	}
	if err := s.saveConfigFile(projectName, "settings.json", data); err != nil {
		return fmt.Errorf("saving settings file: %w", err)
	}
	project.State = "published"
	project.LastUpdate = time.Now().UTC()
	project.Authentication = sInfo.Auth.Type
	project.Title = sInfo.Title
	if err := s.saveConfigFile(projectName, "project.json", project); err != nil {
		return fmt.Errorf("updating project file: %w", err)
	}
	return nil
}

func (s *DiskStorage) GetSettings(projectName string) (domain.ProjectSettings, error) {
	var settings domain.ProjectSettings
	content, err := os.ReadFile(s.GetSettingsPath(projectName))
	if err != nil {
		return settings, err
	}
	err = json.Unmarshal(content, &settings)
	// err = jsoniter.Unmarshal(content, &settings)
	return settings, err
}

func (s *DiskStorage) ParseQgisMetadata(projectName string, data interface{}) error {
	content, err := os.ReadFile(s.GetQgisMetaPath(projectName))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(content, &data); err != nil {
		return err
	}
	return nil
}

func (s *DiskStorage) UpdateMeta(projectName string, meta json.RawMessage) error {
	pInfo, err := s.GetProjectInfo(projectName)
	if err != nil {
		return err
	}
	var i Info
	if err := json.Unmarshal(meta, &i); err != nil {
		s.log.Errorw("parsing qgis meta", zap.Error(err))
		return domain.ErrInvalidQgisMeta
	}

	if err := s.saveConfigFile(projectName, "qgis.json", meta); err != nil {
		return fmt.Errorf("creating qgis meta file: %w", err)
	}

	pInfo.QgisFile = i.File
	pInfo.Projection = i.Projection
	pInfo.Title = i.Title
	pInfo.LastUpdate = time.Now().UTC()
	return s.saveConfigFile(projectName, "project.json", pInfo)
}

func (s *DiskStorage) GetScripts(projectName string) (domain.Scripts, error) {
	file := filepath.Join(s.ProjectsRoot, projectName, ".gisquick", "scripts.json")
	content, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var data domain.Scripts
	err = json.Unmarshal(content, &data)
	return data, nil
}

func (s *DiskStorage) UpdateScripts(projectName string, scripts domain.Scripts) error {
	return s.saveConfigFile(projectName, "scripts.json", scripts)
}

func (s *DiskStorage) GetScriptsPath(projectName string) string {
	// if !s.checkProjectExists(projectName) {
	// 	return nil, domain.ErrProjectNotExists
	// }
	return filepath.Join(s.ProjectsRoot, projectName, "web")
}
