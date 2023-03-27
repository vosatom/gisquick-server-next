package server

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

type GetFeature struct {
	XMLName xml.Name `xml:"GetFeature"`
	Query   []Query  `xml:"Query"`
}

type Query struct {
	XMLName    xml.Name       `xml:"Query"`
	TypeName   string         `xml:"typeName,attr"`
	Properties []PropertyName `xml:"ogc:PropertyName"`
	Contents   []AnyTag       `xml:",any"`
}

type PropertyName struct {
	XMLName xml.Name `xml:"ogc:PropertyName"`
	Name    string   `xml:",chardata"`
}

type AnyTag struct {
	XMLName xml.Name
	Content string `xml:",innerxml"`
}

type Transaction struct {
	XMLName xml.Name `xml:"Transaction"`
	Updates []Update `xml:"Update"`
	Inserts []Insert `xml:"Insert"`
	Deletes []Delete `xml:"Delete"`
}

type Update struct {
	XMLName    xml.Name   `xml:"Update"`
	TypeName   string     `xml:"typeName,attr"`
	Properties []Property `xml:"Property"`
}

type InsertObject struct {
	XMLName    xml.Name
	Properties []InsertProperty `xml:",any"`
}
type InsertProperty struct {
	XMLName xml.Name
	// Content string `xml:",innerxml"`
}

type Insert struct {
	XMLName xml.Name       `xml:"Insert"`
	Objects []InsertObject `xml:",any"`
}

type Delete struct {
	XMLName  xml.Name `xml:"Delete"`
	TypeName string   `xml:"typeName,attr"`
}

type Property struct {
	XMLName xml.Name `xml:"Property"`
	Name    string   `xml:"Name"`
	Value   string   `xml:"Value"`
}

type OwsRequestParams struct {
	Map     string `query:"map"`
	Service string `query:"service"`
	Request string `query:"request"`
	Layers  string `query:"layers"`
}

func parseTypeName(typeName string) (string, error) {
	parts := strings.Split(typeName, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("Invalid typeName: %s", typeName)
	}
	return parts[1], nil
}

func (s *Server) handleMapOws() func(c echo.Context) error {
	/*
		director := func(req *http.Request) {
			target, _ := url.Parse(s.Config.MapserverURL)
			query := req.URL.Query()
			mapParam := req.URL.Query().Get("MAP")
			query.Set("MAP", filepath.Join("/publish", mapParam))
			req.URL.RawQuery = query.Encode()
			req.URL.Path = target.Path
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host

			if _, ok := req.Header["User-Agent"]; !ok {
				// explicitly disable User-Agent so it's not set to default value
				req.Header.Set("User-Agent", "")
			}
		}
	*/
	director := func(req *http.Request) {
		target, _ := url.Parse(s.Config.MapserverURL)
		s.log.Infow("Map proxy", "query", req.URL.RawQuery)
		req.URL.Path = target.Path
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host

		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
		req.Header.Del("Cookie")
	}
	rewriteGetCapabilities := func(resp *http.Response) (err error) {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		err = resp.Body.Close()
		if err != nil {
			return err
		}
		// original url is still in xsi:schemaLocation
		// regexp.MustCompile(`xsi:schemaLocation="(.)+"`)

		// reg := regexp.MustCompile(`xlink:href="http://localhost[^"]+"`)
		reg := regexp.MustCompile(`xlink:href="http[s]?://[^"]+MAP=[^"]+"`)

		owsPath := resp.Request.Header.Get("X-Ows-Url")
		doc := string(body)
		replaced := make(map[string]string, 2)
		for _, match := range reg.FindAllString(doc, -1) {
			_, done := replaced[match]
			if !done {
				u := strings.TrimPrefix(match, `xlink:href="`)
				u = strings.TrimSuffix(u, `"`)
				parsed, _ := url.Parse(html.UnescapeString(u))
				params := parsed.Query()
				params.Del("MAP")
				parsed.Path = owsPath
				parsed.RawQuery = params.Encode()
				replaced[match] = fmt.Sprintf(`xlink:href="%s"`, html.EscapeString(parsed.String()))
				doc = strings.ReplaceAll(doc, match, replaced[match])
			}
		}
		newBody := []byte(doc)
		resp.Body = ioutil.NopCloser(bytes.NewReader(newBody))
		resp.ContentLength = int64(len(newBody))
		resp.Header.Set("Content-Length", strconv.Itoa(len(newBody)))
		return nil
	}
	reverseProxy := &httputil.ReverseProxy{Director: director}
	capabilitiesProxy := &httputil.ReverseProxy{Director: director}
	capabilitiesProxy.ModifyResponse = rewriteGetCapabilities

	return func(c echo.Context) error {
		params := new(OwsRequestParams)
		if err := (&echo.DefaultBinder{}).BindQueryParams(c, params); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid query parameters")
		}

		projectName := getProjectName(c)
		pInfo, err := s.projects.GetProjectInfo(projectName)
		if err != nil {
			if errors.Is(err, domain.ErrProjectNotExists) {
				return echo.ErrNotFound
			}
			s.log.Errorw("ows handler", zap.Error(err))
			return err
		}
		settings, err := s.projects.GetSettings(projectName)
		if err != nil {
			return fmt.Errorf("getting project settings: %w", err)
		}

		req := c.Request()
		// Set MAP parameter
		owsProject := filepath.Join("/publish", projectName, pInfo.QgisFile)
		query := req.URL.Query()
		query.Set("MAP", owsProject)
		req.URL.RawQuery = query.Encode()

		if params.Service == "WMS" && strings.EqualFold(params.Request, "GetCapabilities") {
			req.Header.Set("X-Ows-Url", req.URL.Path)
			capabilitiesProxy.ServeHTTP(c.Response(), req)
			return nil
		}

		if len(settings.Auth.Roles) > 0 {
			user, err := s.auth.GetUser(c)
			layersPermFlags := make(map[string]domain.Flags)
			layersData, err := s.projects.GetLayersData(projectName)
			if err != nil {
				return fmt.Errorf("getting layer data: %w", err)
			}
			getLayerId := func(typeName string) string {
				parts := strings.Split(typeName, ":")
				lname := parts[len(parts)-1]
				id, _ := layersData.LayerNameToID[lname]
				return id
			}
			getLayerPermissions := func(typeName string) domain.Flags {
				id := getLayerId(typeName)
				flags, ok := layersPermFlags[id]
				if !ok {
					flags = settings.UserLayerPermissionsFlags(user, id)
					layersPermFlags[id] = flags
				}
				return flags
			}
			if params.Service == "WMS" && strings.EqualFold(params.Request, "GetMap") {
				for _, lname := range strings.Split(params.Layers, ",") {
					if !getLayerPermissions(lname).Has("view") {
						return echo.ErrForbidden
					}
				}
			}
			if params.Service == "WFS" {
				layersAttrsFlags := make(map[string]map[string]domain.Flags)
				getLayerAttributesFlags := func(typeName string) map[string]domain.Flags {
					id := getLayerId(typeName)
					attrsFlags, ok := layersAttrsFlags[id]
					if !ok {
						attrsFlags = settings.UserLayerAttrinutesFlags(user, id)
					}
					return attrsFlags
				}

				if params.Request == "" && req.Method == "POST" { // GetFeature Insert/Update/Delete
					var wfsTransaction Transaction
					// read all bytes from content body and create new stream using it.
					bodyBytes, _ := ioutil.ReadAll(req.Body)
					req.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
					if err := xml.Unmarshal(bodyBytes, &wfsTransaction); err != nil {
						return err
					}
					for _, u := range wfsTransaction.Updates {
						if !getLayerPermissions(u.TypeName).Has("update") {
							return echo.ErrForbidden
						}
						attrsFlags := getLayerAttributesFlags(u.TypeName)
						for _, p := range u.Properties {
							if !attrsFlags[p.Name].Has("edit") {
								return echo.ErrForbidden
							}
						}
					}
					for _, i := range wfsTransaction.Inserts {
						for _, o := range i.Objects {
							if !getLayerPermissions(o.XMLName.Local).Has("insert") {
								return echo.ErrForbidden
							}
							attrsFlags := getLayerAttributesFlags(o.XMLName.Local)
							for _, p := range o.Properties {
								if !attrsFlags[p.XMLName.Local].Has("edit") {
									return echo.ErrForbidden
								}
							}
						}
					}
					for _, d := range wfsTransaction.Deletes {
						if !getLayerPermissions(d.TypeName).Has("delete") {
							return echo.ErrForbidden
						}
					}
				} else if strings.EqualFold(params.Request, "GetFeature") {
					bodyBytes, _ := ioutil.ReadAll(req.Body)
					var getFeature GetFeature
					if err := xml.Unmarshal(bodyBytes, &getFeature); err != nil {
						return err
					}
					bodyModified := false
					for i, q := range getFeature.Query {
						if !getLayerPermissions(q.TypeName).Has("query") {
							return echo.ErrForbidden
						}
						attrsFlags := getLayerAttributesFlags(q.TypeName)
						// Note: at least one valid non-geometry field must be specified, otherwise qgis server will return all fields
						if len(q.Properties) > 0 {
							nonGeomProperties := 0
							for _, p := range q.Properties {
								if p.Name != "geometry" {
									aFlags, exist := attrsFlags[p.Name]
									if !exist || !aFlags.Has("view") {
										return echo.ErrForbidden
									}
									nonGeomProperties += 1
								}
							}
							if nonGeomProperties == 0 {
								return echo.ErrForbidden
							}
							req.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
						} else {
							properties := []PropertyName{{Name: "geometry"}}
							for name, flags := range attrsFlags {
								if flags.Has("view") {
									properties = append(properties, PropertyName{Name: name})
								}
							}
							if len(properties) == 1 {
								return echo.ErrForbidden
							}
							getFeature.Query[i].Properties = properties
							bodyModified = true
						}
					}
					if bodyModified {
						newData, err := xml.Marshal(getFeature)
						if err != nil {
							s.log.Errorw("transforming GetFeature request", zap.Error(err))
							return err
						}
						req.Body = ioutil.NopCloser(bytes.NewBuffer(newData))
						newSize := len(newData)
						req.Header.Set("Content-Length", strconv.Itoa(newSize))
						req.ContentLength = int64(newSize)
						// s.log.Debugw("GetFeature request", "data", string(newData))
					} else {
						req.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
					}
				}
			}
		}
		reverseProxy.ServeHTTP(c.Response(), req)
		return nil
	}
}
