package aitool

import (
	"context"
	"net/url"
	"path"
	"strings"
	"unicode"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/junler/intent-admin/internal/auth"
	"github.com/junler/intent-admin/internal/capability"
	"github.com/junler/intent-admin/internal/pkg/errcode"
)

type Navigation interface {
	Routes(context.Context) ([]auth.Route, error)
}

type Page struct {
	Title string `json:"title"`
	Route string `json:"route"`
}

type navigateInput struct {
	Route string `json:"route" jsonschema:"description=从 list_pages 选择的准确页面 route，不允许猜测或外部链接"`
}

// Pages come from the same permission-derived menu tree as the SPA. Navigation
// does not expose the page's configuration writes to the model.
func (t *Tools) pages(ctx context.Context) ([]Page, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}
	if t.Navigation == nil {
		return nil, errcode.Internalf("页面导航不可用")
	}
	routes, err := t.Navigation.Routes(ctx)
	if err != nil {
		return nil, err
	}
	pages := []Page{}
	var walk func([]auth.Route, string)
	walk = func(routes []auth.Route, parent string) {
		for _, route := range routes {
			if route.External || strings.ContainsAny(route.Path, "\\?#") || strings.ContainsFunc(route.Path, unicode.IsControl) {
				continue
			}
			u, err := url.Parse(route.Path)
			if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(route.Path, "//") {
				continue
			}
			base := path.Clean("/" + strings.TrimPrefix(route.Path, "/"))
			if !strings.HasPrefix(route.Path, "/") {
				base = path.Join(parent, route.Path)
			}
			switch route.Component {
			case "InnerLink":
				continue
			case "", "Layout", "ParentView":
			default:
				pages = append(pages, Page{Title: route.Meta.Title, Route: base})
			}
			walk(route.Children, base)
		}
	}
	walk(routes, "/")
	return pages, nil
}

func (t *Tools) findPage(ctx context.Context, route string) (Page, error) {
	pages, err := t.pages(ctx)
	if err != nil {
		return Page{}, err
	}
	for _, page := range pages {
		if page.Route == route {
			return page, nil
		}
	}
	return Page{}, errcode.NotFoundf("页面不存在或无权访问")
}

func (t *Tools) Navigate(ctx context.Context, route string) (Page, error) {
	page, err := t.findPage(ctx, route)
	if err != nil {
		return Page{}, err
	}
	t.Navigated.Store(true)
	if t.Emit != nil {
		t.Emit("navigate", map[string]any{"route": page.Route, "note": "已打开" + page.Title})
	}
	return page, nil
}

// SuggestPage retains an optional destination in the answer without navigating.
func (t *Tools) SuggestPage(ctx context.Context, route string) (Page, error) {
	page, err := t.findPage(ctx, route)
	if err != nil {
		return Page{}, err
	}
	t.Evidence = append(t.Evidence, capability.Evidence{Route: page.Route})
	return page, nil
}

func (t *Tools) navigationTools() ([]tool.BaseTool, error) {
	list, err := utils.InferTool("list_pages", "查找当前用户有权访问的页面，按标题或路径搜索。打开页面前先查此目录，无需查询业务数据。", func(ctx context.Context, in keywordInput) (out any, err error) {
		if err = t.before(ctx, "list_pages"); err != nil {
			return nil, err
		}
		defer func() { t.after("list_pages", out, err) }()
		pages, err := t.pages(ctx)
		if err != nil {
			return nil, err
		}
		rows := []Page{}
		keyword := strings.ToLower(strings.TrimSpace(in.Keyword))
		for _, page := range pages {
			if keyword == "" || strings.Contains(strings.ToLower(page.Title+" "+page.Route), keyword) {
				rows = append(rows, page)
			}
		}
		return rows, nil
	})
	if err != nil {
		return nil, err
	}
	navigate, err := utils.InferTool("navigate", "直接打开 list_pages 返回的页面，前端自动跳转并收起对话框，无需用户点击。仅切换页面，不查询或修改业务数据。", func(ctx context.Context, in navigateInput) (out any, err error) {
		if err = t.before(ctx, "navigate"); err != nil {
			return nil, err
		}
		defer func() { t.after("navigate", out, err) }()
		return t.Navigate(ctx, in.Route)
	})
	if err != nil {
		return nil, err
	}
	suggest, err := utils.InferTool("suggest_page", "在回答或任务完成后提供可点击的相关页面入口，询问用户是否前往查看。先从 list_pages 选择准确路径；此工具不会跳转，用户明确要求跳转时才用 navigate。", func(ctx context.Context, in navigateInput) (out any, err error) {
		if err = t.before(ctx, "suggest_page"); err != nil {
			return nil, err
		}
		defer func() { t.after("suggest_page", out, err) }()
		return t.SuggestPage(ctx, in.Route)
	})
	if err != nil {
		return nil, err
	}
	return []tool.BaseTool{list, navigate, suggest}, nil
}
