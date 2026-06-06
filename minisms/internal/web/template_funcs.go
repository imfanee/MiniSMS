// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"html/template"
	"path/filepath"
	"strings"

	"github.com/minisms/minisms"
)

// TemplateFuncs returns funcs shared by all admin HTML templates.
func TemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				panic("dict: odd number of arguments")
			}
			m := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				m[values[i].(string)] = values[i+1]
			}
			return m, nil
		},
		"hasPrefix": strings.HasPrefix,
		"hasPerm": func(perms map[string]bool, key string) bool {
			if perms == nil {
				return false
			}
			return perms[key]
		},
	}
}

// ParseTemplates parses embedded templates with admin funcs (hasPerm, hasPrefix).
func ParseTemplates(patterns ...string) (*template.Template, error) {
	name := "base.html"
	if len(patterns) == 1 {
		name = filepath.Base(patterns[0])
	}
	return template.New(name).Funcs(TemplateFuncs()).ParseFS(minisms.TemplateFS, patterns...)
}
