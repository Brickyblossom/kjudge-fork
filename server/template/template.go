package template

import (
	"encoding/json"
	"html/template"
	"io"
	"io/fs"
	"log"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/natsukagami/kjudge/embed"
	"github.com/pkg/errors"
)

// List of all template's requirements.
// All requirements are then prepended (recursively) into the requirement list.
//
// The root template "root" is always prepended at the beginning.
var templateList = map[string][]string{
	"admin/home":                  {"admin/root", "admin/contest_inputs"},
	"admin/contests":              {"admin/root", "admin/contest_inputs"},
	"admin/contest":               {"admin/root", "admin/contest_inputs", "admin/problem_inputs"},
	"admin/contest_submissions":   {"admin/root", "admin/submission_inputs"},
	"admin/contest_announcements": {"admin/root"},
	"admin/problem":               {"admin/root", "admin/problem_inputs", "admin/test_inputs", "admin/test_group_inputs", "admin/file_inputs"},
	"admin/test_group":            {"admin/root", "admin/test_inputs", "admin/test_group_inputs"},
	"admin/problem_submissions":   {"admin/root", "admin/submission_inputs"},
	"admin/users":                 {"admin/root", "admin/user_inputs"},
	"admin/user":                  {"admin/root", "admin/user_inputs", "admin/submission_inputs"},
	"admin/submissions":           {"admin/root", "admin/submission_inputs"},
	"admin/submission":            {"admin/root"},
	"admin/jobs":                  {"admin/root"},
	"admin/contest_scoreboard":    {"admin/root"},
	"admin/clarifications":        {"admin/root"},
	"admin/login":                 {},

	"user/login": {"user_root"},
	"user/home":  {"user_root"},

	"contests/home":            {"user_root"},
	"contests/root":            {"user_root"},
	"contests/overview":        {"contests/root"},
	"contests/messages":        {"contests/root"},
	"contests/problem":         {"contests/root"},
	"contests/submission":      {"contests/root"},
	"contests/scoreboard":      {"contests/root"},
	"contests/scoreboard_wide": {},

	"error": {},
}

// From a single template name, resolve the requirement tree into a list of template names.
func resolveTemplate(name string, into []string) []string {
	reqs, ok := templateList[name]
	// We're at a non-leaf template.
	if ok {
		// resolve the inner requirements one by one
		for _, req := range reqs {
			into = resolveTemplate(req, into)
		}
	}
	return append(into, name)
}

// Renderer implements echo.Renderer
type Renderer struct{}

var _ echo.Renderer = Renderer{}

// Render implement echo.Renderer.Render
func (r Renderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return Render(w, name, data)
}

func templateFilename(name string) string {
	return "templates/" + name + ".html"
}

func parseTemplateTree(root *template.Template, name string) (*template.Template, error) {
	names := resolveTemplate(name, nil)
	t, err := root.Clone()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	for _, name := range names {
		content, err := fs.ReadFile(embed.Content, templateFilename(name))
		if err != nil {
			return nil, errors.Wrapf(err, "file %s", name)
		}
		if _, err := t.New(name).Parse(string(content)); err != nil {
			return nil, errors.Wrapf(err, "file %s", name)
		}
	}
	return t, nil
}

func parseRootTemplate() (*template.Template, error) {
	root, err := fs.ReadFile(embed.Content, "templates/root.html")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	tRoot := template.New("")
	// Include a bunch of funcs
	tRoot.Funcs(map[string]interface{}{
		"time":     func(t time.Time) string { return t.Format(time.RFC1123) },
		"isFuture": func(t time.Time) bool { return t.After(time.Now()) },
		"isPast":   func(t time.Time) bool { return t.Before(time.Now()) },
		"join":     strings.Join,
		"add":      func(a, b int) int { return a + b },
		"version":  version,
		"loggedIn": loggedIn,
		"json":     func(item interface{}) (string, error) { b, err := json.Marshal(item); return string(b), err },
		"zip":      func(items ...interface{}) []interface{} { return items },
	})
	tRoot, err = tRoot.Parse(string(root))
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return tRoot, nil
}

// Searches for and load all html templates.
func parseAllTemplates() (map[string]*template.Template, error) {
	tRoot, err := parseRootTemplate()
	if err != nil {
		return nil, err
	}
	mp := make(map[string]*template.Template)
	names := []string{}
	for file := range templateList {
		names = append(names, file)
		t, err := parseTemplateTree(tRoot, file)
		if err != nil {
			return nil, errors.Wrapf(err, "file %s", file)
		}
		mp[file] = t
	}
	log.Printf("defined templates: %s", strings.Join(names, ", "))
	return mp, nil
}
