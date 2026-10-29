package auth

import (
	"sort"
	"strings"

	"github.com/junler/intent-admin/internal/model"
)

// Route is the permission-derived router contract consumed by the SPA.
type Route struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Component string    `json:"component"`
	Hidden    bool      `json:"hidden"`
	Query     string    `json:"query,omitempty"`
	Meta      RouteMeta `json:"meta"`
	Children  []Route   `json:"children,omitempty"`
}
type RouteMeta struct {
	Title   string `json:"title"`
	Icon    string `json:"icon,omitempty"`
	NoCache bool   `json:"noCache"`
}

// BuildRoutes uses the effective permission union, rather than role-menu IDs,
// so an individual grant makes its page visible too. Disabled directories
// prune their descendants; hidden pages remain routable.
func BuildRoutes(menus []model.Menu, permissions []string) []Route {
	perms := map[string]bool{}
	for _, p := range permissions {
		perms[p] = true
	}
	children := map[int64][]model.Menu{}
	for _, m := range menus {
		if m.Status != "0" || m.DelFlag != "0" || m.MenuType == "F" {
			continue
		}
		children[m.ParentID] = append(children[m.ParentID], m)
	}
	for id := range children {
		sort.SliceStable(children[id], func(i, j int) bool {
			a, b := children[id][i], children[id][j]
			if a.OrderNum == b.OrderNum {
				return a.MenuID < b.MenuID
			}
			return a.OrderNum < b.OrderNum
		})
	}
	var walk func(int64, map[int64]bool) []Route
	walk = func(parent int64, seen map[int64]bool) []Route {
		result := []Route{}
		for _, m := range children[parent] {
			if seen[m.MenuID] {
				continue
			}
			seen[m.MenuID] = true
			sub := walk(m.MenuID, seen)
			delete(seen, m.MenuID)
			if m.MenuType == "M" {
				if len(sub) == 0 {
					continue
				}
			} else if m.MenuType != "C" || (m.Perms != "" && !perms[m.Perms] && !perms["*:*:*"]) {
				continue
			}
			component := m.Component
			if component == "" && m.MenuType == "M" {
				component = "ParentView"
			}
			result = append(result, Route{Name: strings.ReplaceAll(m.Path, "/", "_") + "_" + itoa(m.MenuID), Path: m.Path, Component: component, Hidden: m.Visible == "1", Query: m.Query, Meta: RouteMeta{Title: m.MenuName, Icon: m.Icon, NoCache: m.IsCache == "1"}, Children: sub})
		}
		return result
	}
	return walk(0, map[int64]bool{})
}
