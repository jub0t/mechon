// Package templates holds the built-in bot templates. Each is one YAML file in this directory;
// adding a template is adding a file.
package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jub0t/mechon/internal/proto"
)

//go:embed *.yaml
var files embed.FS

type EnvVar struct {
	Key      string `yaml:"key" json:"key"`
	Label    string `yaml:"label" json:"label"`
	Secret   bool   `yaml:"secret" json:"secret"`
	Required bool   `yaml:"required" json:"required"`
	Default  string `yaml:"default" json:"default,omitempty"`
}

type Template struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Language    string   `yaml:"language" json:"language"`
	Description string   `yaml:"description" json:"description"`
	Image       string   `yaml:"image" json:"image"`
	Install     string   `yaml:"install" json:"-"`
	Start       string   `yaml:"start" json:"-"`
	Env         []EnvVar `yaml:"env" json:"env"`
}

func (t Template) Proto() proto.Template {
	return proto.Template{ID: t.ID, Image: t.Image, Install: strings.TrimSpace(t.Install), Start: strings.TrimSpace(t.Start)}
}

var all = mustLoad()

func mustLoad() []Template {
	entries, err := fs.Glob(files, "*.yaml")
	if err != nil {
		panic(err)
	}
	var out []Template
	for _, name := range entries {
		raw, _ := files.ReadFile(name)
		var t Template
		if err := yaml.Unmarshal(raw, &t); err != nil {
			panic(fmt.Sprintf("template %s: %v", name, err))
		}
		if t.ID == "" || t.Image == "" || t.Start == "" {
			panic(fmt.Sprintf("template %s: id, image and start are required", name))
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Template) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func All() []Template { return all }

func Get(id string) (Template, bool) {
	i := slices.IndexFunc(all, func(t Template) bool { return t.ID == id })
	if i < 0 {
		return Template{}, false
	}
	return all[i], true
}
