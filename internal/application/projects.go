package application

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ReneKroon/ttlcache/v2"
	"github.com/gisquick/gisquick-server/internal/domain"
	"go.uber.org/zap"
)

type ProjectService interface {
	Create(username, name string, meta json.RawMessage) (*domain.ProjectInfo, error)
	Delete(projectName string) error
	GetProjectInfo(projectName string) (domain.ProjectInfo, error)
	GetUserProjects(username string) ([]domain.ProjectInfo, error)
	SaveFile(projectName, filename string, r io.Reader) error
	ListProjectFiles(projectName string, checksum bool) ([]domain.ProjectFile, error)

	GetQgisMetadata(projectName string, data interface{}) error
	UpdateMeta(projectName string, meta json.RawMessage) error

	GetSettings(projectName string) (domain.ProjectSettings, error)
	UpdateSettings(projectName string, data json.RawMessage) error

	GetThumbnailPath(projectName string) string
	SaveThumbnail(projectName string, r io.Reader) error

	UpdateFiles(projectName string, info domain.FilesChanges, next func() (string, io.ReadCloser, error)) ([]domain.ProjectFile, error)

	GetLayersData(projectName string) (LayersData, error)
	GetMapConfig(projectName string, user domain.User) (map[string]interface{}, error)

	GetScripts(projectName string) (domain.Scripts, error)
	UpdateScripts(projectName string, scripts domain.Scripts) error
	RemoveScripts(projectName string, modules ...string) (domain.Scripts, error)
	GetScriptsPath(projectName string) string
}

type projectService struct {
	log   *zap.SugaredLogger
	repo  domain.ProjectsRepository
	cache *ttlcache.Cache
}

func NewProjectsService(log *zap.SugaredLogger, repo domain.ProjectsRepository) *projectService {
	return &projectService{
		log:   log,
		repo:  repo,
		cache: ttlcache.NewCache(),
	}
}

func (s *projectService) Create(username, name string, meta json.RawMessage) (*domain.ProjectInfo, error) {
	projName := filepath.Join(username, name)
	return s.repo.Create(projName, meta)
}

func (s *projectService) GetProjectInfo(name string) (domain.ProjectInfo, error) {
	return s.repo.GetProjectInfo(name)
}

func (s *projectService) Delete(name string) error {
	p, err := s.GetProjectInfo(name)
	if err != nil {
		return err
	}
	if p.Mapcache {
		// TODO
	}
	return s.repo.Delete(name)
}

func (s *projectService) ListProjectFiles(project string, checksum bool) ([]domain.ProjectFile, error) {
	return s.repo.ListProjectFiles(project, checksum)
}

func (s *projectService) GetUserProjects(username string) ([]domain.ProjectInfo, error) {
	projects, err := s.repo.UserProjects(username)
	if err != nil {
		return nil, err
	}
	data := make([]domain.ProjectInfo, len(projects))
	for i, name := range projects {
		info, err := s.repo.GetProjectInfo(name)
		info.Name = name
		if err != nil {
			// TODO: skip or fail?
			return nil, err
		}
		data[i] = info
	}
	return data, nil
}

func (s *projectService) SaveFile(projectName, filename string, r io.Reader) error {
	return s.repo.SaveFile(projectName, filename, r)
}

func (s *projectService) GetQgisMetadata(projectName string, data interface{}) error {
	return s.repo.ParseQgisMetadata(projectName, data)
}

func (s *projectService) UpdateMeta(projectName string, meta json.RawMessage) error {
	return s.repo.UpdateMeta(projectName, meta)
}

func (s *projectService) GetSettings(projectName string) (domain.ProjectSettings, error) {
	return s.repo.GetSettings(projectName)
}

func (s *projectService) UpdateSettings(projectName string, data json.RawMessage) error {
	return s.repo.UpdateSettings(projectName, data)
}

func (s *projectService) SaveThumbnail(projectName string, r io.Reader) error {
	return s.repo.SaveThumbnail(projectName, r)
}

func (s *projectService) GetThumbnailPath(projectName string) string {
	return s.repo.GetThumbnailPath(projectName)
}

func (s *projectService) UpdateFiles(projectName string, info domain.FilesChanges, next func() (string, io.ReadCloser, error)) ([]domain.ProjectFile, error) {
	return s.repo.UpdateFiles(projectName, info, next)
}

func (s *projectService) GetScripts(projectName string) (domain.Scripts, error) {
	return s.repo.GetScripts(projectName)
}

func (s *projectService) UpdateScripts(projectName string, scripts domain.Scripts) error {
	return s.repo.UpdateScripts(projectName, scripts)
}

func (s *projectService) RemoveScripts(projectName string, modules ...string) (domain.Scripts, error) {
	scripts, err := s.GetScripts(projectName)
	if err != nil {
		return nil, err
	}
	files := make([]string, len(modules))
	for i, m := range modules {
		meta := scripts[m]
		files[i] = filepath.Join("web", meta.Path)
		delete(scripts, m)
	}
	s.log.Infow("RemoveScripts", "files", files, "scripts", scripts)
	changes := domain.FilesChanges{Removes: files}
	_, err = s.UpdateFiles(projectName, changes, nil)
	if err != nil {
		return nil, err
	}
	return scripts, s.UpdateScripts(projectName, scripts)
}

func (s *projectService) GetScriptsPath(projectName string) string {
	return s.repo.GetScriptsPath(projectName)
}

func contains(items []string, value string) bool {
	for _, i := range items {
		if i == value {
			return true
		}
	}
	return false
}

type LayersData struct {
	LayerNameToID map[string]string
}

func (s *projectService) GetLayersData(projectName string) (LayersData, error) {
	type LayersMetadata struct {
		Layers map[string]domain.LayerMeta `json:"layers"`
	}
	var meta LayersMetadata
	if err := s.GetQgisMetadata(projectName, &meta); err != nil {
		return LayersData{}, nil
	}
	nameToID := make(map[string]string, len(meta.Layers))
	for id, layer := range meta.Layers {
		nameToID[layer.Name] = id
	}
	data := LayersData{
		LayerNameToID: nameToID,
	}
	return data, nil
}

func (s *projectService) GetLayerPermissions(projectName string, layerId string, user domain.User) (domain.LayerPermission, error) {
	settings, err := s.GetSettings(projectName)
	if err != nil {
		return domain.LayerPermission{}, err
	}
	return settings.UserLayerPermissions(user, layerId), nil
	// settings.GetLayerPermissions()
}

type BaseLayer struct {
	Name         string            `json:"name"`
	Title        string            `json:"title"`
	Type         string            `json:"type"`
	Projection   string            `json:"projection"`
	LegendURL    string            `json:"legend_url,omitempty"`
	Metadata     map[string]string `json:"metadata"`
	Attribution  map[string]string `json:"attribution,omitempty"`
	Extent       []float64         `json:"extent"`
	Provider     string            `json:"provider_type"`
	SourceParams map[string]string `json:"source"`

	// WMS params, old API
	URL       string   `json:"url"`
	Format    string   `json:"format"`
	WmsLayers []string `json:"wms_layers"`
}

type OverlayLayer struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Type  string `json:"type"`
	// Extent       []float64               `json:"extent"`
	Projection           string                  `json:"projection"`
	LegendURL            string                  `json:"legend_url,omitempty"`
	Metadata             map[string]string       `json:"metadata"`
	Attribution          map[string]string       `json:"attribution,omitempty"`
	Attributes           []domain.LayerAttribute `json:"attributes,omitempty"`
	DrawingOrder         *int                    `json:"drawing_order,omitempty"`
	Visible              bool                    `json:"visible"`
	Hidden               bool                    `json:"hidden"`
	Queryable            bool                    `json:"queryable"`
	GeomType             string                  `json:"wkb_type,omitempty"`
	InfoPanel            string                  `json:"infopanel_component,omitempty"`
	Permissions          domain.LayerPermission  `json:"permissions"`
	AttributeTableFields []string                `json:"attr_table_fields,omitempty"`
	InfoPanelFields      []string                `json:"panel_fields,omitempty"`
	ExportFields         []string                `json:"export_fields,omitempty"`
}

func filterList(list []string, test func(item string) bool) []string {
	res := make([]string, 0)
	for _, v := range list {
		if test(v) {
			res = append(res, v)
		}
	}
	return res
}

func MergeAttributeConfig(meta domain.LayerAttribute, settings domain.AttributeSettings) domain.LayerAttribute {
	attr := domain.LayerAttribute{
		Alias:      meta.Alias,
		Name:       meta.Name,
		Type:       meta.Type,
		Widget:     meta.Widget,
		Config:     meta.Config,
		Constrains: meta.Constrains,
	}
	if settings.Widget != "" {
		attr.Widget = settings.Widget
	}
	if len(settings.Config) > 0 {
		attr.Config = settings.Config
	}
	if settings.Formatter != "" {
		attr.Format = settings.Formatter
	}
	return attr
}

func indexOf(items []string, value string) int {
	for i, v := range items {
		if v == value {
			return i
		}
	}
	return -1
}

func (s *projectService) GetMapConfig(projectName string, user domain.User) (map[string]interface{}, error) {
	var meta domain.QgisMeta
	if err := s.repo.ParseQgisMetadata(projectName, &meta); err != nil {
		return nil, fmt.Errorf("parsing qgis meta: %w", err)
	}
	settings, err := s.repo.GetSettings(projectName)
	if err != nil {
		return nil, err
	}
	layersTree, err := domain.CreateTree2(meta.LayersTree)
	if err != nil {
		return nil, err
	}

	// split layers into base layers and overlay layers
	baseLayers := make([]domain.TreeNode, 0)
	overlays := make([]domain.TreeNode, 0)
	for _, item := range layersTree {
		var id string
		if item.IsGroup() {
			id = item.GroupName()
		} else {
			id = item.LayerID()
		}
		if contains(settings.BaseLayers, id) {
			baseLayers = append(baseLayers, item)
		} else {
			overlays = append(overlays, item)
		}
	}

	rolesPerms := domain.NewUserRolesPermissions(user, settings.Auth)

	baseLayersData, err := domain.TransformLayersTree(
		baseLayers,
		func(id string) bool {
			return !settings.Layers[id].Flags.Has("excluded") && (rolesPerms == nil || rolesPerms.LayerFlags(id).Has("view"))
		},
		func(id string) interface{} {
			lmeta := meta.Layers[id]
			ldata := BaseLayer{
				Name:         lmeta.Name,
				Title:        lmeta.Title,
				Type:         lmeta.Type,
				Projection:   lmeta.Projection,
				Metadata:     lmeta.Metadata,
				LegendURL:    lmeta.LegendURL,
				Attribution:  lmeta.Attribution,
				Extent:       lmeta.Extent,
				Provider:     lmeta.Provider,
				SourceParams: lmeta.SourceParams,
			}
			if lmeta.Type == "RasterLayer" && lmeta.Provider == "wms" {
				ldata.Format = string(lmeta.SourceParams["format"])
				ldata.URL = string(lmeta.SourceParams["url"])
				ldata.WmsLayers = strings.Split(string(lmeta.SourceParams["layers"]), ",")
			}
			return ldata
		},
	)

	layers, err := domain.TransformLayersTree(
		overlays,
		func(id string) bool {
			drawingOrder := indexOf(meta.LayersOrder, id)
			// return !settings.Layers[id].Flags.Has("excluded") && rolesPerms.LayerFlags(id).Has("view")
			return drawingOrder != -1 && !settings.Layers[id].Flags.Has("excluded") && (rolesPerms == nil || rolesPerms.LayerFlags(id).Has("view"))
		},
		func(id string) interface{} {
			lmeta := meta.Layers[id]
			lset := settings.Layers[id]
			lflags := lset.Flags
			if rolesPerms != nil {
				lflags = lflags.Intersection(rolesPerms.LayerFlags(id))
			}

			queryable := lmeta.Flags.Has("query") && !lset.Flags.Has("hidden") && lflags.Has("query")
			s.log.Infow("layer info", "layer", lmeta.Title, "type", lmeta.Type, "queryable", queryable, "attrs", len(lset.Attributes))

			ldata := OverlayLayer{
				Name:        lmeta.Name,
				Title:       lmeta.Title,
				Projection:  lmeta.Projection,
				Type:        lmeta.Type,
				Metadata:    lmeta.Metadata,
				Hidden:      lset.Flags.Has("hidden"),
				Queryable:   queryable,
				InfoPanel:   lset.InfoPanelComponent,
				LegendURL:   lmeta.LegendURL,
				Attribution: lmeta.Attribution,
				Visible:     lmeta.Visible,
			}
			drawingOrder := indexOf(meta.LayersOrder, id)
			ldata.DrawingOrder = &drawingOrder

			if lmeta.Type == "VectorLayer" {
				json.Unmarshal(lmeta.Options["wkb_type"], &ldata.GeomType)

				// ldata.Attributes[0].Constrains
				if queryable && len(lmeta.Attributes) > 0 {

					// if len(lset.Attributes) > 0 {
					// 	for _, a := range lmeta.Attributes {
					// 		as, ok := lset.Attributes[a.Name]
					// 		if ok {
					// 			s.log.Infow("attribute", "layer", lmeta.Title, "name", a.Name, "settings", as, "config nill", as.Config == nil)
					// 		}
					// 	}
					// }

					ldata.Permissions = domain.LayerPermission{
						View:   queryable,
						Insert: queryable && lflags.Has("insert"),
						Delete: queryable && lflags.Has("delete"),
						Update: queryable && lflags.Has("update"),
					}
					if rolesPerms != nil {
						attrsPerms := rolesPerms.AttributesFlags(id)
						isAttributeVisible := func(item string) bool { return attrsPerms[item].Has("view") }
						ldata.AttributeTableFields = filterList(lset.AttributeTableFields, isAttributeVisible)
						ldata.InfoPanelFields = filterList(lset.InfoPanelFields, isAttributeVisible)
						ldata.ExportFields = filterList(lset.ExportFields, func(item string) bool { return attrsPerms[item].Has("export") })
						ldata.Attributes = make([]domain.LayerAttribute, 0, len(lmeta.Attributes))
						for _, a := range lmeta.Attributes {
							if isAttributeVisible(a.Name) {
								attr := MergeAttributeConfig(a, lset.Attributes[a.Name])
								ldata.Attributes = append(ldata.Attributes, attr)
								if !attrsPerms[a.Name].Has("edit") && attr.Constrains.Has("readonly") {
									attr.Constrains = attr.Constrains.Union(domain.Flags{"readonly"})
								}
							}
						}
						s.log.Infow("ldata.Attributes (perms)", "attrs", ldata.Attributes)
					} else {
						ldata.AttributeTableFields = lset.AttributeTableFields
						ldata.InfoPanelFields = lset.InfoPanelFields
						ldata.ExportFields = lset.ExportFields

						ldata.Attributes = make([]domain.LayerAttribute, len(lmeta.Attributes))
						for i, a := range lmeta.Attributes {
							ldata.Attributes[i] = MergeAttributeConfig(a, lset.Attributes[a.Name])
						}
						s.log.Infow("ldata.Attributes", "attrs", ldata.Attributes)
					}
				}
			}
			return ldata
		},
	)

	data := make(map[string]interface{})
	// data["authentication"] = settings.Authentication
	data["use_mapcache"] = settings.MapCache
	data["zoom_extent"] = settings.InitialExtent
	data["project_extent"] = settings.Extent
	data["scales"] = settings.Scales
	data["tile_resolutions"] = settings.TileResolutions
	data["layers"] = layers
	data["base_layers"] = baseLayersData
	data["projection"] = meta.Projection
	data["projections"] = meta.Projections
	data["units"] = meta.Units
	if len(settings.Formatters) > 0 {
		data["formatters"] = settings.Formatters
	}

	scripts, err := s.GetScripts(projectName)
	if err != nil {
		s.log.Errorw("[GetMapConfig] loading scripts", "project", projectName)
	} else {
		data["scripts"] = scripts
	}
	if settings.Title != "" {
		data["root_title"] = settings.Title
	} else {
		data["root_title"] = meta.Title
	}

	data["name"] = projectName
	data["ows_url"] = fmt.Sprintf("/api/map/ows/%s", projectName)
	data["ows_project"] = projectName

	s.log.Infow("topics", "user", user, "rolesPerms", rolesPerms)
	topics := make([]domain.Topic, 0)
	for _, topic := range settings.Topics {
		layers := make([]string, 0)
		for _, lid := range topic.Layers {
			lset := settings.Layers[lid]

			visible := !lset.Flags.Has("excluded") && !lset.Flags.Has("hidden")
			if visible && rolesPerms != nil {
				// s.log.Infow("topics", "user", user, "layer", lid, "view", rolesPerms.LayerFlags(lid).Has("view"))
				visible = rolesPerms.LayerFlags(lid).Has("view")
			}
			if visible {
				layers = append(layers, meta.Layers[lid].Name)
			}
		}
		if len(layers) > 0 {
			topics = append(topics, domain.Topic{Title: topic.Title, Abstract: topic.Abstract, Layers: layers})
		}
	}
	data["topics"] = topics

	// s.log.Infow("[GetMapConfig]", "layers", meta.Layers)
	return data, nil
}
