package server

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/labstack/echo/v4"
)

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
	XMLName    xml.Name   `xml:"Delete"`
	TypeName   string     `xml:"typeName,attr"`
	Properties []Property `xml:"Property"`
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
}

func parseTypeName(typeName string) (string, error) {
	parts := strings.Split(typeName, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("Invalid typeName: %s", typeName)
	}
	return parts[1], nil
}

// TODO: old version, remove it
func (s *Server) owsHandler() func(echo.Context) error {
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
	reverseProxy := &httputil.ReverseProxy{Director: director}
	return func(c echo.Context) error {
		params := new(OwsRequestParams)
		if err := (&echo.DefaultBinder{}).BindQueryParams(c, params); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid query parameters")
		}
		if params.Service == "WFS" && params.Request == "" {

			projectName := strings.TrimSuffix(params.Map, filepath.Ext(params.Map))
			// p, err := s.projects.GetProject(projectName)
			// if err != nil {
			// 	return err
			// }
			layersData, err := s.projects.GetLayersData(projectName)
			if err != nil {
				return err
			}
			settings, err := s.projects.GetSettings(projectName)
			if err != nil {
				return err
			}
			s.log.Info("layersData", layersData)
			user, err := s.auth.GetUser(c) // todo: load user data only when needed (access control is defined)
			// perms := settings.UserLayersPermissions(user)
			perms := make(map[string]domain.LayerPermission)

			getLayerPermissions := func(typeName string) domain.LayerPermission {
				parts := strings.Split(typeName, ":")
				lname := parts[len(parts)-1]
				id, _ := layersData.LayerNameToID[lname]
				perm, ok := perms[id]
				if ok {
					return perm
				} else {
					perm = settings.UserLayerPermissions(user, id)
					perms[id] = perm
				}
				return domain.LayerPermission{View: false, Insert: false, Update: false, Delete: false}
			}

			var wfsTransaction Transaction
			// read all bytes from content body and create new stream using it.
			bodyBytes, _ := ioutil.ReadAll(c.Request().Body)
			c.Request().Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
			if err := xml.Unmarshal(bodyBytes, &wfsTransaction); err != nil {
				return err
			}
			for _, u := range wfsTransaction.Updates {
				if !getLayerPermissions(u.TypeName).Update {
					return echo.ErrForbidden
				}
			}
			for _, i := range wfsTransaction.Inserts {
				for _, o := range i.Objects {
					if !getLayerPermissions(o.XMLName.Local).Insert {
						return echo.ErrForbidden
					}
				}
			}
			for _, d := range wfsTransaction.Deletes {
				if !getLayerPermissions(d.TypeName).Delete {
					return echo.ErrForbidden
				}
			}
		}
		reverseProxy.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}
