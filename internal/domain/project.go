package domain

import (
	"encoding/json"
	"errors"
	"io"
	"time"
)

var (
	ErrProjectNotExists     = errors.New("project does not exists")
	ErrProjectAlreadyExists = errors.New("project already exists")
	ErrProjectSize          = errors.New("project size is over limit")
)

// Old code, currently used in mapcache package
type ProjectFileInfo struct {
	User     string
	Folder   string
	Name     string
	FullName string
	Map      string
}

// TODO: remove
type Project struct {
	Info     ProjectFileInfo
	Meta     map[string]interface{}
	Settings ProjectSettings
}

func (p *Project) ProjectionCode() string {
	projection, ok := p.Meta["projection"].(map[string]interface{})
	if !ok {
		return ""
	}
	return projection["code"].(string)
}

type Projection struct {
	Proj4       string `json:"proj4"`
	IsGeografic bool   `json:"is_geographic"`
}

type LayerPermission struct {
	View   bool `json:"view"`
	Insert bool `json:"insert"`
	Update bool `json:"update"`
	Delete bool `json:"delete"`
}

type UserRolesPermissions struct {
	roles      []ProjectRole // user roles
	layers     map[string]Flags
	attributes map[string]map[string]Flags
}

func NewUserRolesPermissions(user User, auth Authentication) *UserRolesPermissions {
	if auth.Roles == nil {
		return nil
	}
	roles := FilterUserRoles(user, auth.Roles)
	layersFlags := make(map[string]Flags)
	attributesFlags := make(map[string]map[string]Flags)
	return &UserRolesPermissions{roles: roles, layers: layersFlags, attributes: attributesFlags}
}

func (p *UserRolesPermissions) LayerFlags(layerId string) Flags {
	// if len(p.roles) == 0 {
	// 	return Flags([]string{})
	// }
	flags, exists := p.layers[layerId]
	if !exists {
		flags = p.roles[0].Permissions.Layers[layerId]
		for _, f := range p.roles[1:] {
			flags = flags.Union(f.Permissions.Layers[layerId])
		}
		p.layers[layerId] = flags
	}
	return flags
}

func (p *UserRolesPermissions) AttributesFlags(layerId string) map[string]Flags {
	// if len(p.roles) == 0 {
	// 	return Flags([]string{})
	// }
	flagsMap, exists := p.attributes[layerId]
	if !exists {
		flagsMap = p.roles[0].Permissions.Attributes[layerId]
		// flagsMap = make(map[string]Flags, 0) // is it needed to check if Permissions.Attributes[layerId] exists?
		for _, f := range p.roles[1:] {
			for attrName, flags := range flagsMap {
				flagsMap[attrName] = flags.Union(f.Permissions.Attributes[layerId][attrName])
			}
		}
		p.attributes[layerId] = flagsMap
	}
	return flagsMap
}

func (s ProjectSettings) UserLayerPermissions(u User, layerId string) LayerPermission {
	lset, ok := s.Layers[layerId]
	if !ok || lset.Flags.Has("excluded") {
		return LayerPermission{}
	}
	if s.Auth.Roles == nil {
		// TODO: map layer wfs flags
		return LayerPermission{View: true, Insert: true, Update: true, Delete: true}
	}
	roles := FilterUserRoles(u, s.Auth.Roles)
	flags := roles[0].Permissions.Layers[layerId]
	for _, f := range roles[1:] {
		flags = flags.Union(f.Permissions.Layers[layerId])
	}

	return LayerPermission{
		View:   flags.Has("view"),
		Insert: flags.Has("insert"),
		Update: flags.Has("update"),
		Delete: flags.Has("delete"),
	}
}

type ProjectFile struct {
	Path  string    `json:"path"`
	Hash  string    `json:"hash"`
	Size  int64     `json:"size"`
	Mtime time.Time `json:"mtime"`
}

func checkUserRole(u User, role ProjectRole) bool {
	if role.Auth == "all" {
		return true
	}
	if role.Auth == "authenticated" {
		return u.IsAuthenticated
	}
	if role.Auth == "anonymous" {
		return !u.IsAuthenticated
	}
	if role.Auth == "users" {
		for _, username := range role.Users {
			if u.Username == username {
				return true
			}
		}
	}
	return false
}

func FilterUserRoles(u User, roles []ProjectRole) []ProjectRole {
	var userRoles []ProjectRole
	for _, r := range roles {
		if r.Auth != "other" && checkUserRole(u, r) {
			userRoles = append(userRoles, r)
		}
	}
	if len(userRoles) == 0 {
		for _, r := range roles {
			if r.Auth == "other" {
				userRoles = append(userRoles, r)
			}
		}
	}
	return userRoles
}

type FilesChanges struct {
	Removes []string
	Updates []ProjectFile
}

type ScriptModule struct {
	Path       string   `json:"path"`
	Components []string `json:"components"`
}

type Scripts map[string]ScriptModule

type ProjectsRepository interface {
	CheckProjectExists(name string) bool
	Create(name string, qmeta json.RawMessage) (*ProjectInfo, error)
	UserProjects(user string) ([]string, error) // or should it require User object?
	GetProjectInfo(name string) (ProjectInfo, error)
	Delete(name string) error
	// SaveFile(projectName, filename string, r io.Reader) error
	CreateFile(projectName, pattern string, r io.Reader, size int64) (ProjectFile, error)
	SaveFile(project string, finfo ProjectFile, path string) error

	ListProjectFiles(project string, checksum bool) ([]ProjectFile, error)

	ParseQgisMetadata(projectName string, data interface{}) error
	UpdateMeta(projectName string, meta json.RawMessage) error

	GetSettings(projectName string) (ProjectSettings, error)
	UpdateSettings(projectName string, data json.RawMessage) error

	GetThumbnailPath(projectName string) string
	SaveThumbnail(projectName string, r io.Reader) error

	UpdateFiles(projectName string, info FilesChanges, next func() (string, io.ReadCloser, error)) ([]ProjectFile, error)
	GetScripts(projectName string) (Scripts, error)
	UpdateScripts(projectName string, scripts Scripts) error
	Close()
}
