package skills

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Skill represents a reusable skill definition.
type Skill struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Prompt      string   `yaml:"prompt"`
	Tools       []string `yaml:"tools"`
	Version     string   `yaml:"version"`
	Author      string   `yaml:"author,omitempty"`
}

// Loader loads skill definitions from disk.
type Loader struct {
	skillsDir string
	skills    map[string]*Skill
}

// NewLoader creates a new skill loader.
func NewLoader(skillsDir string) *Loader {
	return &Loader{
		skillsDir: skillsDir,
		skills:    make(map[string]*Skill),
	}
}

// LoadAll loads all skill definitions from the skills directory.
func (l *Loader) LoadAll() error {
	entries, err := os.ReadDir(l.skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no skills dir is fine
		}
		return fmt.Errorf("read skills dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(l.skillsDir, entry.Name())
		if err := l.loadFile(path); err != nil {
			return fmt.Errorf("load skill %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// loadFile loads a single skill definition file.
func (l *Loader) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var skill Skill
	if err := yaml.Unmarshal(data, &skill); err != nil {
		return fmt.Errorf("parse YAML: %w", err)
	}

	if skill.Name == "" {
		return fmt.Errorf("skill in %s has no name", path)
	}

	l.skills[skill.Name] = &skill
	return nil
}

// Get retrieves a skill by name.
func (l *Loader) Get(name string) (*Skill, bool) {
	s, ok := l.skills[name]
	return s, ok
}

// List returns all loaded skill names.
func (l *Loader) List() []string {
	names := make([]string, 0, len(l.skills))
	for name := range l.skills {
		names = append(names, name)
	}
	return names
}

// All returns all loaded skills.
func (l *Loader) All() map[string]*Skill {
	return l.skills
}

// InjectPrompt generates a system prompt section from a skill.
func (s *Skill) InjectPrompt() string {
	return fmt.Sprintf("\n## Skill: %s\n%s\nAvailable tools: %v\n", s.Name, s.Prompt, s.Tools)
}
