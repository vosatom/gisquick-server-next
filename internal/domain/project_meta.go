package domain

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalidQgisMeta = errors.New("invalid qgis meta data")
)

type ProjectInfo struct {
	Name           string    `json:"name,omitempty"`
	Title          string    `json:"title"`
	QgisFile       string    `json:"qgis_file"`
	Created        time.Time `json:"created"`
	LastUpdate     time.Time `json:"last_update"`
	Projection     string    `json:"projection"` // projection code
	Mapcache       bool      `json:"mapcache"`
	Authentication string    `json:"authentication"`
	// empty, pending update, hidden
	State     string `json:"state"`
	Size      int64  `json:"size"` // size in bytes
	Thumbnail bool   `json:"thumbnail"`
}

type LayerNode struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Layers []LayerNode `json:"layers"`
}

type LayerMeta struct {
	Id           string                     `json:"id"`
	Name         string                     `json:"name"`
	Title        string                     `json:"title"`
	Type         string                     `json:"type"`
	Extent       []float64                  `json:"extent"`
	Projection   string                     `json:"projection"`
	Flags        Flags                      `json:"flags"`
	LegendURL    string                     `json:"legend_url,omitempty"`
	Provider     string                     `json:"provider_type"`
	SourceParams QueryParams                `json:"source_params"`
	Metadata     map[string]string          `json:"metadata"`
	Attribution  map[string]string          `json:"attribution,omitempty"`
	Attributes   []LayerAttribute           `json:"attributes,omitempty"` // vector layers
	Bands        []string                   `json:"bands,omitempty"`      // raster layers
	Options      map[string]json.RawMessage `json:"options,omitempty"`
	Visible      bool                       `json:"visible"`
	Relations    json.RawMessage            `json:"relations,omitempty"`
}

type LayerAttribute struct {
	Alias      string                 `json:"alias,omitempty"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Constrains Flags                  `json:"constrains,omitempty"`
	Widget     string                 `json:"widget,omitempty"`
	Config     map[string]interface{} `json:"config,omitempty"`
	Format     string                 `json:"format,omitempty"`
}

type BookmarkMeta struct {
	Id       string    `json:"id"`
	Name     string    `json:"name"`
	Extent   []float64 `json:"extent"`
	Rotation float64   `json:"rotation"`
	Group    string    `json:"group"`
}

type QgisMeta struct {
	File   string    `json:"file"`
	Title  string    `json:"title"`
	Extent []float64 `json:"extent"`
	Scales []float64 `json:"scales"`
	// LayersTree        []TreeNode                      `json:"layers_tree"`
	LayersTree        []interface{}                      `json:"layers_tree"`
	LayersOrder       []string                           `json:"layers_order"`
	BaseLayers        []string                           `json:"base_layers"`
	Layers            map[string]LayerMeta               `json:"layers"`
	Projection        string                             `json:"projection"`
	Projections       map[string]*Projection             `json:"projections"`
	Units             map[string]interface{}             `json:"units"`
	ComposerTemplates []interface{}                      `json:"composer_templates"`
	Client            map[string]interface{}             `json:"client_info"`
	ProjectHash       string                             `json:"project_hash"`
	Bookmarks         map[string]map[string]BookmarkMeta `json:"bookmarks"`
}

type TreeNode interface {
	IsGroup() bool
	Children() []TreeNode
	GroupName() string
	LayerID() string
}

type LayerTreeNode struct {
	ID string `json:"id"`
}

func (l LayerTreeNode) LayerID() string {
	return l.ID
}
func (l LayerTreeNode) IsGroup() bool {
	return false
}
func (l LayerTreeNode) GroupName() string {
	return ""
}
func (l LayerTreeNode) Children() []TreeNode {
	return nil
}

type GroupTreeNode struct {
	Name              string     `json:"name"`
	Layers            []TreeNode `json:"layers"`
	MutuallyExclusive bool       `json:"mutually_exclusive,omitempty"`
}

func (g GroupTreeNode) LayerID() string {
	return ""
}
func (g GroupTreeNode) IsGroup() bool {
	return true
}
func (g GroupTreeNode) GroupName() string {
	return g.Name
}
func (g GroupTreeNode) Children() []TreeNode {
	return g.Layers
}

var ErrInvalidTree = errors.New("Invalid tree structure")

type Object = map[string]interface{}

func createTree(items []interface{}) ([]TreeNode, error) {
	nodes := make([]TreeNode, len(items))
	for i, n := range items {
		switch v := n.(type) {
		case string:
			nodes[i] = LayerTreeNode{v}
		case Object:
			name, nameOk := v["name"].(string)
			children, layersOk := v["layers"].([]interface{})
			if !nameOk || !layersOk {
				return nil, ErrInvalidTree
			}
			subtree, err := createTree(children)
			if err != nil {
				return nil, ErrInvalidTree
			}
			nodes[i] = GroupTreeNode{Name: name, Layers: subtree}
		default:
			return nil, ErrInvalidTree
		}
	}
	return nodes, nil
}

func CreateTree2(items []interface{}) ([]TreeNode, error) {
	nodes := make([]TreeNode, len(items))
	for i, n := range items {
		o, ok := n.(Object)
		if !ok {
			return nil, ErrInvalidTree
		}
		layers, isGroup := o["layers"].([]interface{})
		if isGroup {
			name, ok := o["name"].(string)
			if !ok {
				return nil, ErrInvalidTree
			}
			subtree, err := CreateTree2(layers)
			if err != nil {
				return nil, ErrInvalidTree
			}
			mutuallyExclusive, ok := o["mutually_exclusive"].(bool)
			nodes[i] = GroupTreeNode{Name: name, Layers: subtree, MutuallyExclusive: mutuallyExclusive}
		} else {
			id, ok := o["id"].(string)
			if !ok {
				return nil, ErrInvalidTree
			}
			nodes[i] = LayerTreeNode{id}
		}
	}
	return nodes, nil
}
