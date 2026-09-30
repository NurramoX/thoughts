// Package skill embeds the agent skills (spec §9), which `thought install` writes
// to ~/.claude/skills/<name>/SKILL.md: `thought`, and `new-thought`, the /new-thought
// command.
package skill

import (
	"embed"
	"path"
)

//go:embed */SKILL.md
var files embed.FS

// Skill is one skill: its directory name and its SKILL.md.
type Skill struct {
	Name     string
	Markdown []byte
}

// All returns the embedded skills, sorted by name.
func All() []Skill {
	dirs, err := files.ReadDir(".")
	if err != nil {
		panic(err)
	}
	var all []Skill
	for _, d := range dirs {
		b, err := files.ReadFile(path.Join(d.Name(), "SKILL.md"))
		if err != nil {
			panic(err)
		}
		all = append(all, Skill{d.Name(), b})
	}
	return all
}
