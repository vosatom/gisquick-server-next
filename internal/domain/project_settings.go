package domain

import "encoding/json"

type AttributeSettings struct {
	Widget    string                 `json:"widget,omitempty"`
	Config    map[string]interface{} `json:"config,omitempty"`
	Formatter string                 `json:"format,omitempty"`
}

type LayerSettings struct {
	Flags                Flags                        `json:"flags"`
	Attributes           map[string]AttributeSettings `json:"attributes"`
	InfoPanelComponent   string                       `json:"infopanel_component,omitempty"` // or group with other possible settings into generic map[string]interface{}
	AttributeTableFields []string                     `json:"attr_table_fields,omitempty"`
	InfoPanelFields      []string                     `json:"info_panel_fields,omitempty"`
	ExportFields         []string                     `json:"export_fields,omitempty"`
}

type Topic struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Abstract string   `json:"abstract"`
	Layers   []string `json:"visible_overlays"`
}

type ProjectRole struct {
	Auth        string          `json:"type"`
	Name        string          `json:"name"`
	Users       []string        `json:"users"`
	Permissions RolePermissions `json:"permissions"`
}

type RolePermissions struct {
	Attributes map[string]map[string]Flags `json:"attributes"`
	Layers     map[string]Flags            `json:"layers"`
	Topics     []string                    `json:"topics"`
}

type Authentication struct {
	Type  string        `json:"type"`
	Users []string      `json:"users,omitempty"`
	Roles []ProjectRole `json:"roles,omitempty"`
}

type ProjectSettings struct {
	Auth            Authentication           `json:"auth"` // or access?
	Users           []string                 `json:"users,omitempty"`
	BaseLayers      []string                 `json:"base_layers"`
	Layers          map[string]LayerSettings `json:"layers"`
	Title           string                   `json:"title"`
	MapCache        bool                     `json:"use_mapcache"`
	Topics          []Topic                  `json:"topics"`
	Extent          []float64                `json:"extent"`
	InitialExtent   []float64                `json:"initial_extent"`
	Scales          json.RawMessage          `json:"scales"`
	TileResolutions []float64                `json:"tile_resolutions"`
	Formatters      []json.RawMessage        `json:"formatters,omitempty"`
}
